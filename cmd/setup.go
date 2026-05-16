package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type detectedRuntime struct {
	Name    string
	Command string
	Path    string
}

var knownRuntimes = []struct {
	Name    string
	Command string
}{
	{"claude-code", "claude"},
	{"opencode", "opencode"},
	{"aider", "aider"},
	{"pi-agent", "pi-agent"},
	{"codex", "codex"},
}

func NewSetupCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:           "setup",
		Short:         "Scaffold .baton/ directory with auto-detected runtimes",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing .baton/ files")
	return cmd
}

func runSetup(force bool) error {
	batonDir := ".baton"

	if !force {
		if _, err := os.Stat(filepath.Join(batonDir, "agents.yaml")); err == nil {
			return exitError(2, ".baton/agents.yaml already exists. Use --force to overwrite.")
		}
	}

	detected := detectRuntimes()
	if len(detected) == 0 {
		fmt.Println("No agent runtimes detected.")
		fmt.Println("Install at least one: opencode, claude (Claude Code), aider, pi-agent")
		return exitError(2, "no runtimes found")
	}

	fmt.Printf("Detected %d runtime(s):\n", len(detected))
	for _, rt := range detected {
		fmt.Printf("  ✓ %s (%s)\n", rt.Name, rt.Path)
	}
	fmt.Println()

	dirs := []string{
		batonDir,
		filepath.Join(batonDir, "specs"),
		filepath.Join(batonDir, "tasks"),
		filepath.Join(batonDir, "skills"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return exitError(1, "creating %s: %v", d, err)
		}
	}

	agentsYAML := generateAgentsYAML(detected)
	if err := os.WriteFile(filepath.Join(batonDir, "agents.yaml"), []byte(agentsYAML), 0o644); err != nil {
		return exitError(1, "writing agents.yaml: %v", err)
	}

	projectName := inferProjectName()
	brief := generateProjectBrief(projectName)
	briefPath := filepath.Join(batonDir, "project-brief.md")
	if force || !fileExists(briefPath) {
		if err := os.WriteFile(briefPath, []byte(brief), 0o644); err != nil {
			return exitError(1, "writing project-brief.md: %v", err)
		}
	}

	sampleSpec := generateSampleSpec()
	specPath := filepath.Join(batonDir, "specs", "example-task.yaml")
	if force || !fileExists(specPath) {
		if err := os.WriteFile(specPath, []byte(sampleSpec), 0o644); err != nil {
			return exitError(1, "writing sample spec: %v", err)
		}
	}

	if err := appendGitignore(); err != nil {
		fmt.Printf("Warning: could not update .gitignore: %v\n", err)
	}

	fmt.Println("Created:")
	fmt.Println("  .baton/agents.yaml          — runtime config")
	fmt.Println("  .baton/project-brief.md      — project context for workers")
	fmt.Println("  .baton/specs/example-task.yaml — sample task spec")
	fmt.Println("  .baton/specs/                — put task specs here")
	fmt.Println("  .baton/tasks/                — task state (managed by baton)")
	fmt.Println("  .baton/skills/               — domain context files")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit .baton/project-brief.md with your project details")
	fmt.Println("  2. Edit .baton/agents.yaml to configure models")
	fmt.Println("  3. Write a task spec:")
	fmt.Println("       cp .baton/specs/example-task.yaml .baton/specs/my-task.yaml")
	fmt.Println("  4. Run it:")
	fmt.Println("       baton init .baton/specs/my-task.yaml")
	fmt.Println()
	fmt.Println("Or use the coordinator flow:")
	fmt.Println("  baton init .baton/specs/my-task.yaml --complexity MEDIUM")
	fmt.Println("  cp .baton/tasks/<task-id>/<instructions> <instructions>")
	fmt.Println("  # Start your coordinator agent — it reads the instructions and orchestrates")
	fmt.Println()
	fmt.Println("Instructions file auto-detected from orchestrator.runtime in agents.yaml:")
	fmt.Println("  claude-code → CLAUDE.md | cursor → .cursorrules | windsurf → .windsurfrules | default → AGENTS.md")

	return nil
}

func detectRuntimes() []detectedRuntime {
	var found []detectedRuntime
	for _, rt := range knownRuntimes {
		path, err := exec.LookPath(rt.Command)
		if err == nil {
			found = append(found, detectedRuntime{
				Name:    rt.Name,
				Command: rt.Command,
				Path:    path,
			})
		}
	}
	return found
}

func generateAgentsYAML(runtimes []detectedRuntime) string {
	var b strings.Builder

	orchestrator := runtimes[0]
	for _, rt := range runtimes {
		if rt.Name == "claude-code" {
			orchestrator = rt
			break
		}
	}

	fmt.Fprintf(&b, "orchestrator:\n")
	fmt.Fprintf(&b, "  runtime: %s\n", orchestrator.Name)
	fmt.Fprintf(&b, "  model: auto\n\n")

	b.WriteString("runtimes:\n")
	for _, rt := range runtimes {
		fmt.Fprintf(&b, "  %s:\n", rt.Name)
		fmt.Fprintf(&b, "    command: \"%s\"\n", rt.Command)

		switch rt.Name {
		case "claude-code":
			b.WriteString("    prompt_mode: stdin\n")
			b.WriteString("    extra_flags:\n")
			b.WriteString("      - \"--print\"\n")
			b.WriteString("      - \"--dangerously-skip-permissions\"\n")
			b.WriteString("    models:\n")
			b.WriteString("      - claude-sonnet-4-6\n")
			b.WriteString("      - claude-opus-4-6\n")
		case "opencode":
			b.WriteString("    model_flag: \"-m\"\n")
			b.WriteString("    context_flag: \"--file\"\n")
			b.WriteString("    positional:\n")
			b.WriteString("      - \"run\"\n")
			b.WriteString("      - \"{{prompt}}\"\n")
			b.WriteString("    extra_flags:\n")
			b.WriteString("      - \"--dangerously-skip-permissions\"\n")
			b.WriteString("    models:\n")
			b.WriteString("      - kimi\n")
			b.WriteString("      - deepseek\n")
		case "aider":
			b.WriteString("    model_flag: \"--model\"\n")
			b.WriteString("    prompt_flag: \"--message\"\n")
			b.WriteString("    extra_flags:\n")
			b.WriteString("      - \"--yes\"\n")
			b.WriteString("      - \"--no-auto-commits\"\n")
			b.WriteString("    models:\n")
			b.WriteString("      - gpt-4o\n")
			b.WriteString("      - deepseek\n")
		case "pi-agent":
			b.WriteString("    model_flag: \"--model\"\n")
			b.WriteString("    prompt_flag: \"--prompt\"\n")
			b.WriteString("    context_flag: \"--context\"\n")
			b.WriteString("    models:\n")
			b.WriteString("      - gemini\n")
			b.WriteString("      - grok\n")
		case "codex":
			b.WriteString("    prompt_flag: \"--prompt\"\n")
			b.WriteString("    models:\n")
			b.WriteString("      - codex\n")
		}
		b.WriteString("\n")
	}

	if hasRuntime(runtimes, "claude-code") {
		b.WriteString("role_models:\n")
		b.WriteString("  lead: claude-code\n")
		b.WriteString("  reviewer: claude-code\n")
		b.WriteString("  test_lead: claude-code\n")
		worker := "claude-code"
		for _, rt := range runtimes {
			if rt.Name != "claude-code" {
				worker = rt.Name
				break
			}
		}
		fmt.Fprintf(&b, "  developer: %s\n", worker)
		fmt.Fprintf(&b, "  tester: %s\n", worker)
		b.WriteString("\n")
	}

	b.WriteString("defaults:\n")
	defaultRT := runtimes[0].Name
	if hasRuntime(runtimes, "opencode") {
		defaultRT = "opencode"
	}
	fmt.Fprintf(&b, "  runtime: %s\n", defaultRT)
	b.WriteString("  model: auto\n\n")

	b.WriteString("clarification_exit_code: 10\n")
	b.WriteString("clarification_patterns:\n")
	b.WriteString("  - \"CLARIFICATION_NEEDED\"\n")
	b.WriteString("  - \"I'm not sure\"\n\n")
	b.WriteString("event_log: \".baton/events.ndjson\"\n")
	b.WriteString("task_dir: \".baton/tasks\"\n")
	b.WriteString("default_timeout: \"10m\"\n")

	return b.String()
}

func generateProjectBrief(projectName string) string {
	var b strings.Builder
	b.WriteString("# Project Brief\n\n")
	fmt.Fprintf(&b, "Project: %s\n", projectName)
	b.WriteString("Language: TODO\n")
	b.WriteString("Framework: TODO\n\n")
	b.WriteString("## Conventions\n\n")
	b.WriteString("- TODO: Add your coding conventions here\n")
	b.WriteString("- TODO: Error handling patterns\n")
	b.WriteString("- TODO: Naming conventions\n\n")
	b.WriteString("## Important Notes for Workers\n\n")
	b.WriteString("- TODO: Add anything workers should know about this project\n")
	return b.String()
}

func generateSampleSpec() string {
	return `spec:
  what: |
    TODO: Describe what the worker should produce.

  why: |
    TODO: Explain why this task exists and what depends on it.

  constraints:
    - "TODO: What the worker must NOT do"

  context_files:
    - "TODO: Files the worker should read for context"

  acceptance_criteria:
    - "TODO: How to verify the work is correct"

  # Optional: automated checks after worker completes
  # acceptance_checks:
  #   - command: "go build ./..."
  #     expect_exit: 0
  #     description: "Code must compile"

  # Optional: files this task will modify (for lock conflicts)
  # writes_to:
  #   - path/to/file.go
`
}

func inferProjectName() string {
	dir, err := os.Getwd()
	if err != nil {
		return "my-project"
	}
	return filepath.Base(dir)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasRuntime(runtimes []detectedRuntime, name string) bool {
	for _, rt := range runtimes {
		if rt.Name == name {
			return true
		}
	}
	return false
}

func appendGitignore() error {
	entries := []string{
		".baton/tasks/",
		".baton/events.ndjson*",
		".baton/locks.yaml",
		".baton/results/",
		".baton/session.yaml",
		".baton/feedback/",
		".baton/annealing/",
	}

	existing := ""
	if data, err := os.ReadFile(".gitignore"); err == nil {
		existing = string(data)
	}

	var toAdd []string
	for _, entry := range entries {
		if !strings.Contains(existing, entry) {
			toAdd = append(toAdd, entry)
		}
	}

	if len(toAdd) == 0 {
		return nil
	}

	f, err := os.OpenFile(".gitignore", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if existing != "" && !strings.HasSuffix(existing, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	if _, err := f.WriteString("\n# Baton runtime data\n"); err != nil {
		return err
	}
	for _, entry := range toAdd {
		if _, err := fmt.Fprintf(f, "%s\n", entry); err != nil {
			return err
		}
	}
	return nil
}
