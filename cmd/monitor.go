package cmd

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
	"github.com/yosephbernandus/baton/internal/tui"
)

func NewMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "monitor",
		Short:         "Launch interactive TUI to watch tasks",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}

			model, err := tui.NewModel(cfg.EventLog)
			if err != nil {
				return fmt.Errorf("starting monitor: %w", err)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return fmt.Errorf("creating task store: %w", err)
			}

			emitter, _ := events.NewEmitter(cfg.EventLog)

			model.SetStore(store)

			killCh := make(chan string, 10)
			model.SetKillChannel(killCh)

			reapCh := make(chan string, 10)
			model.SetReapChannel(reapCh)

			go func() {
				for taskID := range killCh {
					if err := store.KillTask(taskID); err != nil {
						fmt.Fprintf(cmd.OutOrStderr(), "kill error: %v\n", err)
					}
					if emitter != nil {
						_ = emitter.TaskEvent(taskID, "", "", "", "task_killed", nil)
					}
				}
			}()

			go func() {
				for taskID := range reapCh {
					t, err := store.Get(taskID)
					if err != nil || t.Status != "running" {
						continue
					}
					t.Status = "failed"
					t.Error = fmt.Sprintf("process exited unexpectedly (PID %d dead)", t.PID)
					now := time.Now().UTC()
					t.CompletedAt = &now
					_ = store.Update(t)
					if emitter != nil {
						_ = emitter.TaskEvent(taskID, "", "", "", "task_failed", map[string]interface{}{
							"reason": "process_dead",
						})
					}
				}
			}()

			p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("monitor error: %w", err)
			}
			return nil
		},
	}
	return cmd
}
