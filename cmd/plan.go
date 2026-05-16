package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/spec"
)

func NewPlanCmd() *cobra.Command {
	var (
		interactive bool
		output      string
	)

	cmd := &cobra.Command{
		Use:           "plan <description>",
		Short:         "Generate a task spec from a description",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(args[0], interactive, output)
		},
	}

	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Prompt for each field interactively")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output path (default: .baton/specs/<slug>.yaml)")
	return cmd
}

func runPlan(description string, interactive bool, output string) error {
	slug := spec.Slugify(description)
	if output == "" {
		output = filepath.Join(".baton", "specs", slug+".yaml")
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return exitError(1, "creating directory: %v", err)
	}

	var content string

	if interactive {
		scanner := bufio.NewScanner(os.Stdin)

		fmt.Printf("Task: %s\n\n", description)

		why := prompt(scanner, "Why does this task exist?")
		constraintStr := prompt(scanner, "Constraints (comma-separated)?")
		filesStr := prompt(scanner, "Context files (comma-separated)?")
		criteriaStr := prompt(scanner, "Acceptance criteria (comma-separated)?")
		complexity := prompt(scanner, "Complexity (TRIVIAL/SMALL/MEDIUM/LARGE, or empty)?")
		criticality := prompt(scanner, "Criticality (low/medium/high, or empty)?")

		constraints := splitAndTrim(constraintStr)
		files := splitAndTrim(filesStr)
		criteria := splitAndTrim(criteriaStr)

		opts := spec.TemplateOpts{
			Complexity:  strings.ToUpper(complexity),
			Criticality: strings.ToLower(criticality),
		}

		content = spec.GenerateFromInputs(description, why, constraints, files, criteria, opts)
	} else {
		content = spec.GenerateTemplate(description)
	}

	if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
		return exitError(1, "writing spec: %v", err)
	}

	fmt.Printf("Spec created: %s\n", output)
	if !interactive {
		fmt.Println("Edit the file to fill in TODO fields, then run:")
	} else {
		fmt.Println("Next:")
	}
	fmt.Printf("  baton init %s\n", output)

	return nil
}

func prompt(scanner *bufio.Scanner, question string) string {
	fmt.Printf("%s\n> ", question)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
