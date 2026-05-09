package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/session"
)

var defaultManifestPath = filepath.Join(".baton", "session.yaml")

func NewSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage pipeline session state",
	}

	cmd.AddCommand(newSessionStatusCmd())
	cmd.AddCommand(newSessionResetCmd())
	return cmd
}

func newSessionStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show current pipeline session state",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := session.Load(defaultManifestPath)
			if os.IsNotExist(err) {
				fmt.Println("No active session.")
				return nil
			}
			if err != nil {
				return exitError(1, "loading session: %v", err)
			}

			fmt.Fprintf(os.Stdout, "Session:    %s\n", m.SessionID)
			fmt.Fprintf(os.Stdout, "Status:     %s\n", m.Status)
			fmt.Fprintf(os.Stdout, "Spec:       %s\n", m.SpecPath)
			fmt.Fprintf(os.Stdout, "Complexity: %s\n", m.Complexity)
			fmt.Fprintf(os.Stdout, "Started:    %s\n", m.StartedAt.Format("2006-01-02 15:04:05"))
			fmt.Fprintf(os.Stdout, "Updated:    %s\n", m.UpdatedAt.Format("2006-01-02 15:04:05"))
			fmt.Fprintf(os.Stdout, "Phase:      %d\n", m.Pipeline.CurrentPhase)
			fmt.Fprintf(os.Stdout, "Completed:  %v\n", m.Pipeline.PhasesCompleted)
			fmt.Fprintf(os.Stdout, "Skipped:    %v\n", m.Pipeline.PhasesSkipped)
			fmt.Fprintf(os.Stdout, "L2 Cycles:  %d\n", m.Pipeline.L2Cycles)
			fmt.Fprintf(os.Stdout, "L1 Retries: %d\n", m.Budget.L1RetriesTotal)

			if m.IsResumable() {
				fmt.Fprintf(os.Stdout, "\nSession is resumable. Last completed phase: %d\n", m.LastCompletedPhase())
			}

			return nil
		},
	}
}

func newSessionResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "reset",
		Short:         "Clear session manifest and start fresh",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.Remove(defaultManifestPath); err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No session to reset.")
					return nil
				}
				return exitError(1, "removing session: %v", err)
			}
			fmt.Println("Session reset.")
			return nil
		},
	}
}
