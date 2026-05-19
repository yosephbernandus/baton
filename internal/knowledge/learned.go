package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yosephbernandus/baton/internal/config"
	"gopkg.in/yaml.v3"
)

const learnedDomainsFile = "learned-domains.yaml"

type LearnedDomains struct {
	Imports map[string]string `yaml:"imports"`
	Types   map[string]string `yaml:"types"`
}

func LoadLearnedDomains(projectDir string) *LearnedDomains {
	merged := &LearnedDomains{
		Imports: map[string]string{},
		Types:   map[string]string{},
	}

	// Load global first
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".baton", learnedDomainsFile)
		if global, err := loadLearnedFile(globalPath); err == nil {
			mergeLearned(merged, global)
		}
	}

	// Project overrides global
	if projectDir != "" {
		projectPath := filepath.Join(projectDir, ".baton", "knowledge", learnedDomainsFile)
		if project, err := loadLearnedFile(projectPath); err == nil {
			mergeLearned(merged, project)
		}
	}

	return merged
}

func SaveLearnedDomains(projectDir string, learned *LearnedDomains) error {
	data, err := yaml.Marshal(learned)
	if err != nil {
		return err
	}

	// Save to project
	if projectDir != "" {
		dir := filepath.Join(projectDir, ".baton", "knowledge")
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, learnedDomainsFile), data, 0o644); err != nil {
			return err
		}
	}

	// Save to global
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".baton")
		_ = os.MkdirAll(dir, 0o755)
		globalPath := filepath.Join(dir, learnedDomainsFile)

		// Merge with existing global (don't overwrite other projects' learnings)
		existing := &LearnedDomains{
			Imports: map[string]string{},
			Types:   map[string]string{},
		}
		if ex, err := loadLearnedFile(globalPath); err == nil {
			existing = ex
		}
		mergeLearned(existing, learned)

		globalData, err := yaml.Marshal(existing)
		if err != nil {
			return err
		}
		return os.WriteFile(globalPath, globalData, 0o644)
	}

	return nil
}

func loadLearnedFile(path string) (*LearnedDomains, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ld LearnedDomains
	if err := yaml.Unmarshal(data, &ld); err != nil {
		return nil, err
	}
	if ld.Imports == nil {
		ld.Imports = map[string]string{}
	}
	if ld.Types == nil {
		ld.Types = map[string]string{}
	}
	return &ld, nil
}

func mergeLearned(dst, src *LearnedDomains) {
	for k, v := range src.Imports {
		dst.Imports[k] = v
	}
	for k, v := range src.Types {
		dst.Types[k] = v
	}
}

// ClassifyWithLearned checks the learned cache for an import or type name.
func (ld *LearnedDomains) ClassifyImport(imp string) string {
	low := strings.ToLower(imp)
	// Check exact match first
	if d, ok := ld.Imports[low]; ok {
		return d
	}
	// Check if any learned pattern is a substring
	for pattern, domain := range ld.Imports {
		if strings.Contains(low, pattern) {
			return domain
		}
	}
	return ""
}

func (ld *LearnedDomains) ClassifyType(name string) string {
	low := strings.ToLower(name)
	if d, ok := ld.Types[low]; ok {
		return d
	}
	return ""
}

// LearnFromLLM sends unclassified imports/types to an LLM and stores results.
func LearnFromLLM(projectDir string, unknownImports []string, unknownTypes []string, cfg *config.Config, runtime, model string) (*LearnedDomains, error) {
	if len(unknownImports) == 0 && len(unknownTypes) == 0 {
		return &LearnedDomains{
			Imports: map[string]string{},
			Types:   map[string]string{},
		}, nil
	}

	if cfg == nil || runtime == "" {
		return nil, fmt.Errorf("no runtime configured for LLM learning")
	}

	rt, ok := cfg.Runtimes[runtime]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found in config", runtime)
	}

	prompt := buildLearnPrompt(unknownImports, unknownTypes)
	output, err := runLearnLLM(projectDir, prompt, &rt, model)
	if err != nil {
		return nil, err
	}

	return parseLearnOutput(output)
}

func buildLearnPrompt(imports []string, types []string) string {
	var b strings.Builder
	b.WriteString(`Classify these programming imports and type names into domains.

Output ONLY valid JSON (no markdown, no explanation):
{
  "imports": {"import_name": "domain", ...},
  "types": {"type_name": "domain", ...}
}

Valid domains: api, database, security, testing, frontend, cli, infra, serialization, concurrency, observability, system, ml, events, config, pipeline, core

Rules:
- Use the most specific domain that fits
- "api" = web frameworks, HTTP, REST, GraphQL, gRPC
- "database" = ORMs, SQL, NoSQL, caches
- "ml" = machine learning, AI, data science
- "frontend" = UI frameworks, CSS, components
- "core" = general utilities, helpers
- If truly unknown, use "core"

`)

	if len(imports) > 0 {
		b.WriteString("Imports to classify:\n")
		for _, imp := range imports {
			fmt.Fprintf(&b, "- %s\n", imp)
		}
	}
	if len(types) > 0 {
		b.WriteString("\nTypes to classify:\n")
		for _, t := range types {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}

	return b.String()
}

func runLearnLLM(projectDir, prompt string, rt *config.RuntimeConfig, model string) (string, error) {
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

func parseLearnOutput(output string) (*LearnedDomains, error) {
	jsonStr := extractLearnJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in LLM output")
	}

	var raw struct {
		Imports map[string]string `json:"imports"`
		Types   map[string]string `json:"types"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("parsing LLM JSON: %w", err)
	}

	ld := &LearnedDomains{
		Imports: map[string]string{},
		Types:   map[string]string{},
	}

	validDomains := map[string]bool{
		"api": true, "database": true, "security": true, "testing": true,
		"frontend": true, "cli": true, "infra": true, "serialization": true,
		"concurrency": true, "observability": true, "system": true, "ml": true,
		"events": true, "config": true, "pipeline": true, "core": true,
	}

	for k, v := range raw.Imports {
		v = strings.ToLower(strings.TrimSpace(v))
		if validDomains[v] {
			ld.Imports[strings.ToLower(k)] = v
		}
	}
	for k, v := range raw.Types {
		v = strings.ToLower(strings.TrimSpace(v))
		if validDomains[v] {
			ld.Types[strings.ToLower(k)] = v
		}
	}

	return ld, nil
}

func extractLearnJSON(s string) string {
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
