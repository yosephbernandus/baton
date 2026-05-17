package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
)

type SoftCompileResult struct {
	Facts []*PackageFact
	Claims []SoftClaim
}

const softPromptTemplate = `Analyze the following source files and extract all functions, classes, and types.

Output ONLY valid JSON in this exact format (no markdown, no explanation):
{
  "packages": [
    {
      "package": "<relative-directory-path>",
      "functions": [
        {"name": "<function-name>", "params": "<param-list>", "returns": "<return-type>", "exported": true}
      ],
      "types": [
        {"name": "<type-name>", "kind": "<class|interface|struct|enum>", "methods": ["method1", "method2"], "exported": true}
      ]
    }
  ]
}

Rules:
- Group by directory (each directory = one package)
- exported = true if public (no underscore prefix in Python, exported in JS/TS)
- Include ALL functions and classes, not just a sample
- params should be a short signature like "x: int, y: str"
- For kind: use "class" for Python/JS classes, "interface" for TS interfaces, "struct" for dataclasses

Files to analyze:
`

type softResponse struct {
	Packages []softPackage `json:"packages"`
}

type softPackage struct {
	Package   string         `json:"package"`
	Functions []softFunction `json:"functions"`
	Types     []softType     `json:"types"`
}

type softFunction struct {
	Name     string `json:"name"`
	Params   string `json:"params"`
	Returns  string `json:"returns"`
	Exported bool   `json:"exported"`
}

type softType struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Methods  []string `json:"methods"`
	Exported bool     `json:"exported"`
}

type SoftOpts struct {
	Runtime string // runtime name from config (e.g. "claude-code", "opencode")
	Model   string // model to use (e.g. "haiku", "gpt-4o-mini")
	Config  *config.Config
}

func CompileSoft(projectDir string, lang DetectedLang, opts SoftOpts) (*SoftCompileResult, error) {
	if opts.Config == nil || opts.Runtime == "" {
		return nil, fmt.Errorf("no runtime configured — set orchestrator.runtime in agents.yaml or pass --runtime/--model flags")
	}

	rt, ok := opts.Config.Runtimes[opts.Runtime]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found in config (available: %s)", opts.Runtime, availableRuntimes(opts.Config))
	}

	files, err := findSourceFiles(projectDir, lang.Name)
	if err != nil {
		return nil, err
	}

	batches := batchFiles(files, 50)
	var allFacts []*PackageFact

	for i, batch := range batches {
		fmt.Fprintf(os.Stderr, "  analyzing batch %d/%d (%d files)...\n", i+1, len(batches), len(batch))

		prompt := buildSoftPrompt(batch, projectDir)
		output, err := runLLMAnalysis(projectDir, prompt, &rt, opts.Model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: batch %d failed: %v\n", i+1, err)
			continue
		}

		facts, err := parseSoftOutput(output, projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: batch %d parse error: %v\n", i+1, err)
			continue
		}

		allFacts = append(allFacts, facts...)
	}

	if len(allFacts) == 0 {
		return nil, fmt.Errorf("LLM analysis produced no results")
	}

	verified := verifySoftFacts(projectDir, allFacts)

	return &SoftCompileResult{
		Facts: verified,
	}, nil
}

func availableRuntimes(cfg *config.Config) string {
	var names []string
	for k := range cfg.Runtimes {
		names = append(names, k)
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func buildSoftPrompt(files []string, projectDir string) string {
	var b strings.Builder
	b.WriteString(softPromptTemplate)

	for _, f := range files {
		rel, _ := filepath.Rel(projectDir, f)
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// Truncate large files
		text := string(content)
		if len(text) > 8000 {
			text = text[:8000] + "\n... (truncated)"
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", rel, text)
	}

	return b.String()
}

func runLLMAnalysis(projectDir, prompt string, rt *config.RuntimeConfig, model string) (string, error) {
	cmdPath, err := exec.LookPath(rt.Command)
	if err != nil {
		return "", fmt.Errorf("runtime command %q not found in PATH", rt.Command)
	}

	stdinMode := rt.PromptMode == "stdin"

	var args []string
	for _, p := range rt.Positional {
		if p == "{{prompt}}" {
			if !stdinMode {
				args = append(args, prompt)
			}
		} else {
			args = append(args, p)
		}
	}
	if rt.ModelFlag != "" && model != "" {
		args = append(args, rt.ModelFlag, model)
	}
	args = append(args, rt.ExtraFlags...)
	if rt.PromptFlag != "" && !stdinMode {
		args = append(args, rt.PromptFlag, prompt)
	}

	cmd := exec.Command(cmdPath, args...)
	cmd.Dir = projectDir

	if stdinMode {
		cmd.Stdin = strings.NewReader(prompt)
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("LLM exited %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return "", err
	}

	return string(out), nil
}

func parseSoftOutput(output, projectDir string) ([]*PackageFact, error) {
	// Extract JSON from output (LLM might wrap in markdown)
	jsonStr := extractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in LLM output")
	}

	var resp softResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parsing LLM JSON: %w", err)
	}

	var facts []*PackageFact
	for _, pkg := range resp.Packages {
		fact := &PackageFact{
			Package:    pkg.Package,
			Path:       filepath.Join(projectDir, pkg.Package),
			CompiledAt: time.Now().UTC(),
			SourceHash: hashDir(filepath.Join(projectDir, pkg.Package)),
		}

		for _, fn := range pkg.Functions {
			var params []ParamFact
			if fn.Params != "" {
				params = parseParamString(fn.Params)
			}
			fact.Functions = append(fact.Functions, FunctionFact{
				Name:     fn.Name,
				Params:   params,
				Returns:  fn.Returns,
				Exported: fn.Exported,
			})
		}

		for _, t := range pkg.Types {
			fact.Types = append(fact.Types, TypeFact{
				Name:     t.Name,
				Kind:     t.Kind,
				Methods:  t.Methods,
				Exported: t.Exported,
			})
		}

		if len(fact.Functions) > 0 || len(fact.Types) > 0 {
			facts = append(facts, fact)
		}
	}

	return facts, nil
}

func verifySoftFacts(projectDir string, facts []*PackageFact) []*PackageFact {
	var verified []*PackageFact
	for _, fact := range facts {
		var validFuncs []FunctionFact
		for _, fn := range fact.Functions {
			// Verify function exists via grep
			results, err := Grep(fn.Name, GrepOpts{Dir: projectDir, MaxResults: 1})
			if err == nil && len(results) > 0 {
				validFuncs = append(validFuncs, fn)
			}
		}

		var validTypes []TypeFact
		for _, t := range fact.Types {
			results, err := Grep(t.Name, GrepOpts{Dir: projectDir, MaxResults: 1})
			if err == nil && len(results) > 0 {
				validTypes = append(validTypes, t)
			}
		}

		if len(validFuncs) > 0 || len(validTypes) > 0 {
			fact.Functions = validFuncs
			fact.Types = validTypes
			verified = append(verified, fact)
		}
	}
	return verified
}

func extractJSON(s string) string {
	// Find first { and last }
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}
	end := strings.LastIndex(s, "}")
	if end == -1 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func parseParamString(s string) []ParamFact {
	var params []ParamFact
	parts := strings.Split(s, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if idx := strings.Index(p, ":"); idx > 0 {
			params = append(params, ParamFact{
				Name: strings.TrimSpace(p[:idx]),
				Type: strings.TrimSpace(p[idx+1:]),
			})
		} else {
			params = append(params, ParamFact{Name: p})
		}
	}
	return params
}

func batchFiles(files []string, batchSize int) [][]string {
	var batches [][]string
	for i := 0; i < len(files); i += batchSize {
		end := i + batchSize
		if end > len(files) {
			end = len(files)
		}
		batches = append(batches, files[i:end])
	}
	return batches
}
