package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/knowledge"
)

func NewGrepCmd() *cobra.Command {
	var (
		jsonOutput  bool
		filePattern string
		maxResults  int
	)

	cmd := &cobra.Command{
		Use:           "grep <pattern>",
		Short:         "Search project content (ripgrep/grep fallback)",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return exitError(1, "getting working directory: %v", err)
			}

			pattern := args[0]
			results, err := knowledge.Grep(pattern, knowledge.GrepOpts{
				Dir:         cwd,
				MaxResults:  maxResults,
				FilePattern: filePattern,
			})
			if err != nil {
				return exitError(1, "grep failed: %v", err)
			}

			if jsonOutput {
				fmt.Println(knowledge.FormatGrepJSON(results))
			} else {
				fmt.Print(knowledge.FormatGrepText(results))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")
	cmd.Flags().StringVar(&filePattern, "glob", "", "File pattern filter (e.g. *.go)")
	cmd.Flags().IntVar(&maxResults, "max", 50, "Maximum results")

	return cmd
}
