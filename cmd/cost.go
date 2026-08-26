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
	var byTask bool

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
			fmt.Printf("Estimated total: $%.4f\n", summary.TotalEstimate)

			// Say how much of that figure was counted rather than inferred.
			// A total that mixes both without distinguishing them reads as
			// precision it does not have.
			switch {
			case summary.MeasuredTasks == summary.TotalTasks && summary.TotalTasks > 0:
				fmt.Printf("  all %d priced from reported tokens\n", summary.MeasuredTasks)
			case summary.MeasuredTasks > 0:
				fmt.Printf("  $%.4f from reported tokens (%d of %d tasks)\n",
					summary.MeasuredEstimate, summary.MeasuredTasks, summary.TotalTasks)
				fmt.Printf("  $%.4f inferred from elapsed time (%d tasks reported no tokens)\n",
					summary.TotalEstimate-summary.MeasuredEstimate,
					summary.TotalTasks-summary.MeasuredTasks)
			case summary.TotalTasks > 0:
				fmt.Println("  all inferred from elapsed time; no runtime reported tokens")
			}

			if summary.InputTokens > 0 || summary.OutputTokens > 0 {
				fmt.Printf("Tokens: %d in, %d out", summary.InputTokens, summary.OutputTokens)
				if rate := summary.CacheRate(); rate >= 0 {
					fmt.Printf(" + %d cached reads (%.1f%% of all input)",
						summary.CachedTokens, rate*100)
				}
				fmt.Println()
			}
			fmt.Println()

			if byTask {
				entries, err := tracker.ReadAll()
				if err != nil {
					return exitError(1, "reading costs: %v", err)
				}
				if len(entries) == 0 {
					fmt.Println("No recorded tasks.")
					return nil
				}

				// Per task, so a pipeline's phases can be read in order. The
				// cache column is what says whether re-priming each phase is
				// actually costing anything.
				fmt.Println("By task:")
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "  TASK\tMODEL\tSOURCE\tIN\tOUT\tCACHE\tCOST")
				for _, e := range entries {
					cacheCol := "-"
					if rate := e.CacheRate(); rate >= 0 {
						cacheCol = fmt.Sprintf("%.1f%%", rate*100)
					}
					in, out := 0, 0
					if e.Usage != nil {
						in, out = e.Usage.InputTokens, e.Usage.OutputTokens
					}
					_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%d\t%d\t%s\t$%.4f\n",
						e.TaskID, e.Model, e.Source, in, out, cacheCol, e.Estimate)
				}
				_ = w.Flush()
				fmt.Println()
			}

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
	cmd.Flags().BoolVar(&byTask, "by-task", false, "list every recorded task, in order, with its cache hit rate")
	return cmd
}
