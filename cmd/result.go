package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewResultCmd() *cobra.Command {
	var (
		jsonOutput    bool
		clarification bool
		escalation    bool
		filesOnly     bool
	)

	cmd := &cobra.Command{
		Use:   "result <task-id>",
		Short: "Read output of a completed task",
		Args:  cobra.ExactArgs(1),
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

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(t)
			}

			if clarification {
				if t.Escalation.WorkerClarification == "" {
					fmt.Println("no clarification recorded")
				} else {
					fmt.Println(t.Escalation.WorkerClarification)
				}
				return nil
			}

			if escalation {
				fmt.Printf("Worker clarification: %s\n", valueOr(t.Escalation.WorkerClarification, "(none)"))
				fmt.Printf("Orchestrator analysis: %s\n", valueOr(t.Escalation.OrchestratorAnalysis, "(none)"))
				fmt.Printf("Human decision: %s\n", valueOr(t.Escalation.HumanDecision, "(none)"))
				fmt.Printf("Human reason: %s\n", valueOr(t.Escalation.HumanReason, "(none)"))
				return nil
			}

			if filesOnly {
				for _, f := range t.FilesChanged {
					fmt.Println(f)
				}
				return nil
			}

			fmt.Printf("Task:     %s\n", t.ID)
			fmt.Printf("Runtime:  %s\n", t.Runtime)
			fmt.Printf("Model:    %s\n", t.Model)
			fmt.Printf("Status:   %s\n", t.Status)
			fmt.Printf("Duration: %s\n", valueOr(t.Duration, "-"))
			if t.ExitCode != nil {
				fmt.Printf("Exit:     %d\n", *t.ExitCode)
			}
			if t.Error != "" {
				fmt.Printf("Error:    %s\n", t.Error)
			}
			if len(t.FilesChanged) > 0 {
				fmt.Printf("Files:    %v\n", t.FilesChanged)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&clarification, "clarification", false, "show worker clarification")
	cmd.Flags().BoolVar(&escalation, "escalation", false, "show full escalation chain")
	cmd.Flags().BoolVar(&filesOnly, "files-only", false, "only show changed files")
	return cmd
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
