package coordinator

import (
	"fmt"
	"strings"

	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/role"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/worker"
)

func buildIdentitySection() string {
	return `# Baton Coordinator Protocol

You are the **coordinator** of a baton pipeline. You orchestrate the entire task
through multiple phases — planning, dispatching implementation to workers,
reviewing results, and handling retries.

You are NOT a worker. For phases you own (lead, reviewer, test_lead), you do the
thinking and analysis work directly. For phases that require code writing
(developer, tester), you dispatch to external runtimes via ` + "`baton run`" + `.

Use ` + "`baton worker`" + ` commands to track your progress through phases.

`
}

func buildTaskSection(s *spec.Spec) string {
	if s == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Task\n\n")

	if s.What != "" {
		fmt.Fprintf(&b, "**What:** %s\n\n", s.What)
	}
	if s.Why != "" {
		fmt.Fprintf(&b, "**Why:** %s\n\n", s.Why)
	}
	if len(s.Constraints) > 0 {
		b.WriteString("**Constraints:**\n")
		for _, c := range s.Constraints {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}
	if len(s.AcceptanceCriteria) > 0 {
		b.WriteString("**Acceptance Criteria:**\n")
		for _, ac := range s.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", ac)
		}
		b.WriteString("\n")
	}
	if len(s.ContextFiles) > 0 {
		b.WriteString("**Context Files:**\n")
		for _, f := range s.ContextFiles {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func buildPhaseTableSection(phases []phase.Phase, dispatch map[int]DispatchTarget, complexity string) string {
	var b strings.Builder
	b.WriteString("## Active Phases\n\n")
	fmt.Fprintf(&b, "Complexity: **%s**\n\n", complexity)
	b.WriteString("| # | Phase | Role | Action | Runtime |\n")
	b.WriteString("|---|-------|------|--------|--------|\n")

	for _, ph := range phases {
		dt := dispatch[ph.ID]
		action := "Self"
		runtime := "—"
		if !dt.Self {
			action = "Dispatch"
			runtime = fmt.Sprintf("%s/%s", dt.Runtime, dt.Model)
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n",
			ph.ID, ph.Name, ph.Role, action, runtime)
	}
	b.WriteString("\n")

	return b.String()
}

func buildSelfPhaseProtocol(batonBin, taskID string) string {
	var b strings.Builder
	b.WriteString("## Self-Phase Protocol\n\n")
	b.WriteString("For phases marked **Self** in the table above:\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "# 1. Start the phase\n")
	fmt.Fprintf(&b, "%s worker start %s\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 2. Do the work (read code, analyze, plan, review)\n")
	fmt.Fprintf(&b, "#    Report progress periodically:\n")
	fmt.Fprintf(&b, "%s worker heartbeat \"analyzing codebase\"\n", batonBin)
	fmt.Fprintf(&b, "%s worker progress 50 \"identified key modules\"\n\n", batonBin)
	fmt.Fprintf(&b, "# 3. Answer mandatory exit questions:\n")
	fmt.Fprintf(&b, "%s worker observe \"answer to question 1\"\n", batonBin)
	fmt.Fprintf(&b, "%s worker observe \"answer to question 2\"\n\n", batonBin)
	fmt.Fprintf(&b, "# 4. Complete the phase\n")
	fmt.Fprintf(&b, "%s worker complete %s\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 5. Advance to next phase\n")
	fmt.Fprintf(&b, "%s worker next %s\n", batonBin, taskID)
	b.WriteString("```\n\n")

	return b.String()
}

func buildDispatchProtocol(batonBin, taskID string, phases []phase.Phase, dispatch map[int]DispatchTarget) string {
	var b strings.Builder
	b.WriteString("## Dispatch Protocol\n\n")
	b.WriteString("For phases marked **Dispatch** in the table above:\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "# 1. Start tracking the phase\n")
	fmt.Fprintf(&b, "%s worker start %s\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 2. Dispatch to worker runtime\n")
	fmt.Fprintf(&b, "%s run --runtime <RUNTIME> --model <MODEL> \\\n", batonBin)
	fmt.Fprintf(&b, "  --spec <spec.yaml> --task-id %s-dispatch\n\n", taskID)
	fmt.Fprintf(&b, "# 3. Wait for result\n")
	fmt.Fprintf(&b, "%s wait %s-dispatch\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 4. Read result and evaluate\n")
	fmt.Fprintf(&b, "%s result %s-dispatch\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 5. If success:\n")
	fmt.Fprintf(&b, "%s worker complete %s\n", batonBin, taskID)
	fmt.Fprintf(&b, "%s worker next %s\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 5. If failure: see Retry Protocol below\n")
	b.WriteString("```\n\n")

	// List specific dispatch targets
	hasDispatch := false
	for _, ph := range phases {
		dt := dispatch[ph.ID]
		if !dt.Self {
			if !hasDispatch {
				b.WriteString("**Dispatch targets:**\n")
				hasDispatch = true
			}
			fmt.Fprintf(&b, "- Phase %d (%s): `--runtime %s --model %s`\n",
				ph.ID, ph.Name, dt.Runtime, dt.Model)
		}
	}
	if hasDispatch {
		b.WriteString("\n")
	}

	return b.String()
}

func buildRetryProtocol(maxL1, maxL2 int, batonBin, taskID string) string {
	var b strings.Builder
	b.WriteString("## Retry Protocol (L1)\n\n")
	fmt.Fprintf(&b, "**Budget: %d retries per phase.**\n\n", maxL1)
	b.WriteString("When a phase fails:\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "# 1. Check budget\n")
	fmt.Fprintf(&b, "%s worker status %s\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 2. If retries remaining > 0:\n")
	fmt.Fprintf(&b, "%s worker retry %s\n", batonBin, taskID)
	fmt.Fprintf(&b, "# This resets the phase and shows scratchpad from previous attempt.\n")
	fmt.Fprintf(&b, "# Re-attempt the phase with the scratchpad context.\n\n")
	fmt.Fprintf(&b, "# 3. If retries exhausted and this is a verification phase (9-15):\n")
	fmt.Fprintf(&b, "#    Trigger L2 loop (see below)\n\n")
	fmt.Fprintf(&b, "# 4. If retries exhausted and NOT a verification phase:\n")
	fmt.Fprintf(&b, "%s worker fail %s \"reason: retries exhausted\"\n", batonBin, taskID)
	b.WriteString("```\n\n")

	return b.String()
}

func buildL2LoopProtocol(batonBin, taskID string) string {
	var b strings.Builder
	b.WriteString("## L2 Loop Protocol\n\n")
	b.WriteString("When a **verification phase** (9-15) fails and L1 retries are exhausted,\n")
	b.WriteString("loop back to implementation (phase 8) to fix the issue.\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "# 1. Check L2 budget\n")
	fmt.Fprintf(&b, "%s worker status %s\n\n", batonBin, taskID)
	fmt.Fprintf(&b, "# 2. If L2 cycles remaining > 0:\n")
	fmt.Fprintf(&b, "%s worker loopback %s 8\n", batonBin, taskID)
	fmt.Fprintf(&b, "# This jumps back to phase 8 (implementation).\n")
	fmt.Fprintf(&b, "# Include the verification failure context in the implementation prompt.\n")
	fmt.Fprintf(&b, "# Re-execute from phase 8 forward.\n\n")
	fmt.Fprintf(&b, "# 3. If L2 cycles exhausted:\n")
	fmt.Fprintf(&b, "%s worker fail %s \"L2 cycles exhausted\"\n", batonBin, taskID)
	b.WriteString("```\n\n")

	return b.String()
}

func buildStuckProtocol(batonBin string) string {
	var b strings.Builder
	b.WriteString("## Stuck Protocol\n\n")
	b.WriteString("**If YOU (coordinator) are stuck:**\n")
	b.WriteString("- Ask the human directly — you are in conversation with them.\n\n")
	b.WriteString("**If a DISPATCHED worker gets stuck:**\n")
	b.WriteString("- Read the worker's result/output\n")
	b.WriteString("- If you can answer the question: respond and re-dispatch\n")
	b.WriteString("- If you cannot answer: ask the human\n\n")
	b.WriteString("**If a dispatched worker returns `needs_clarification`:**\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "# Read what the worker asked\n")
	fmt.Fprintf(&b, "%s result <dispatch-task-id>\n\n", batonBin)
	fmt.Fprintf(&b, "# If you can answer, re-dispatch with the answer included in prompt\n")
	fmt.Fprintf(&b, "# If you cannot answer, ask the human\n")
	b.WriteString("```\n\n")

	return b.String()
}

func buildCommandReference(batonBin, taskID string) string {
	var b strings.Builder
	b.WriteString("## Command Reference\n\n")
	b.WriteString("```bash\n")
	b.WriteString("# Phase lifecycle\n")
	fmt.Fprintf(&b, "%s worker start %s          # Begin current phase\n", batonBin, taskID)
	fmt.Fprintf(&b, "%s worker complete %s       # Mark phase done\n", batonBin, taskID)
	fmt.Fprintf(&b, "%s worker next %s           # Advance to next phase\n", batonBin, taskID)
	fmt.Fprintf(&b, "%s worker fail %s \"reason\"  # Mark phase failed\n", batonBin, taskID)
	b.WriteString("\n# Progress reporting\n")
	fmt.Fprintf(&b, "%s worker heartbeat \"msg\"         # Liveness signal\n", batonBin)
	fmt.Fprintf(&b, "%s worker progress 50 \"msg\"       # Progress update\n", batonBin)
	fmt.Fprintf(&b, "%s worker observe \"reflection\"    # Record observation\n", batonBin)
	b.WriteString("\n# Retry and loopback\n")
	fmt.Fprintf(&b, "%s worker retry %s          # L1 retry current phase\n", batonBin, taskID)
	fmt.Fprintf(&b, "%s worker loopback %s 8     # L2 loop to implementation\n", batonBin, taskID)
	fmt.Fprintf(&b, "%s worker status %s         # Check budget remaining\n", batonBin, taskID)
	b.WriteString("\n# Context\n")
	fmt.Fprintf(&b, "%s worker context %s        # Current task context\n", batonBin, taskID)
	b.WriteString("\n# Dispatch to worker\n")
	fmt.Fprintf(&b, "%s run --runtime X --model Y --spec spec.yaml --task-id <id>\n", batonBin)
	fmt.Fprintf(&b, "%s wait <id>                # Wait for worker to finish\n", batonBin)
	fmt.Fprintf(&b, "%s result <id>              # Read worker result\n", batonBin)
	b.WriteString("```\n\n")

	return b.String()
}

func buildPhaseGuidance(phases []phase.Phase) string {
	var b strings.Builder
	b.WriteString("## Phase Guidance\n\n")

	for _, ph := range phases {
		desc := phase.PhaseDescription(ph.Name)
		if desc == "" {
			continue
		}
		rdesc := phase.RoleDescription(ph.Role)
		boundary := role.BoundaryText(ph.Role)

		fmt.Fprintf(&b, "### Phase %d: %s (%s)\n", ph.ID, ph.Name, ph.Role)
		fmt.Fprintf(&b, "**Objective:** %s\n", desc)
		if rdesc != "" {
			fmt.Fprintf(&b, "%s\n", rdesc)
		}
		if boundary != "" {
			fmt.Fprintf(&b, "%s\n", boundary)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func buildReflectionSection(phases []phase.Phase) string {
	var b strings.Builder
	b.WriteString("## Mandatory Phase Exit Questions\n\n")
	b.WriteString("Before completing each phase, record an observation for each question.\n\n")

	for _, ph := range phases {
		qs := worker.PhaseReflections(ph.ID, ph.Name)
		if len(qs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**Phase %d (%s):**\n", ph.ID, ph.Name)
		for i, q := range qs {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
		b.WriteString("\n")
	}

	return b.String()
}
