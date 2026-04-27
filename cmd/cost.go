package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/cost"
)

func NewCostCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:           "cost",
		Short:         "Show cost tracking summary",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			tracker, err := cost.NewTracker(cfg.ResultDir)
			if err != nil {
				return exitError(1, "creating cost tracker: %v", err)
			}

			summary, err := tracker.Summarize()
			if err != nil {
				return exitError(1, "reading costs: %v", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			}

			fmt.Printf("Total tasks: %d\n", summary.TotalTasks)
			fmt.Printf("Estimated total: $%.4f\n\n", summary.TotalEstimate)

			if len(summary.ByModel) > 0 {
				fmt.Println("By model:")
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				for model, est := range summary.ByModel {
					_, _ = fmt.Fprintf(w, "  %s\t$%.4f\n", model, est)
				}
				_ = w.Flush()
				fmt.Println()
			}

			if len(summary.ByRuntime) > 0 {
				fmt.Println("By runtime:")
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				for rt, est := range summary.ByRuntime {
					_, _ = fmt.Fprintf(w, "  %s\t$%.4f\n", rt, est)
				}
				_ = w.Flush()
				fmt.Println()
			}

			if len(summary.ByStatus) > 0 {
				fmt.Println("By status:")
				for status, count := range summary.ByStatus {
					fmt.Printf("  %s: %d\n", status, count)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
