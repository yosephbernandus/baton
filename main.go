package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/cmd"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "baton",
		Short:   "Runtime-agnostic multi-agent orchestrator",
		Version: version,
	}

	root.AddCommand(
		cmd.NewRunCmd(),
		cmd.NewStatusCmd(),
		cmd.NewListCmd(),
		cmd.NewResultCmd(),
		cmd.NewMonitorCmd(),
		cmd.NewConfigCmd(),
		cmd.NewCostCmd(),
		cmd.NewKillCmd(),
		cmd.NewWaitCmd(),
		cmd.NewRespondCmd(),
		cmd.NewDeferCmd(),
		cmd.NewEscalateCmd(),
	)

	if err := root.Execute(); err != nil {
		if exitErr, ok := err.(*cmd.ExitErr); ok {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, exitErr.Message)
			}
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
