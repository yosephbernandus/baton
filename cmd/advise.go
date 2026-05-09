package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
)

func NewAdviseCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "advise <task-id>",
		Short:         "Show advisor context for a stuck task",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			contextPath := filepath.Join(cfg.TaskDir, taskID, "advisor-context.yaml")
			data, err := os.ReadFile(contextPath)
			if err != nil {
				if os.IsNotExist(err) {
					return exitError(1, "no advisor context for task %q (task may not have been escalated)", taskID)
				}
				return exitError(1, "reading advisor context: %v", err)
			}

			fmt.Fprintf(os.Stderr, "--- Advisor Context for %s ---\n", taskID)
			fmt.Print(string(data))
			fmt.Fprintf(os.Stderr, "\nTo respond: baton respond %s \"<your guidance>\"\n", taskID)
			return nil
		},
	}
}
