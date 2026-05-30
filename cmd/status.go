package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	bsync "github.com/yosephbernandus/baton/internal/sync"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewStatusCmd() *cobra.Command {
	var (
		jsonOutput bool
		filter     string
		recent     int
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

			if reaped, err := store.ReapDead(); err == nil && len(reaped) > 0 {
				for _, id := range reaped {
					fmt.Fprintf(cmd.OutOrStderr(), "reaped dead task: %s\n", id)
				}
			}

			taskN, subdirN, _ := bsync.ReconcileAll(cfg.EventLog, store, cfg.TaskDir)
			if taskN > 0 {
				fmt.Fprintf(cmd.OutOrStderr(), "reconciled %d tasks from event log\n", taskN)
			}
			if subdirN > 0 {
				fmt.Fprintf(cmd.OutOrStderr(), "reconciled %d stale pipeline manifests\n", subdirN)
			}

			var tasks []*task.Task
			if filter != "" {
				tasks, err = store.List(filter)
			} else {
				tasks, err = store.ListRecent(recent)
			}
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
	cmd.Flags().IntVar(&recent, "recent", 50, "limit to N most recent tasks")
	return cmd
}
