package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
)

func NewListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show available runtimes and models",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			type runtimeInfo struct {
				Name      string   `json:"name"`
				Models    []string `json:"models"`
				Available bool     `json:"available"`
			}

			var runtimes []runtimeInfo
			for name, rt := range cfg.Runtimes {
				runtimes = append(runtimes, runtimeInfo{
					Name:      name,
					Models:    rt.Models,
					Available: cfg.RuntimeAvailable(name),
				})
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(runtimes)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "RUNTIME\tMODELS\tSTATUS")
			for _, r := range runtimes {
				status := "available"
				if !r.Available {
					status = "not found"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, strings.Join(r.Models, ", "), status)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
