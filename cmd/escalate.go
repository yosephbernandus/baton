package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewEscalateCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:           "escalate <task-id>",
		Short:         "Escalate a blocked task to human review",
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

			if t.Status != "needs_clarification" && t.Status != "running" {
				return exitError(1, "task %s has status %q, can only escalate needs_clarification/running", taskID, t.Status)
			}

			t.Status = "needs_human"
			t.Escalation.OrchestratorAnalysis = reason
			now := time.Now().UTC()

			if err := store.Update(t); err != nil {
				return exitError(1, "updating task: %v", err)
			}

			if emitter, err := events.NewEmitter(cfg.EventLog); err == nil {
				_ = emitter.TaskEvent(taskID, t.Runtime, t.Model, "", "needs_human", map[string]interface{}{
					"clarification":         t.Escalation.WorkerClarification,
					"orchestrator_analysis": reason,
					"timestamp":             now,
				})
			}

			fmt.Printf("escalated %s to human review\n", taskID)
			if t.Escalation.WorkerClarification != "" {
				fmt.Printf("question: %s\n", t.Escalation.WorkerClarification)
			}
			if reason != "" {
				fmt.Printf("analysis: %s\n", reason)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "orchestrator's analysis of why it cannot answer")
	return cmd
}
