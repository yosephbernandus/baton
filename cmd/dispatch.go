package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/spec"
)

func NewDispatchCmd() *cobra.Command {
	var (
		complexityFlag string
		modeFlag       string
		dryRun         bool
	)

	cmd := &cobra.Command{
		Use:           "dispatch <spec.yaml>",
		Short:         "Auto-select and execute the best mode for a task spec",
		Long:          "Dispatches a task using the optimal mode (run, pipeline, coordinator) based on complexity and config.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath := args[0]

			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			s, err := spec.Load(specPath)
			if err != nil {
				return exitError(3, "loading spec: %v", err)
			}

			if errs := spec.Validate(s); len(errs) > 0 {
				var msgs []string
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				return exitError(3, "spec validation failed:\n  %s", strings.Join(msgs, "\n  "))
			}

			complexity := resolveComplexity(complexityFlag, s.EstimatedComplexity, cfg.PhaseMachine.ComplexityDefault)
			if complexity == "" {
				complexity = phase.ComplexityMedium
			}
			if !phase.ValidComplexity(complexity) {
				return exitError(3, "invalid complexity %q", complexity)
			}

			mode := resolveDispatchMode(modeFlag, cfg.Dispatch.DefaultMode, complexity, cfg.Dispatch.PipelineThreshold)

			if dryRun {
				fmt.Printf("spec:       %s\n", specPath)
				fmt.Printf("complexity: %s\n", complexity)
				fmt.Printf("mode:       %s\n", mode)
				return nil
			}

			batonBin, err := os.Executable()
			if err != nil {
				batonBin = "baton"
			}

			var execArgs []string
			switch mode {
			case "run":
				execArgs = []string{batonBin, "run", "--spec", specPath}
			case "pipeline":
				execArgs = []string{batonBin, "pipeline", "run", specPath, "--complexity", complexity}
			case "coordinator":
				output := cfg.Dispatch.CoordinatorOutput
				execArgs = []string{batonBin, "init", specPath, "--complexity", complexity, "--mode", "coordinator"}
				if output != "" {
					execArgs = append(execArgs, "--output", output)
				}
			default:
				return exitError(2, "unknown mode: %s", mode)
			}

			fmt.Fprintf(os.Stderr, "dispatch: %s (complexity=%s)\n", mode, complexity)

			child := exec.Command(execArgs[0], execArgs[1:]...)
			child.Stdin = os.Stdin
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			child.Dir, _ = os.Getwd()
			if err := child.Run(); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
				return exitError(1, "dispatch failed: %v", err)
			}

			if mode == "coordinator" {
				specBase := filepath.Base(specPath)
				ext := filepath.Ext(specBase)
				taskID := specBase[:len(specBase)-len(ext)]
				instrFile := cfg.InstructionsFilename()
				if cfg.Dispatch.CoordinatorOutput != "" {
					instrFile = cfg.Dispatch.CoordinatorOutput
				}
				src := filepath.Join(cfg.TaskDir, taskID, instrFile)
				if _, err := os.Stat(src); err == nil {
					fmt.Fprintf(os.Stderr, "\nGenerated: %s\n", src)
					fmt.Fprintf(os.Stderr, "Copy to project root: cp %s %s\n", src, instrFile)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&complexityFlag, "complexity", "", "Override complexity (TRIVIAL|SMALL|MEDIUM|LARGE)")
	cmd.Flags().StringVar(&modeFlag, "mode", "", "Force mode (run|pipeline|coordinator)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be executed without running it")

	return cmd
}

var complexityRank = map[string]int{
	phase.ComplexityTrivial: 0,
	phase.ComplexitySmall:   1,
	phase.ComplexityMedium:  2,
	phase.ComplexityLarge:   3,
}

func resolveDispatchMode(modeFlag, configDefault, complexity, threshold string) string {
	if modeFlag != "" {
		return modeFlag
	}

	if configDefault != "" && configDefault != "auto" {
		return configDefault
	}

	if threshold == "" {
		threshold = phase.ComplexityMedium
	}

	taskRank := complexityRank[complexity]
	thresholdRank := complexityRank[threshold]

	if taskRank >= thresholdRank {
		return "pipeline"
	}
	return "run"
}
