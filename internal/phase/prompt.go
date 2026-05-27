package phase

import (
	"fmt"
	"strings"

	"github.com/yosephbernandus/baton/internal/role"
	"github.com/yosephbernandus/baton/internal/session"
)

var phaseDescriptions = map[string]string{
	"setup":                 "Load context, read project state, and identify the current branch and environment.",
	"triage":                "Classify the task type and determine its complexity level.",
	"discovery":             "Explore the codebase to find relevant files, modules, and patterns.",
	"skill_discovery":       "Map technologies and domains to available skills and context.",
	"complexity":            "Estimate scope, identify risks, unknowns, and potential blockers.",
	"brainstorming":         "Generate 2-3 approach options and select the best one with rationale.",
	"architecture":          "Design the module structure, interfaces, and data flow.",
	"implementation":        "Write the production code to implement the task.",
	"design_verification":   "Verify the implementation matches the architecture plan.",
	"domain_compliance":     "Check the code against project-specific rules and patterns.",
	"code_quality":          "Review for maintainability, idiomatic patterns, and no anti-patterns.",
	"test_planning":         "Identify what needs testing and plan the test strategy.",
	"testing":               "Write and run the tests defined in the test plan.",
	"coverage_verification": "Ensure all modified code paths are covered by tests.",
	"test_quality":          "Review test quality — verify assertions are meaningful, not superficial.",
	"completion":            "Final verification, cleanup, and summary of what was accomplished.",
}

var roleDescriptions = map[string]string{
	RoleLead:      "You are the LEAD. You plan, analyze, and design. You do NOT write production code or tests.",
	RoleDeveloper: "You are the DEVELOPER. You write production code to implement the task. Focus on correctness and quality.",
	RoleReviewer:  "You are the REVIEWER. You review code for quality, correctness, and compliance. You MUST NOT modify any files.",
	RoleTestLead:  "You are the TEST LEAD. You plan test strategy and identify what needs testing. You do NOT write tests.",
	RoleTester:    "You are the TESTER. You write and run tests. You MUST NOT modify production code, only test files.",
}

func BuildPhasePrompt(basePrompt string, ph Phase, complexity string, totalPhases int, records []session.PhaseRecord, scratchpadContent string, dirtyFiles map[int][]string, l2Active, l3Active, librarianEnabled bool, configBudgets map[string]int) string {
	var b strings.Builder

	b.WriteString(basePrompt)
	b.WriteString("\n")

	fmt.Fprintf(&b, "=== PHASE: %s (#%d of %d) ===\n", ph.Name, ph.ID, totalPhases)
	fmt.Fprintf(&b, "Complexity: %s\n\n", complexity)

	if desc, ok := roleDescriptions[ph.Role]; ok {
		b.WriteString(desc)
		b.WriteString("\n")
	}
	if boundary := role.BoundaryText(ph.Role); boundary != "" {
		b.WriteString(boundary)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if desc, ok := phaseDescriptions[ph.Name]; ok {
		fmt.Fprintf(&b, "Objective: %s\n\n", desc)
	}

	if len(records) > 0 {
		if librarianEnabled {
			scored := ScoreRecords(records, ph, dirtyFiles, l2Active, l3Active)
			budget := ResolveBudget(ph.ID, configBudgets)
			scored = AssignTiers(scored, budget)
			renderScoredRecords(&b, scored)
		} else {
			b.WriteString("[PRIOR PHASE CONTEXT]\n")
			for _, r := range records {
				detail := buildRecordDetail(r)
				fmt.Fprintf(&b, "- Phase %d (%s): %s\n", r.ID, r.Name, detail)
				if len(r.Errors) > 0 {
					for _, e := range r.Errors {
						fmt.Fprintf(&b, "  error: %s\n", truncate(e, 200))
					}
				}
			}
			b.WriteString("\n")
		}
	}

	if dirtyFiles != nil && IsVerificationPhase(ph.ID) {
		if len(dirtyFiles) > 0 {
			b.WriteString("[MODIFIED FILES — VERIFY THESE]\n")
			for phaseID, files := range dirtyFiles {
				if phaseID < ph.ID {
					for _, f := range files {
						fmt.Fprintf(&b, "- Phase %d: %s\n", phaseID, f)
					}
				}
			}
			b.WriteString("\n")
		}
	}

	if scratchpadContent != "" {
		b.WriteString(scratchpadContent)
		b.WriteString("\n")
	}

	b.WriteString("[COMPLETION]\n")
	fmt.Fprintf(&b, "When done, output exactly one of these markers:\n")
	fmt.Fprintf(&b, "  BATON:C:%s:done            — phase completed successfully\n", ph.CompletionSignal)
	fmt.Fprintf(&b, "  BATON:C:%s:fail:<reason>    — phase failed, explain why\n", ph.CompletionSignal)
	fmt.Fprintf(&b, "  BATON:C:%s:blocked:<reason> — blocked on external dependency\n", ph.CompletionSignal)
	b.WriteString("\nYou MUST output exactly one completion marker before exiting.\n")
	b.WriteString("\nRecord notes about key decisions, findings, or outcomes:\n")
	fmt.Fprintf(&b, "  BATON:N:<what you decided, found, or accomplished>\n")

	return b.String()
}

func BuildResumeBriefing(records []session.PhaseRecord, interruptedPhase int, interruptedName string, interruptReason string, pipelineFiles []string, l1Remaining, l2Remaining, l3Remaining int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[PIPELINE RESUME — Continuing from Phase %d]\n\n", interruptedPhase)
	b.WriteString("You are resuming an interrupted pipeline. Prior phases completed successfully.\n\n")

	if len(records) > 0 {
		b.WriteString("COMPLETED PHASES:\n")
		for _, r := range records {
			if r.Status != "completed" {
				continue
			}
			detail := ""
			if len(r.Notes) > 0 {
				detail = ": " + strings.Join(r.Notes, "; ")
			} else if len(r.FilesChanged) > 0 {
				detail = ": modified " + strings.Join(r.FilesChanged, ", ")
			}
			attemptsNote := ""
			if r.Attempts > 1 {
				attemptsNote = fmt.Sprintf(" (%d attempts)", r.Attempts)
			}
			fmt.Fprintf(&b, "  Phase %d (%s)%s%s\n", r.ID, r.Name, attemptsNote, detail)
		}
		b.WriteString("\n")
	}

	if interruptedName != "" {
		fmt.Fprintf(&b, "INTERRUPTED AT:\n")
		fmt.Fprintf(&b, "  Phase %d (%s): %s\n\n", interruptedPhase, interruptedName, interruptReason)
	}

	if len(pipelineFiles) > 0 {
		b.WriteString("CODE STATE:\n")
		b.WriteString("  Files modified by this pipeline: ")
		b.WriteString(strings.Join(pipelineFiles, ", "))
		b.WriteString("\n\n")
	}

	fmt.Fprintf(&b, "BUDGET:\n")
	fmt.Fprintf(&b, "  L1 retries: %d available | L2 cycles: %d available | L3 cycles: %d available\n\n", l1Remaining, l2Remaining, l3Remaining)
	b.WriteString("Do not re-implement. Code is on disk. Read modified files and continue.\n")

	return b.String()
}

func BuildL3Briefing(failedPhaseID int, failedPhaseName string, l3Cycle, l2CyclesUsed int, failReasons []string, escalatedModel string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[L3 FRESH APPROACH — Cycle %d]\n\n", l3Cycle)
	b.WriteString("ALL previous approaches have failed after exhausting L1 retries and L2 implementation-verification loops.\n\n")

	fmt.Fprintf(&b, "Failed at: Phase %d (%s)\n", failedPhaseID, failedPhaseName)
	fmt.Fprintf(&b, "L2 cycles used: %d\n\n", l2CyclesUsed)

	if len(failReasons) > 0 {
		b.WriteString("PREVIOUS FAILURE REASONS:\n")
		for i, reason := range failReasons {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, truncate(reason, 300))
		}
		b.WriteString("\n")
	}

	if escalatedModel != "" {
		fmt.Fprintf(&b, "NOTE: Model escalated to %s for this attempt.\n\n", escalatedModel)
	}

	b.WriteString("REQUIREMENTS:\n")
	b.WriteString("- You MUST choose a fundamentally different implementation approach\n")
	b.WriteString("- Do NOT repeat any strategy that was tried before\n")
	b.WriteString("- Consider alternative algorithms, data structures, or architectural patterns\n")
	b.WriteString("- If previous approaches hit the same blocker, address that blocker explicitly\n")

	return b.String()
}

func PhaseDescription(name string) string { return phaseDescriptions[name] }
func RoleDescription(role string) string  { return roleDescriptions[role] }

func buildRecordDetail(r session.PhaseRecord) string {
	var parts []string
	if len(r.Notes) > 0 {
		parts = append(parts, strings.Join(r.Notes, "; "))
	}
	if len(r.FilesChanged) > 0 {
		parts = append(parts, "modified "+strings.Join(r.FilesChanged, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, r.Status)
	}
	detail := strings.Join(parts, " | ")
	if r.Attempts > 1 {
		detail += fmt.Sprintf(" (%d attempts)", r.Attempts)
	}
	return detail
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func renderScoredRecords(b *strings.Builder, scored []ScoredRecord) {
	b.WriteString("[PRIOR PHASE CONTEXT]\n")
	for _, sr := range scored {
		r := sr.Record
		switch sr.Tier {
		case TierFull:
			detail := buildRecordDetail(r)
			fmt.Fprintf(b, "- Phase %d (%s): %s\n", r.ID, r.Name, detail)
			if len(r.Errors) > 0 {
				for _, e := range r.Errors {
					fmt.Fprintf(b, "  error: %s\n", truncate(e, 300))
				}
			}
			if r.FailReason != "" {
				fmt.Fprintf(b, "  fail_reason: %s\n", truncate(r.FailReason, 200))
			}
		case TierSummary:
			fmt.Fprintf(b, "- Phase %d (%s): %s", r.ID, r.Name, r.Status)
			if len(r.Notes) > 0 {
				fmt.Fprintf(b, " — %s", truncate(r.Notes[0], 200))
			}
			b.WriteString("\n")
			if len(r.Errors) > 0 {
				fmt.Fprintf(b, "  error: %s\n", truncate(r.Errors[0], 150))
			}
			if len(r.FilesChanged) > 0 {
				fmt.Fprintf(b, "  files: %s\n", strings.Join(r.FilesChanged, ", "))
			}
		case TierMinimal:
			fmt.Fprintf(b, "- Phase %d (%s): %s", r.ID, r.Name, r.Status)
			if len(r.FilesChanged) > 0 {
				fmt.Fprintf(b, " [%s]", strings.Join(r.FilesChanged, ", "))
			}
			b.WriteString("\n")
		case TierOmit:
			continue
		}
	}
	b.WriteString("\n")
}
