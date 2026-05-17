package knowledge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CompileResult struct {
	Graph        *Graph
	Health       Health
	PackageCount int
}

func Compile(projectDir string) (*CompileResult, error) {
	langs := DetectLanguages(projectDir)
	if len(langs) == 0 {
		return nil, fmt.Errorf("no source files detected in %s", projectDir)
	}

	var allFacts []*PackageFact

	for _, lang := range langs {
		switch {
		case lang.Name == "go":
			facts, err := compileGo(projectDir)
			if err != nil {
				return nil, err
			}
			allFacts = append(allFacts, facts...)

		case lang.Available:
			fmt.Fprintf(os.Stderr, "compiling %s via %s...\n", lang.Name, lang.LSP)
			lspResult, err := CompileWithLSP(projectDir, lang)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s LSP failed: %v\n", lang.Name, err)
				continue
			}
			allFacts = append(allFacts, lspResult.Facts...)

		default:
			fmt.Fprintf(os.Stderr, "skipping %s: %s not installed\n", lang.Name, lang.LSP)
		}
	}

	if len(allFacts) == 0 {
		return nil, fmt.Errorf("no packages compiled (check LSP availability)")
	}

	graph := BuildGraph(allFacts)

	funcCount := 0
	typeCount := 0
	for _, f := range allFacts {
		funcCount += len(f.Functions)
		typeCount += len(f.Types)
	}

	result := &CompileResult{
		Graph:        graph,
		PackageCount: len(allFacts),
		Health: Health{
			CompiledAt:    time.Now().UTC(),
			PackageCount:  len(allFacts),
			FunctionCount: funcCount,
			TypeCount:     typeCount,
		},
	}

	return result, nil
}

type DetectedLang struct {
	Name      string
	FileCount int
	LSP       string // binary name to check
	Available bool   // LSP found in PATH
}

var langExtensions = map[string]struct {
	lang string
	lsp  string
}{
	".go":   {"go", "gopls"},
	".py":   {"python", "pyright-langserver"},
	".pyi":  {"python", "pyright-langserver"},
	".ts":   {"typescript", "typescript-language-server"},
	".tsx":  {"typescript", "typescript-language-server"},
	".js":   {"typescript", "typescript-language-server"},
	".jsx":  {"typescript", "typescript-language-server"},
	".rs":   {"rust", "rust-analyzer"},
	".java": {"java", "jdtls"},
	".kt":   {"kotlin", "kotlin-language-server"},
	".rb":   {"ruby", "ruby-lsp"},
	".cs":   {"csharp", "OmniSharp"},
	".dart": {"dart", "dart"},
	".swift": {"swift", "sourcekit-lsp"},
	".php":  {"php", "intelephense"},
	".ex":   {"elixir", "elixir-ls"},
	".exs":  {"elixir", "elixir-ls"},
	".zig":  {"zig", "zls"},
	".lua":  {"lua", "lua-language-server"},
	".c":    {"cpp", "clangd"},
	".cpp":  {"cpp", "clangd"},
	".h":    {"cpp", "clangd"},
	".hpp":  {"cpp", "clangd"},
}

func DetectLanguages(projectDir string) []DetectedLang {
	counts := map[string]int{}
	lspMap := map[string]string{}

	filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" || base == ".baton" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if entry, ok := langExtensions[ext]; ok {
			counts[entry.lang]++
			lspMap[entry.lang] = entry.lsp
		}
		return nil
	})

	var results []DetectedLang
	for lang, count := range counts {
		lspBin := lspMap[lang]
		available := isLSPAvailable(lspBin)
		results = append(results, DetectedLang{
			Name:      lang,
			FileCount: count,
			LSP:       lspBin,
			Available: available,
		})
	}

	// Sort by file count descending (primary language first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].FileCount > results[j].FileCount
	})

	return results
}

func isLSPAvailable(bin string) bool {
	path, err := exec.LookPath(bin)
	if err != nil {
		return false
	}
	// Verify binary is functional (catches rustup shims for uninstalled components).
	// LSP servers don't support --version, so we just check the binary is real
	// by running it briefly. A broken shim exits with error immediately.
	cmd := exec.Command(path, "--stdio")
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	// If it exits with specific error about "Unknown binary" (rustup shim), it's not available
	if err != nil && strings.Contains(string(out), "Unknown binary") {
		return false
	}
	// Otherwise binary exists and runs (or just needs stdin/protocol)
	return true
}

func DetectLanguage(projectDir string) string {
	langs := DetectLanguages(projectDir)
	if len(langs) == 0 {
		return ""
	}
	return langs[0].Name
}

func compileGo(projectDir string) ([]*PackageFact, error) {
	modulePath, err := readModulePath(projectDir)
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	dirs, err := findGoPackages(projectDir)
	if err != nil {
		return nil, fmt.Errorf("finding packages: %w", err)
	}

	var facts []*PackageFact
	for _, dir := range dirs {
		fact, err := ParsePackage(dir, modulePath)
		if err != nil {
			continue
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func findGoPackages(root string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == ".git" || base == ".baton" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		return nil
	})

	return dirs, err
}

func readModulePath(projectDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module path not found in go.mod")
}
