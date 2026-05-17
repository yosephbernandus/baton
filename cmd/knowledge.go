package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/knowledge"
)

func NewKnowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage project knowledge graph (AST-based)",
	}

	cmd.AddCommand(newKnowledgeCompileCmd())
	cmd.AddCommand(newKnowledgeQueryCmd())
	cmd.AddCommand(newKnowledgeHealthCmd())

	return cmd
}

func newKnowledgeCompileCmd() *cobra.Command {
	var soft bool

	cmd := &cobra.Command{
		Use:           "compile",
		Short:         "Parse codebase and build knowledge graph",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return exitError(1, "getting working directory: %v", err)
			}

			fmt.Println("Detecting project languages...")
			langs := knowledge.DetectLanguages(cwd)
			if len(langs) == 0 {
				return exitError(1, "no source files detected in %s", cwd)
			}

			hasCompilable := false
			for _, l := range langs {
				status := "LSP not found"
				if l.Available {
					status = l.LSP + " ✓"
					hasCompilable = true
				}
				if l.Name == "go" {
					status = "go/ast (stdlib)"
					hasCompilable = true
					if l.Available {
						status = l.LSP + " ✓ (+ go/ast fallback)"
					}
				}
				fmt.Printf("  %-12s %4d files   %s\n", l.Name, l.FileCount, status)
			}
			fmt.Println()

			// If no LSP available and --soft not set, show actionable options
			if !hasCompilable && !soft {
				fmt.Println("No LSP available for detected languages.\n")
				fmt.Println("Options:")
				for _, l := range langs {
					if l.Name == "go" {
						continue
					}
					fmt.Printf("  [install]  %s\n", lspInstallHint(l))
				}
				fmt.Printf("  [soft]     re-run with --soft to use LLM analysis (costs tokens)\n")
				fmt.Printf("  [skip]     skip unsupported languages\n")
				fmt.Println()
				return exitError(1, "run with --soft for LLM fallback, or install LSP above")
			}

			start := time.Now()
			fmt.Println("Compiling knowledge graph...")

			result, err := knowledge.Compile(cwd)
			if err != nil {
				return exitError(1, "compile failed: %v", err)
			}

			if err := knowledge.Save(cwd, result.Graph, result.Health); err != nil {
				return exitError(1, "saving knowledge: %v", err)
			}

			elapsed := time.Since(start)
			fmt.Printf("\nKnowledge compiled in %s:\n", elapsed.Round(time.Millisecond))
			fmt.Printf("  Packages:  %d\n", result.Health.PackageCount)
			fmt.Printf("  Functions: %d\n", result.Health.FunctionCount)
			fmt.Printf("  Types:     %d\n", result.Health.TypeCount)
			fmt.Printf("  Stored:    %s\n", knowledge.KnowledgeDir)

			return nil
		},
	}

	cmd.Flags().BoolVar(&soft, "soft", false, "Use LLM analysis as fallback when LSP unavailable")
	return cmd
}

func lspInstallHint(l knowledge.DetectedLang) string {
	switch l.Name {
	case "python":
		return "pip install pyright  (or)  npm i -g pyright"
	case "typescript":
		return "npm i -g typescript-language-server typescript"
	case "rust":
		return "rustup component add rust-analyzer"
	case "java":
		return "install Eclipse JDT LS (jdtls)"
	case "ruby":
		return "gem install ruby-lsp"
	case "cpp":
		return "brew install llvm  (includes clangd)"
	default:
		return "install " + l.LSP
	}
}

func newKnowledgeQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "query <file-or-package>",
		Short:         "Show relevant knowledge for a file or package",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return exitError(1, "getting working directory: %v", err)
			}

			graph, err := knowledge.Load(cwd)
			if err != nil {
				return exitError(1, "loading knowledge: %v (run 'baton knowledge compile' first)", err)
			}

			modPath, _ := readModPath(cwd) // empty for non-Go projects

			output := knowledge.Inject(graph, args, modPath, knowledge.DefaultTokenBudget)
			if output == "" {
				fmt.Println("No knowledge found for the given files.")
				return nil
			}

			fmt.Print(output)
			return nil
		},
	}
}

func newKnowledgeHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "health",
		Short:         "Show knowledge graph health and staleness",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return exitError(1, "getting working directory: %v", err)
			}

			health, err := knowledge.LoadHealth(cwd)
			if err != nil {
				return exitError(1, "loading health: %v (run 'baton knowledge compile' first)", err)
			}

			graph, err := knowledge.Load(cwd)
			if err != nil {
				return exitError(1, "loading knowledge: %v", err)
			}

			stale := graph.StalePackages()

			fmt.Printf("Knowledge Graph Health:\n")
			fmt.Printf("  Compiled:  %s\n", health.CompiledAt.Format(time.RFC3339))
			fmt.Printf("  Packages:  %d\n", health.PackageCount)
			fmt.Printf("  Functions: %d\n", health.FunctionCount)
			fmt.Printf("  Types:     %d\n", health.TypeCount)
			fmt.Printf("  Stale:     %d\n", len(stale))

			if len(stale) > 0 {
				fmt.Println("\nStale packages (source changed since compile):")
				for _, s := range stale {
					fmt.Printf("  - %s\n", s)
				}
				fmt.Println("\nRun 'baton knowledge compile' to refresh.")
			}

			return nil
		},
	}
}

func readModPath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range splitLines(string(data)) {
		if len(line) > 7 && line[:7] == "module " {
			return line[7:], nil
		}
	}
	return "", fmt.Errorf("module not found")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
