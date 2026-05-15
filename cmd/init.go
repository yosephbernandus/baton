package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/coordinator"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/session"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/worker"
)

func NewInitCmd() *cobra.Command {
	var (
		complexity string
		mode       string
	)

	cmd := &cobra.Command{
		Use:           "init <spec.yaml>",
		Short:         "Initialize a task and generate coordinator or worker instructions",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath := args[0]

			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			s, err := spec.Load(specPath)
			if err != nil {
				return exitError(3, "loading spec: %v", err)
			}

			if errs := spec.Validate(s); len(errs) > 0 {
				for _, e := range errs {
					fmt.Fprintf(os.Stderr, "  %s\n", e.Error())
				}
				return exitError(3, "spec validation failed")
			}

			if complexity == "" {
				complexity = s.EstimatedComplexity
			}
			if complexity == "" {
				complexity = cfg.PhaseMachine.ComplexityDefault
			}
			if complexity == "" {
				complexity = phase.ComplexityMedium
			}
			if !phase.ValidComplexity(complexity) {
				return exitError(3, "invalid complexity: %s", complexity)
			}

			switch mode {
			case "coordinator":
				return initCoordinator(cfg, s, specPath, complexity)
			case "worker":
				return initWorker(cfg, specPath, complexity)
			case "headless":
				fmt.Println("For headless mode, use: baton pipeline run", specPath)
				return nil
			default:
				return exitError(2, "unknown mode: %s (use coordinator, worker, or headless)", mode)
			}
		},
	}

	cmd.Flags().StringVar(&complexity, "complexity", "", "Override complexity (TRIVIAL, SMALL, MEDIUM, LARGE)")
	cmd.Flags().StringVar(&mode, "mode", "coordinator", "Execution mode: coordinator, worker, or headless")
	return cmd
}

func initCoordinator(cfg *config.Config, s *spec.Spec, specPath, complexity string) error {
	specBase := filepath.Base(specPath)
	ext := filepath.Ext(specBase)
	taskID := specBase[:len(specBase)-len(ext)]

	taskDir := filepath.Join(cfg.TaskDir, taskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return exitError(1, "creating task dir: %v", err)
	}

	// Save worker state for baton worker commands
	batonBin := cfg.WorkerProtocol.BatonBinary
	if batonBin == "" {
		batonBin = "baton"
	}

	phases := phase.DefaultPhases()
	active := phase.ActivePhases(phases, complexity)
	if len(active) == 0 {
		return exitError(3, "no active phases for complexity %s", complexity)
	}

	firstPhase := active[0]

	// Write worker state
	wsData := fmt.Sprintf(`task_id: %s
phase: %d
phase_name: %s
role: %s
state: idle
complexity: %s
worker_pid: %d
`, taskID, firstPhase.ID, firstPhase.Name, firstPhase.Role, complexity, os.Getpid())

	if err := os.WriteFile(filepath.Join(taskDir, "worker-state.yaml"), []byte(wsData), 0o644); err != nil {
		return exitError(1, "writing worker state: %v", err)
	}

	// Save manifest
	manifest := session.New(taskID, specPath, complexity)
	skipped := phase.SkippedPhaseIDs(phases, complexity)
	manifest.SetSkipped(skipped)
	if err := manifest.Save(filepath.Join(taskDir, "manifest.yaml")); err != nil {
		return exitError(1, "saving manifest: %v", err)
	}

	// Generate coordinator instructions
	ccfg := coordinator.CoordinatorConfig{
		TaskID:     taskID,
		Spec:       s,
		Complexity: complexity,
		BatonBin:   batonBin,
		Config:     cfg,
	}
	instructions := coordinator.GenerateCoordinatorInstructions(ccfg)

	instrPath := filepath.Join(taskDir, "CLAUDE.md")
	if err := os.WriteFile(instrPath, []byte(instructions), 0o644); err != nil {
		return exitError(1, "writing instructions: %v", err)
	}

	fmt.Printf("Task initialized: %s\n", taskID)
	fmt.Printf("Mode: coordinator\n")
	fmt.Printf("Complexity: %s (%d active phases)\n", complexity, len(active))
	fmt.Printf("Instructions: %s\n", instrPath)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Copy instructions to project root: cp %s CLAUDE.md\n", instrPath)
	fmt.Printf("  2. Start Claude Code in the project directory\n")
	fmt.Printf("  3. Claude Code reads CLAUDE.md and follows the coordinator protocol\n")

	return nil
}

func initWorker(cfg *config.Config, specPath, complexity string) error {
	taskID, instrPath, err := worker.Init(cfg, specPath, complexity, "generic")
	if err != nil {
		return exitError(1, "initializing worker: %v", err)
	}

	fmt.Printf("Task initialized: %s\n", taskID)
	fmt.Printf("Mode: worker\n")
	fmt.Printf("Instructions: %s\n", instrPath)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Start your agent and point it at the instructions\n")
	fmt.Printf("  2. Run: baton worker start %s\n", taskID)

	return nil
}
