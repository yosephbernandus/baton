package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewDeferCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:           "defer <task-id>",
		Short:         "Park a blocked task until you are ready",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "opening task store: %v", err)
			}

			t, err := store.Get(taskID)
			if err != nil {
				return exitError(1, "task not found: %v", err)
			}

			if t.Status != "needs_clarification" && t.Status != "needs_human" {
				return exitError(1, "task %s has status %q, can only defer needs_clarification/needs_human", taskID, t.Status)
			}

			t.Status = "deferred"
			now := time.Now().UTC()
			t.Response = &task.Response{
				Answer:     "deferred",
				AnsweredBy: "human",
				Reason:     reason,
				Timestamp:  now,
			}
			if err := store.Update(t); err != nil {
				return exitError(1, "updating task: %v", err)
			}

			if emitter, err := events.NewEmitter(cfg.EventLog); err == nil {
				_ = emitter.TaskEvent(taskID, t.Runtime, t.Model, "", "task_deferred", map[string]interface{}{
					"reason": reason,
				})
			}

			fmt.Printf("deferred %s\n", taskID)
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "why you are deferring")
	return cmd
}
