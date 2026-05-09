package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/session"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Run a multi-phase pipeline for a task spec",
	}

	cmd.AddCommand(newPipelineRunCmd())
	cmd.AddCommand(newPipelineStatusCmd())
	return cmd
}

func newPipelineRunCmd() *cobra.Command {
	var (
		complexityFlag string
	)

	cmd := &cobra.Command{
		Use:           "run <spec.yaml>",
		Short:         "Execute a 16-phase pipeline for a task spec",
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
				var msgs []string
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				return exitError(3, "spec validation failed:\n  %s", strings.Join(msgs, "\n  "))
			}

			complexity := resolveComplexity(complexityFlag, s.EstimatedComplexity, cfg.PhaseMachine.ComplexityDefault)
			if !phase.ValidComplexity(complexity) {
				return exitError(3, "invalid complexity %q, must be TRIVIAL|SMALL|MEDIUM|LARGE", complexity)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "creating task store: %v", err)
			}

			emitter, _ := events.NewEmitter(cfg.EventLog)

			r := runner.New(cfg, emitter, store)

			specID := strings.TrimSuffix(filepath.Base(specPath), filepath.Ext(specPath))

			p := phase.NewPipeline(cfg, r, store, emitter, s, specID, phase.PipelineConfig{
				Complexity: complexity,
			})

			manifestPath := filepath.Join(".baton", "session.yaml")
			sessionID := fmt.Sprintf("%s-%d", specID, time.Now().Unix())
			manifest := session.New(sessionID, specPath, complexity)
			p.SetManifest(manifest, manifestPath)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			allPhases := phase.DefaultPhases()
			active := phase.ActivePhases(allPhases, complexity)
			skipped := phase.SkippedPhaseIDs(allPhases, complexity)

			fmt.Fprintf(os.Stderr, "Pipeline: %s (complexity: %s)\n", specID, complexity)
			fmt.Fprintf(os.Stderr, "Phases: %d active, %d skipped\n", len(active), len(skipped))
			fmt.Fprintf(os.Stderr, "Active: %s\n\n", phaseNames(active))

			start := time.Now()
			result, err := p.Run(ctx)
			if err != nil {
				return exitError(1, "pipeline error: %v", err)
			}

			fmt.Fprintf(os.Stderr, "\n--- Pipeline Result ---\n")
			fmt.Fprintf(os.Stderr, "Status:    %s\n", result.Status)
			fmt.Fprintf(os.Stderr, "Duration:  %s\n", result.Duration.Round(time.Second))
			fmt.Fprintf(os.Stderr, "Completed: %v\n", result.PhasesCompleted)
			fmt.Fprintf(os.Stderr, "Skipped:   %v\n", result.PhasesSkipped)
			if result.L2Cycles > 0 {
				fmt.Fprintf(os.Stderr, "L2 Cycles: %d\n", result.L2Cycles)
			}
			if result.FailedPhase != nil {
				fmt.Fprintf(os.Stderr, "Failed at: phase %d\n", *result.FailedPhase)
				fmt.Fprintf(os.Stderr, "Reason:    %s\n", result.FailReason)
			}
			for phaseID, attempts := range result.AttemptsByPhase {
				if attempts > 1 {
					fmt.Fprintf(os.Stderr, "  Phase %d: %d attempts\n", phaseID, attempts)
				}
			}

			_ = start // used via result.Duration

			switch result.Status {
			case "completed":
				return nil
			case "blocked":
				return exitError(10, "pipeline blocked: %s", result.FailReason)
			default:
				return exitError(1, "pipeline failed: %s", result.FailReason)
			}
		},
	}

	cmd.Flags().StringVar(&complexityFlag, "complexity", "", "Override task complexity (TRIVIAL|SMALL|MEDIUM|LARGE)")

	return cmd
}

func newPipelineStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show current pipeline status",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("No active pipeline.")
			return nil
		},
	}
}

func resolveComplexity(flag, specValue, configDefault string) string {
	if flag != "" {
		return flag
	}
	if specValue != "" {
		return specValue
	}
	if configDefault != "" {
		return configDefault
	}
	return phase.ComplexityMedium
}

func phaseNames(phases []phase.Phase) string {
	names := make([]string, len(phases))
	for i, p := range phases {
		names[i] = fmt.Sprintf("%d:%s", p.ID, p.Name)
	}
	return strings.Join(names, ", ")
}
