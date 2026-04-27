package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewStatusCmd() *cobra.Command {
	var (
		jsonOutput bool
		filter     string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show all active/completed tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "opening task store: %v", err)
			}

			tasks, err := store.List(filter)
			if err != nil {
				return exitError(1, "listing tasks: %v", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tasks)
			}

			if len(tasks) == 0 {
				fmt.Println("no tasks found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "TASK ID\tRUNTIME\tMODEL\tSTATUS\tDURATION")
			for _, t := range tasks {
				duration := t.Duration
				if duration == "" {
					duration = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Runtime, t.Model, t.Status, duration)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().StringVar(&filter, "filter", "", "filter by status")
	return cmd
}
