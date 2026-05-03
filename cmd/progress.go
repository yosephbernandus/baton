package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/socket"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewProgressCmd() *cobra.Command {
	var watch bool

	cmd := &cobra.Command{
		Use:           "progress <task-id>",
		Short:         "Show worker progress messages",
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

			if !watch {
				return showHistoricalProgress(cfg, taskID)
			}

			sockPath := t.SocketPath
			if sockPath == "" {
				sockPath = fmt.Sprintf(".baton/tasks/%s/baton.sock", taskID)
			}

			client, err := socket.Dial(sockPath)
			if err != nil {
				return exitError(1, "connecting to task socket: %v (is the task still running?)", err)
			}
			defer client.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fmt.Printf("watching progress for %s (ctrl+c to stop)\n", taskID)
			ch := client.Stream(ctx)
			for msg := range ch {
				printMessage(msg)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&watch, "watch", false, "live-stream progress via socket")
	return cmd
}

func showHistoricalProgress(cfg *config.Config, taskID string) error {
	tailer := events.NewTailer(cfg.EventLog)
	allEvents, err := tailer.ReadAll()
	if err != nil {
		return exitError(1, "reading events: %v", err)
	}

	found := false
	for _, ev := range allEvents {
		if ev.TaskID != taskID {
			continue
		}
		switch ev.EventType {
		case "worker_heartbeat", "worker_progress", "worker_stuck", "worker_error", "worker_milestone":
			msg := ev.Data["msg"]
			pct, _ := ev.Data["pct"].(float64)
			ts := ev.Timestamp.Format("15:04:05")
			typeName := ev.EventType[len("worker_"):]
			extra := ""
			if pct > 0 {
				extra = fmt.Sprintf(" (%d%%)", int(pct))
			}
			fmt.Printf("%s [%-9s] %v%s\n", ts, typeName, msg, extra)
			found = true
		}
	}

	if !found {
		fmt.Println("no progress messages yet")
	}
	return nil
}

func printMessage(msg proto.Message) {
	extra := ""
	if msg.P > 0 {
		extra = fmt.Sprintf(" (%d%%)", msg.P)
	}
	if msg.Detail != "" {
		extra = fmt.Sprintf("\n         Detail: %s", msg.Detail)
	}
	fmt.Printf("[%-9s] %s%s\n", msg.M, msg.Msg, extra)
}
