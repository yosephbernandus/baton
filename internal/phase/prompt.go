package phase

import (
	"fmt"
	"strings"

	"github.com/yosephbernandus/baton/internal/role"
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

func BuildPhasePrompt(basePrompt string, ph Phase, complexity string, totalPhases int, prevOutputs map[int]string, scratchpadContent string, dirtyFiles ...map[int][]string) string {
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

	if len(prevOutputs) > 0 {
		b.WriteString("[PREVIOUS PHASE OUTPUTS]\n")
		for id, output := range prevOutputs {
			fmt.Fprintf(&b, "- Phase %d: %s\n", id, truncate(output, 500))
		}
		b.WriteString("\n")
	}

	if len(dirtyFiles) > 0 && dirtyFiles[0] != nil && IsVerificationPhase(ph.ID) {
		df := dirtyFiles[0]
		if len(df) > 0 {
			b.WriteString("[MODIFIED FILES — VERIFY THESE]\n")
			for phaseID, files := range df {
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
	b.WriteString("\nTo record notes for future attempts (if this phase is retried):\n")
	fmt.Fprintf(&b, "  BATON:N:<what you tried and what happened>\n")

	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func lastNLines(lines []string, n int) string {
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
