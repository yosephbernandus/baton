package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/worker"
)

func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Worker protocol commands — called by LLM agents to participate in baton pipeline",
	}

	cmd.AddCommand(newWorkerInitCmd())
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerNextCmd())
	cmd.AddCommand(newWorkerHeartbeatCmd())
	cmd.AddCommand(newWorkerProgressCmd())
	cmd.AddCommand(newWorkerStuckCmd())
	cmd.AddCommand(newWorkerCompleteCmd())
	cmd.AddCommand(newWorkerFailCmd())
	cmd.AddCommand(newWorkerObserveCmd())
	cmd.AddCommand(newWorkerContextCmd())
	cmd.AddCommand(newWorkerWatchCmd())
	cmd.AddCommand(newWorkerRetryCmd())
	cmd.AddCommand(newWorkerLoopbackCmd())
	cmd.AddCommand(newWorkerStatusCmd())
	return cmd
}

func newWorkerInitCmd() *cobra.Command {
	var (
		complexity string
		runtime    string
	)

	cmd := &cobra.Command{
		Use:           "init <spec.yaml>",
		Short:         "Initialize a task from a spec, generate worker instructions",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID, instrPath, err := worker.Init(cfg, args[0], complexity, runtime)
			if err != nil {
				return err
			}

			fmt.Printf("Task initialized: %s\n", taskID)
			fmt.Printf("Instructions: %s\n", instrPath)
			fmt.Printf("\nStart the worker agent and point it at the instructions file.\n")
			fmt.Printf("Then run: baton worker start %s\n", taskID)
			return nil
		},
	}

	cmd.Flags().StringVar(&complexity, "complexity", "", "Override complexity (TRIVIAL, SMALL, MEDIUM, LARGE)")
	cmd.Flags().StringVar(&runtime, "runtime", "generic", "Target runtime (claude-code, opencode, generic)")
	return cmd
}

func newWorkerStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "start <task-id>",
		Short:         "Register worker and get current phase info",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			ts, prompt, err := worker.Start(cfg, args[0])
			if err != nil {
				return err
			}

			worker.EmitEvent(cfg, ts.TaskID, "worker_started", map[string]interface{}{
				"phase": ts.Phase, "role": ts.Role, "pid": os.Getpid(),
			})

			fmt.Print(prompt)
			return nil
		},
	}
}

func newWorkerNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "next",
		Short:         "Advance to next phase",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			ts, prompt, err := worker.Next(cfg, args[0])
			if err != nil {
				return err
			}

			if prompt == "" {
				fmt.Println("Pipeline complete. All phases done.")
				worker.EmitEvent(cfg, ts.TaskID, "pipeline_completed", nil)
				return nil
			}

			worker.EmitEvent(cfg, ts.TaskID, "phase_advanced", map[string]interface{}{
				"phase": ts.Phase, "phase_name": ts.PhaseName, "role": ts.Role,
			})

			fmt.Print(prompt)
			return nil
		},
	}
}

func newWorkerHeartbeatCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "heartbeat <message>",
		Short:         "Record liveness signal",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if taskID == "" {
				return fmt.Errorf("no active task found; run 'baton worker start <task-id>' first")
			}

			msg := joinArgs(args)
			return worker.Heartbeat(cfg, taskID, msg)
		},
	}
}

func newWorkerProgressCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "progress <percent> <message>",
		Short:         "Record progress update",
		Args:          cobra.MinimumNArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			pct, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid percent: %v", err)
			}

			msg := joinArgs(args[1:])
			return worker.Progress(cfg, taskID, pct, msg)
		},
	}
}

func newWorkerStuckCmd() *cobra.Command {
	var timeout string

	cmd := &cobra.Command{
		Use:           "stuck <question>",
		Short:         "Signal worker is blocked, wait for guidance",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			dur := 60 * time.Second
			if timeout != "" {
				if d, err := time.ParseDuration(timeout); err == nil {
					dur = d
				}
			} else if cfg.WorkerProtocol.StuckTimeout != "" {
				if d, err := time.ParseDuration(cfg.WorkerProtocol.StuckTimeout); err == nil {
					dur = d
				}
			}

			question := joinArgs(args)
			fmt.Printf("Stuck: %s\nWaiting for guidance (timeout: %s)...\n", question, dur)

			guidance, err := worker.Stuck(cfg, taskID, question, dur)
			if err != nil {
				fmt.Printf("No guidance received: %v\n", err)
				os.Exit(10)
			}

			fmt.Printf("Guidance received: %s\n", guidance)
			return nil
		},
	}

	cmd.Flags().StringVar(&timeout, "timeout", "", "How long to wait for guidance (default: 60s)")
	return cmd
}

func newWorkerCompleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "complete",
		Short:         "Signal current phase is done",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if len(args) > 0 {
				taskID = args[0]
			}
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			if err := worker.Complete(cfg, taskID); err != nil {
				return err
			}

			worker.EmitEvent(cfg, taskID, "phase_completed_by_worker", nil)
			fmt.Println("Phase completed. Run 'baton worker next' to advance.")
			return nil
		},
	}
}

func newWorkerFailCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "fail <reason>",
		Short:         "Signal current phase failed",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			reason := joinArgs(args)
			if err := worker.Fail(cfg, taskID, reason); err != nil {
				return err
			}

			worker.EmitEvent(cfg, taskID, "phase_failed_by_worker", map[string]interface{}{
				"reason": reason,
			})
			fmt.Printf("Phase failed: %s\n", reason)
			return nil
		},
	}
}

func newWorkerObserveCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "observe <note>",
		Short:         "Record a meta-cognitive reflection",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			note := joinArgs(args)
			if err := worker.Observe(cfg, taskID, note); err != nil {
				return err
			}

			fmt.Printf("Observation recorded.\n")
			return nil
		},
	}
}

func newWorkerContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "context",
		Short:         "Print current task context",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if len(args) > 0 {
				taskID = args[0]
			}
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			ctx, err := worker.Context(cfg, taskID)
			if err != nil {
				return err
			}
			fmt.Print(ctx)
			return nil
		},
	}
}

func newWorkerWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "watch <task-id>",
		Short:         "Live-watch task state changes (for orchestrator)",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			w, err := worker.NewWatcher(cfg.TaskDir)
			if err != nil {
				return err
			}
			defer w.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle interrupt
			go func() {
				c := make(chan os.Signal, 1)
				//nolint:govet
				_ = c
				cancel()
			}()

			ch := w.Watch(ctx, args[0])
			fmt.Printf("Watching task %s...\n", args[0])

			for event := range ch {
				fmt.Printf("[%s] %s: %v\n", time.Now().Format("15:04:05"), event.Type, event.Payload)
			}

			return nil
		},
	}
}

// resolveTaskID finds the active task ID from the task directory.
func resolveTaskID(cfg *config.Config) string {
	entries, err := os.ReadDir(cfg.TaskDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		statePath := cfg.TaskDir + "/" + e.Name() + "/worker-state.yaml"
		if _, err := os.Stat(statePath); err == nil {
			return e.Name()
		}
	}
	return ""
}

func newWorkerRetryCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "retry <task-id>",
		Short:         "Retry current phase (L1 retry)",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			ts, prompt, err := worker.Retry(cfg, args[0])
			if err != nil {
				return err
			}

			worker.EmitEvent(cfg, ts.TaskID, "phase_retry_by_worker", map[string]interface{}{
				"phase": ts.Phase, "phase_name": ts.PhaseName,
			})

			fmt.Print(prompt)
			return nil
		},
	}
}

func newWorkerLoopbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "loopback <task-id> <phase-id>",
		Short:         "Loop back to implementation phase (L2 cycle)",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			targetPhase, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid phase ID: %v", err)
			}

			ts, prompt, err := worker.Loopback(cfg, args[0], targetPhase)
			if err != nil {
				return err
			}

			worker.EmitEvent(cfg, ts.TaskID, "l2_loopback_by_worker", map[string]interface{}{
				"phase": ts.Phase, "phase_name": ts.PhaseName, "target": targetPhase,
			})

			fmt.Print(prompt)
			return nil
		},
	}
}

func newWorkerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status [task-id]",
		Short:         "Show task state and budget summary",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %v", err)
			}

			taskID := resolveTaskID(cfg)
			if len(args) > 0 {
				taskID = args[0]
			}
			if taskID == "" {
				return fmt.Errorf("no active task found")
			}

			out, err := worker.Status(cfg, taskID)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
