package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/socket"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewGuideCmd() *cobra.Command {
	var (
		msg string
		by  string
	)

	cmd := &cobra.Command{
		Use:           "guide <task-id>",
		Short:         "Send guidance to a running worker",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			if msg == "" {
				return exitError(1, "--msg is required")
			}

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

			if t.Status != "running" {
				return exitError(1, "task %s has status %q, can only guide running tasks", taskID, t.Status)
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

			if err := client.Send(proto.Message{M: "guide", ID: 1, Msg: msg, From: by}); err != nil {
				return exitError(1, "sending guidance: %v", err)
			}

			reply, err := client.Receive()
			if err != nil {
				return exitError(1, "waiting for acknowledgment: %v", err)
			}

			if emitter, err := events.NewEmitter(cfg.EventLog); err == nil {
				_ = emitter.TaskEvent(taskID, t.Runtime, t.Model, "", "guidance_sent", map[string]interface{}{
					"from": by,
					"msg":  msg,
				})
			}

			if reply.M == "ok" {
				fmt.Printf("guidance sent to %s (acknowledged)\n", taskID)
			} else {
				fmt.Printf("guidance sent to %s\n", taskID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&msg, "msg", "", "guidance message to send (required)")
	cmd.Flags().StringVar(&by, "by", "human", "who is sending: human or orchestrator")
	return cmd
}
