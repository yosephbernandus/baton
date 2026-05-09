package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/annealing"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/feedback"
	"gopkg.in/yaml.v3"
)

func NewAnnealCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "anneal",
		Short: "Generate config patches from feedback analysis",
	}

	cmd.AddCommand(newAnnealGenerateCmd())
	cmd.AddCommand(newAnnealListCmd())
	cmd.AddCommand(newAnnealHistoryCmd())

	return cmd
}

func newAnnealGenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "generate",
		Short:         "Analyze feedback and generate config patches",
		Aliases:       []string{"gen"},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			window := 7 * 24 * time.Hour
			if cfg.Feedback.AnalysisWindow != "" {
				if d, err := time.ParseDuration(cfg.Feedback.AnalysisWindow); err == nil {
					window = d
				}
			}

			minOcc := cfg.Feedback.MinOccurrences
			if minOcc <= 0 {
				minOcc = 3
			}

			miner := feedback.NewMiner(cfg.EventLog, window, minOcc)
			analysis, err := miner.Analyze()
			if err != nil {
				return exitError(1, "analysis error: %v", err)
			}

			if len(analysis.Patterns) == 0 {
				fmt.Fprintf(os.Stderr, "No actionable patterns found. Nothing to patch.\n")
				return nil
			}

			annealer := annealing.New(annealing.Config{
				MinConfidence:    cfg.Annealing.MinConfidence,
				AutoApply:        cfg.Annealing.AutoApply,
				AutoApplyMaxRisk: cfg.Annealing.AutoApplyMaxRisk,
				PatchDir:         cfg.Annealing.PatchDir,
			})

			pf, err := annealer.GeneratePatches(analysis)
			if err != nil {
				return exitError(1, "generating patches: %v", err)
			}

			fmt.Fprintf(os.Stderr, "Generated %d patches:\n", len(pf.Patches))
			for _, p := range pf.Patches {
				fmt.Fprintf(os.Stderr, "  %s [%s/%s] %s\n", p.ID, p.Confidence, p.Risk, p.Description)
				fmt.Fprintf(os.Stderr, "    target: %s → %s\n", p.TargetFile, p.TargetPath)
				fmt.Fprintf(os.Stderr, "    rationale: %s\n", p.Rationale)
			}

			eligible := annealer.AutoApplyEligible(pf.Patches)
			if len(eligible) > 0 {
				fmt.Fprintf(os.Stderr, "\n%d patches eligible for auto-apply (confidence+risk criteria met).\n", len(eligible))
				if cfg.Annealing.AutoApply {
					fmt.Fprintf(os.Stderr, "Auto-apply is enabled but not yet implemented. Review patches manually.\n")
				} else {
					fmt.Fprintf(os.Stderr, "Auto-apply is disabled. Review and apply manually.\n")
				}
			}

			return nil
		},
	}
}

func newAnnealListCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "List pending patches",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			annealer := annealing.New(annealing.Config{
				PatchDir: cfg.Annealing.PatchDir,
			})

			pf, err := annealer.LoadPatches()
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No patches generated yet. Run: baton anneal generate")
					return nil
				}
				return exitError(1, "loading patches: %v", err)
			}

			pending := 0
			for _, p := range pf.Patches {
				if !p.Applied {
					pending++
					fmt.Fprintf(os.Stderr, "%s [%s/%s] %s\n", p.ID, p.Confidence, p.Risk, p.Description)
					fmt.Fprintf(os.Stderr, "  → %s: %s\n", p.TargetPath, p.Rationale)
				}
			}

			if pending == 0 {
				fmt.Println("No pending patches.")
			}

			return nil
		},
	}
}

func newAnnealHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "history",
		Short:         "Show all patches (pending, applied, reverted)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			annealer := annealing.New(annealing.Config{
				PatchDir: cfg.Annealing.PatchDir,
			})

			pf, err := annealer.LoadPatches()
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No patch history.")
					return nil
				}
				return exitError(1, "loading patches: %v", err)
			}

			data, _ := yaml.Marshal(pf)
			fmt.Print(string(data))
			return nil
		},
	}
}
