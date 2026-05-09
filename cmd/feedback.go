package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/feedback"
	"gopkg.in/yaml.v3"
)

func NewFeedbackCmd() *cobra.Command {
	var (
		windowFlag string
		jsonFlag   bool
	)

	cmd := &cobra.Command{
		Use:           "feedback",
		Short:         "Analyze event log for performance patterns",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			window := 7 * 24 * time.Hour
			if windowFlag != "" {
				if d, err := time.ParseDuration(windowFlag); err == nil {
					window = d
				}
			} else if cfg.Feedback.AnalysisWindow != "" {
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

			if cfg.Feedback.OutputPath != "" {
				data, _ := yaml.Marshal(analysis)
				_ = os.MkdirAll(".baton/feedback", 0o755)
				_ = os.WriteFile(cfg.Feedback.OutputPath, data, 0o644)
			}

			if jsonFlag {
				data, _ := json.MarshalIndent(analysis, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Fprintf(os.Stderr, "--- Feedback Analysis ---\n")
			fmt.Fprintf(os.Stderr, "Window: %s to %s\n", analysis.EventWindow.From.Format(time.RFC3339), analysis.EventWindow.To.Format(time.RFC3339))
			fmt.Fprintf(os.Stderr, "Events: %d | Tasks: %d\n\n", analysis.EventWindow.TotalEvents, analysis.EventWindow.TotalTasks)

			if len(analysis.RuntimePerformance) > 0 {
				fmt.Fprintf(os.Stderr, "Runtime Performance:\n")
				for name, rm := range analysis.RuntimePerformance {
					fmt.Fprintf(os.Stderr, "  %s: %d tasks, %.0f%% success, %.1f avg retries\n",
						name, rm.Tasks, rm.SuccessRate*100, rm.AvgRetries)
				}
				fmt.Fprintln(os.Stderr)
			}

			if len(analysis.PhaseMetrics) > 0 {
				fmt.Fprintf(os.Stderr, "Phase Metrics:\n")
				for name, pm := range analysis.PhaseMetrics {
					fmt.Fprintf(os.Stderr, "  %s: %d runs, %.1f avg retries, %d loops, %d timeouts\n",
						name, pm.TotalRuns, pm.AvgRetries, pm.LoopDetections, pm.Timeouts)
				}
				fmt.Fprintln(os.Stderr)
			}

			if len(analysis.Patterns) > 0 {
				fmt.Fprintf(os.Stderr, "Detected Patterns:\n")
				for _, p := range analysis.Patterns {
					fmt.Fprintf(os.Stderr, "  [%s] %s (%s, %d occurrences)\n",
						p.Confidence, p.Description, p.Type, p.Occurrences)
					fmt.Fprintf(os.Stderr, "    → %s\n", p.Suggestion)
				}
			} else {
				fmt.Fprintf(os.Stderr, "No actionable patterns detected.\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&windowFlag, "window", "", "Analysis window (e.g., 168h, 720h)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")

	return cmd
}
