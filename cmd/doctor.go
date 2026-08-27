package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/acp"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/transport"
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
		detail := fmt.Sprintf("%d model(s)", modelCount)
		if diag.Protocol == config.ProtocolACP {
			// An ACP runtime's models come from the agent, not from config, so
			// counting the configured ones would report zero for a working one.
			detail = "acp"
		}
		fmt.Printf("  ✓ %-15s %-35s %s\n", name, diag.CommandPath, detail)

		if modelCount == 0 && diag.Protocol != config.ProtocolACP {
			fmt.Printf("    ⚠ no models configured\n")
		}

		if probe {
			fmt.Printf("    probing... ")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			if diag.Protocol == config.ProtocolACP {
				if err := probeACP(ctx, cfg, name); err != nil {
					fmt.Printf("✗ %v\n", err)
					anyFailed = true
				}
			} else {
				pr := cfg.ProbeRuntime(ctx, name)
				if pr.Error != "" {
					fmt.Printf("✗ %s\n", pr.Error)
					anyFailed = true
				} else {
					fmt.Printf("✓ responded in %s\n", pr.Duration.Round(100*time.Millisecond))
				}
			}
			cancel()
		}
	}

	if anyFailed {
		return exitError(2, "some runtimes failed checks")
	}

	return nil
}

// probeACP establishes whether an ACP runtime works by doing what baton does:
// a handshake. That is also the only way to learn what the agent can enforce,
// since capabilities come from the agent rather than from config.
func probeACP(ctx context.Context, cfg *config.Config, name string) error {
	start := time.Now()
	caps, err := acp.New(cfg, func(string, ...any) {}).Probe(ctx, name)
	if err != nil {
		return err
	}
	fmt.Printf("✓ handshake in %s\n", time.Since(start).Round(100*time.Millisecond))
	fmt.Printf("      tool restriction: %s\n", caps.ToolRestriction)
	fmt.Printf("      model select: %v   usage: %v   file locations: %v\n",
		caps.ModelSelect, caps.Usage, caps.FileLocations)
	if caps.ToolRestriction == transport.RestrictNone {
		fmt.Printf("      ⚠ this agent cannot restrict tools, so role boundaries are prompt guidance only\n")
	}
	return nil
}
