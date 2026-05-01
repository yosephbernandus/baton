package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/ansi"
	"github.com/yosephbernandus/baton/internal/brief"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/decisions"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

var respondableStatuses = map[string]bool{
	"needs_clarification": true,
	"needs_human":         true,
	"deferred":            true,
}

func NewRespondCmd() *cobra.Command {
	var (
		answer    string
		answeredBy string
		reason    string
		resume    bool
		deferTask bool
	)

	cmd := &cobra.Command{
		Use:           "respond <task-id>",
		Short:         "Respond to a blocked task with an answer",
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

			emitter, err := events.NewEmitter(cfg.EventLog)
			if err != nil {
				return exitError(1, "creating event emitter: %v", err)
			}

			t, err := store.Get(taskID)
			if err != nil {
				return exitError(1, "task not found: %v", err)
			}

			if !respondableStatuses[t.Status] {
				return exitError(1, "task %s has status %q, expected needs_clarification/needs_human/deferred", taskID, t.Status)
			}

			now := time.Now().UTC()
			t.Response = &task.Response{
				Answer:     answer,
				AnsweredBy: answeredBy,
				Reason:     reason,
				Timestamp:  now,
			}

			if answeredBy == "human" {
				t.Escalation.HumanDecision = answer
				t.Escalation.HumanReason = reason
			} else {
				t.Escalation.OrchestratorAnalysis = answer
			}

			_ = emitter.TaskEvent(taskID, t.Runtime, t.Model, "", "task_responded", map[string]interface{}{
				"answer":      answer,
				"answered_by": answeredBy,
				"reason":      reason,
			})

			if t.Escalation.WorkerClarification != "" {
				if dstore, err := decisions.NewStore(cfg.ResultDir); err == nil {
					_ = dstore.Append(decisions.Record{
						TaskID:    taskID,
						Question:  t.Escalation.WorkerClarification,
						Answer:    answer,
						Reason:    reason,
						DecidedBy: answeredBy,
						Timestamp: now,
					})
				}
			}

			if deferTask {
				t.Status = "deferred"
				_ = emitter.TaskEvent(taskID, t.Runtime, t.Model, "", "task_deferred", map[string]interface{}{
					"reason": reason,
				})
				_ = store.Update(t)
				fmt.Printf("deferred %s\n", taskID)
				return nil
			}

			if !resume {
				t.Status = "pending"
				_ = store.Update(t)
				fmt.Printf("responded to %s (not resumed, use --resume to re-dispatch)\n", taskID)
				return nil
			}

			attemptNum := len(t.Attempts) + 1
			t.Status = "running"
			t.StartedAt = &now
			t.CompletedAt = nil
			t.Attempts = append(t.Attempts, task.Attempt{
				Attempt:   attemptNum,
				StartedAt: now,
				Status:    "running",
			})
			_ = store.Update(t)

			_ = emitter.TaskEvent(taskID, t.Runtime, t.Model, "", "task_redispatched", map[string]interface{}{
				"attempt":        attemptNum,
				"answer_context": answer,
			})

			var prompt string
			if t.Spec != nil {
				projectBrief := brief.Load(cfg.ProjectBrief)
				prompt = spec.BuildPromptWithResponse(t.Spec, projectBrief, t.Escalation.WorkerClarification, answer, reason)
			} else {
				prompt = fmt.Sprintf("Previous attempt asked: %s\nAnswer: %s\nReason: %s\nProceed.", t.Escalation.WorkerClarification, answer, reason)
			}

			timeout, err := time.ParseDuration(cfg.DefaultTimeout)
			if err != nil {
				timeout = 10 * time.Minute
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			r := runner.New(cfg, emitter, store)
			result, err := r.Run(ctx, taskID, t.Runtime, t.Model, prompt, t.Spec, timeout)
			if err != nil {
				t.Status = "failed"
				t.Error = err.Error()
				_ = store.Update(t)
				return exitError(1, "runner error: %v", err)
			}

			current, _ := store.Get(taskID)
			if current != nil && current.Status == "killed" {
				return nil
			}

			completedAt := time.Now().UTC()
			t.Status = result.Status
			t.CompletedAt = &completedAt
			t.ExitCode = &result.ExitCode
			t.Duration = result.Duration.Round(time.Second).String()
			t.FilesChanged = result.FilesChanged
			tailN := cfg.OutputTailLines
			if tailN <= 0 {
				tailN = 50
			}
			if len(result.Output) > tailN {
				t.OutputTail = ansi.StripLines(result.Output[len(result.Output)-tailN:])
			} else {
				t.OutputTail = ansi.StripLines(result.Output)
			}
			if len(t.Attempts) > 0 {
				t.Attempts[len(t.Attempts)-1].CompletedAt = &completedAt
				t.Attempts[len(t.Attempts)-1].Status = result.Status
			}
			_ = store.Update(t)

			switch result.Status {
			case "completed":
				fmt.Fprintf(os.Stdout, "task %s completed in %s (attempt %d)\n", taskID, t.Duration, attemptNum)
			case "failed":
				fmt.Fprintf(os.Stderr, "task %s failed (attempt %d, exit code %d)\n", taskID, attemptNum, result.ExitCode)
			case "needs_clarification":
				fmt.Fprintf(os.Stdout, "task %s needs clarification again: %s\n", taskID, result.Clarification)
			default:
				fmt.Fprintf(os.Stdout, "task %s ended with status: %s\n", taskID, result.Status)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&answer, "answer", "", "response to the worker's question")
	cmd.Flags().StringVar(&answeredBy, "by", "human", "who answered: human or orchestrator")
	cmd.Flags().StringVar(&reason, "reason", "", "reasoning for the answer")
	cmd.Flags().BoolVar(&resume, "resume", false, "re-dispatch the task with the answer")
	cmd.Flags().BoolVar(&deferTask, "defer", false, "park the task for later")
	_ = cmd.MarkFlagRequired("answer")
	return cmd
}
