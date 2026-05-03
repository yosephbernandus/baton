package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/ansi"
	"github.com/yosephbernandus/baton/internal/brief"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/decisions"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/routing"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewResultCmd() *cobra.Command {
	var (
		jsonOutput    bool
		clarification bool
		escalation    bool
		filesOnly     bool
		showOutput      bool
		showOutputFull  bool
		clarifyContext  bool
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

			if clarifyContext {
				if t.Status != "needs_clarification" && t.Status != "needs_human" && t.Status != "deferred" {
					return exitError(1, "task %s has status %q, not blocked", taskID, t.Status)
				}

				projectBrief := brief.Load(cfg.ProjectBrief)
				var allDecisions []decisions.Record
				if dstore, err := decisions.NewStore(cfg.ResultDir); err == nil {
					allDecisions, _ = dstore.ReadAll()
				}

				verdict := routing.AnalyzeClarification(routing.ClarifyContext{
					Clarification: t.Escalation.WorkerClarification,
					Spec:          t.Spec,
					ProjectBrief:  projectBrief,
					Decisions:     allDecisions,
				})

				fmt.Printf("CLARIFICATION NEEDED for %s\n", taskID)
				fmt.Printf("Question: %s\n", valueOr(t.Escalation.WorkerClarification, "(not recorded)"))
				fmt.Println()

				if len(t.OutputTail) > 0 {
					fmt.Println("Worker output (last lines):")
					maxLines := 10
					start := 0
					if len(t.OutputTail) > maxLines {
						start = len(t.OutputTail) - maxLines
					}
					for _, line := range t.OutputTail[start:] {
						fmt.Printf("  %s\n", line)
					}
					fmt.Println()
				}

				if len(allDecisions) > 0 {
					matches := searchDecisions(allDecisions, t.Escalation.WorkerClarification)
					if len(matches) > 0 {
						fmt.Println("Related decisions:")
						for _, d := range matches {
							fmt.Printf("  - Q: %s -> A: %s (reason: %s)\n", d.Question, d.Answer, d.Reason)
						}
						fmt.Println()
					}
				}

				if verdict.CanAutoAnswer {
					fmt.Printf("Suggested answer: %s (from %s)\n", verdict.Answer, verdict.Source)
					fmt.Printf("Confidence: %s\n", verdict.Confidence)
				} else {
					fmt.Printf("No auto-answer found (confidence: %s)\n", verdict.Confidence)
					if verdict.Source != "" {
						fmt.Printf("Hint: %s\n", verdict.Source)
					}
				}

				return nil
			}

			if showOutputFull {
				lines, err := events.ReadTaskOutput(cfg.EventLog, taskID)
				if err != nil {
					return exitError(1, "reading output: %v", err)
				}
				for _, line := range ansi.StripLines(lines) {
					fmt.Println(line)
				}
				return nil
			}

			if showOutput {
				if len(t.OutputTail) == 0 {
					fmt.Println("(no output stored)")
				} else {
					for _, line := range t.OutputTail {
						fmt.Println(line)
					}
				}
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
	cmd.Flags().BoolVar(&showOutput, "output", false, "show stored output tail")
	cmd.Flags().BoolVar(&showOutputFull, "output-full", false, "extract full output from event log")
	cmd.Flags().BoolVar(&clarifyContext, "clarify-context", false, "show decision context for blocked task")
	return cmd
}

func searchDecisions(records []decisions.Record, query string) []decisions.Record {
	if query == "" {
		return nil
	}
	queryLower := strings.ToLower(query)
	var matches []decisions.Record
	for _, r := range records {
		if strings.Contains(strings.ToLower(r.Question), queryLower) ||
			strings.Contains(queryLower, strings.ToLower(r.Answer)) {
			matches = append(matches, r)
		}
	}
	return matches
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
