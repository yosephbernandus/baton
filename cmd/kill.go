package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewKillCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:           "kill <task-id> [task-id...]",
		Short:         "Kill running tasks by ID",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("provide task IDs or use --all")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "opening task store: %v", err)
			}

			ids := args
			if all {
				tasks, err := store.List("running")
				if err != nil {
					return exitError(1, "listing tasks: %v", err)
				}
				pending, err := store.List("pending")
				if err != nil {
					return exitError(1, "listing tasks: %v", err)
				}
				tasks = append(tasks, pending...)
				for _, t := range tasks {
					ids = append(ids, t.ID)
				}
			}

			if len(ids) == 0 {
				fmt.Println("no running tasks to kill")
				return nil
			}

			var failed int
			for _, id := range ids {
				if err := store.KillTask(id); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "kill %s: %v\n", id, err)
					failed++
					continue
				}
				fmt.Printf("killed %s\n", id)
			}

			if failed > 0 {
				return exitError(1, "%d task(s) failed to kill", failed)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "kill all running/pending tasks")
	return cmd
}
