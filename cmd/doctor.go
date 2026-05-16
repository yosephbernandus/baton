package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
)

func NewDoctorCmd() *cobra.Command {
	var probe bool

	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Verify runtime configurations are correct and working",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(probe)
		},
	}

	cmd.Flags().BoolVar(&probe, "probe", false, "Actually spawn each runtime with a test prompt")
	return cmd
}

func runDoctor(probe bool) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return exitError(2, "config error: %v", err)
	}

	if len(cfg.Runtimes) == 0 {
		fmt.Println("No runtimes configured.")
		fmt.Println("Run: baton setup")
		return exitError(2, "no runtimes configured")
	}

	anyFailed := false

	for name := range cfg.Runtimes {
		diag := cfg.DiagnoseRuntime(name)

		if !diag.Exists {
			fmt.Printf("  ✗ %-15s command not found: %s\n", name, diag.Command)
			anyFailed = true
			continue
		}

		if !diag.ArgsValid {
			fmt.Printf("  ✗ %-15s %s\n", name, diag.ArgsError)
			anyFailed = true
			continue
		}

		modelCount := len(diag.Models)
		fmt.Printf("  ✓ %-15s %-35s %d model(s)\n", name, diag.CommandPath, modelCount)

		if modelCount == 0 {
			fmt.Printf("    ⚠ no models configured\n")
		}

		if probe {
			fmt.Printf("    probing... ")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			pr := cfg.ProbeRuntime(ctx, name)
			cancel()

			if pr.Error != "" {
				fmt.Printf("✗ %s\n", pr.Error)
				anyFailed = true
			} else {
				fmt.Printf("✓ responded in %s\n", pr.Duration.Round(100*time.Millisecond))
			}
		}
	}

	if anyFailed {
		return exitError(2, "some runtimes failed checks")
	}

	return nil
}
