package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/brief"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/lock"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewRunCmd() *cobra.Command {
	var (
		runtimeFlag    string
		modelFlag      string
		specFlag       string
		promptFlag     string
		taskIDFlag     string
		contextFiles   string
		skipValidation bool
		timeoutFlag    string
		jsonOutput     bool
	)

	cmd := &cobra.Command{
		Use:           "run",
		Short:         "Spawn a task in an external runtime",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			runtimeName, model := cfg.ResolveRuntime(runtimeFlag, modelFlag)
			if err := cfg.ValidateRuntime(runtimeName, model); err != nil {
				return exitError(2, "%v", err)
			}

			if specFlag == "" && promptFlag == "" {
				return exitError(3, "either --spec or --prompt required")
			}

			var s *spec.Spec
			var prompt string

			if specFlag != "" {
				s, err = spec.Load(specFlag)
				if err != nil {
					return exitError(3, "loading spec: %v", err)
				}

				if !skipValidation {
					if errs := spec.Validate(s); len(errs) > 0 {
						var msgs []string
						for _, e := range errs {
							msgs = append(msgs, e.Error())
						}
						return exitError(3, "spec validation failed:\n  %s", strings.Join(msgs, "\n  "))
					}
				}

				projectBrief := brief.Load(cfg.ProjectBrief)
				prompt = spec.BuildPrompt(s, projectBrief)
			} else {
				if !skipValidation {
					return exitError(3, "--prompt requires --skip-validation")
				}
				prompt = promptFlag
				if contextFiles != "" {
					s = &spec.Spec{
						ContextFiles: strings.Split(contextFiles, ","),
					}
				}
			}

			taskID := taskIDFlag
			if taskID == "" {
				taskID = fmt.Sprintf("task-%d", time.Now().Unix())
			}

			emitter, err := events.NewEmitter(cfg.EventLog)
			if err != nil {
				return exitError(1, "creating event emitter: %v", err)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "creating task store: %v", err)
			}

			now := time.Now().UTC()
			t := &task.Task{
				ID:           taskID,
				Runtime:      runtimeName,
				Model:        model,
				Status:       "pending",
				DispatchedBy: cfg.Orchestrator.Runtime + "/" + cfg.Orchestrator.Model,
				Spec:         s,
				CreatedAt:    now,
				Attempts: []task.Attempt{
					{Attempt: 1, StartedAt: now, Status: "running"},
				},
			}

			if err := store.Create(t); err != nil {
				return exitError(1, "creating task record: %v", err)
			}

			emitter.TaskEvent(taskID, runtimeName, model, t.DispatchedBy, "task_created", map[string]interface{}{
				"spec_summary": truncate(strings.TrimSpace(prompt), 200),
			})

			timeout, err := time.ParseDuration(timeoutFlag)
			if err != nil {
				timeout, _ = time.ParseDuration(cfg.DefaultTimeout)
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			lockReg := lock.NewRegistry(cfg.LockFile)
			if s != nil && len(s.WritesTo) > 0 {
				conflicts, err := lockReg.Check(s.WritesTo)
				if err != nil {
					return exitError(1, "checking locks: %v", err)
				}
				if len(conflicts) > 0 {
					var msgs []string
					for _, c := range conflicts {
						msgs = append(msgs, c.String())
					}
					return exitError(4, "lock conflict: %s", strings.Join(msgs, "; "))
				}
				if err := lockReg.Acquire(taskID, s.WritesTo); err != nil {
					return exitError(4, "acquiring locks: %v", err)
				}
				emitter.TaskEvent(taskID, runtimeName, model, "", "lock_acquired", map[string]interface{}{
					"paths": s.WritesTo,
				})
			}

			t.Status = "running"
			t.StartedAt = &now
			store.Update(t)

			r := runner.New(cfg, emitter, store)
			result, err := r.Run(ctx, taskID, runtimeName, model, prompt, s, timeout)

			if s != nil && len(s.WritesTo) > 0 {
				lockReg.Release(taskID)
				emitter.TaskEvent(taskID, runtimeName, model, "", "lock_released", map[string]interface{}{
					"paths": s.WritesTo,
				})
			}

			if err != nil {
				t.Status = "failed"
				t.Error = err.Error()
				store.Update(t)
				return exitError(1, "runner error: %v", err)
			}

			completedAt := time.Now().UTC()
			t.Status = result.Status
			t.CompletedAt = &completedAt
			t.ExitCode = &result.ExitCode
			t.Duration = result.Duration.Round(time.Second).String()
			t.FilesChanged = result.FilesChanged
			if result.Clarification != "" {
				t.Escalation.WorkerClarification = result.Clarification
			}
			if len(t.Attempts) > 0 {
				t.Attempts[0].CompletedAt = &completedAt
				t.Attempts[0].Status = result.Status
			}
			store.Update(t)

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.Encode(t)
			}

			switch result.Status {
			case "completed":
				if !jsonOutput {
					fmt.Fprintf(os.Stdout, "task %s completed in %s\n", taskID, t.Duration)
				}
				return nil
			case "failed":
				msg := fmt.Sprintf("task %s failed (exit code %d)", taskID, result.ExitCode)
				if len(result.ChecksFailed) > 0 {
					msg += fmt.Sprintf("\nacceptance checks failed: %s", strings.Join(result.ChecksFailed, ", "))
				}
				return exitError(1, "%s", msg)
			case "needs_clarification":
				if !jsonOutput {
					fmt.Fprintf(os.Stdout, "task %s needs clarification: %s\n", taskID, result.Clarification)
				}
				return exitError(10, "")
			case "timeout":
				return exitError(124, "task %s timed out after %s", taskID, timeoutFlag)
			default:
				return exitError(1, "task %s ended with status: %s", taskID, result.Status)
			}
		},
	}

	cmd.Flags().StringVar(&runtimeFlag, "runtime", "", "runtime key from agents.yaml")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model from runtime's model list")
	cmd.Flags().StringVar(&specFlag, "spec", "", "path to task spec YAML file")
	cmd.Flags().StringVar(&promptFlag, "prompt", "", "inline prompt (requires --skip-validation)")
	cmd.Flags().StringVar(&taskIDFlag, "task-id", "", "task identifier (default: task-{timestamp})")
	cmd.Flags().StringVar(&contextFiles, "context-files", "", "comma-separated context files (with --prompt)")
	cmd.Flags().BoolVar(&skipValidation, "skip-validation", false, "skip spec validation")
	cmd.Flags().StringVar(&timeoutFlag, "timeout", "10m", "max time before killing worker")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output task record as JSON")

	return cmd
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
