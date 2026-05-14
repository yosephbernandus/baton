package worker

import (
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/spec"
)

func TestGenerateInstructionsGeneric(t *testing.T) {
	phases := phase.DefaultPhases()
	cfg := InstructionConfig{
		Runtime:    RuntimeGeneric,
		TaskID:     "test-task",
		Spec:       &spec.Spec{What: "implement auth", Why: "security requirement"},
		Phase:      phases[0], // setup
		Complexity: "MEDIUM",
		BatonBin:   "baton",
	}

	out := GenerateInstructions(cfg)

	checks := []string{
		"# Baton Worker Protocol",
		"implement auth",
		"security requirement",
		"baton worker heartbeat",
		"baton worker complete",
		"baton worker stuck",
		"Phase Exit Questions",
	}
	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("expected output to contain %q", check)
		}
	}
}

func TestGenerateInstructionsClaudeCode(t *testing.T) {
	phases := phase.DefaultPhases()
	cfg := InstructionConfig{
		Runtime:    RuntimeClaudeCode,
		TaskID:     "auth-task",
		Spec:       &spec.Spec{What: "add OAuth", Why: "user request"},
		Phase:      phases[7], // implementation (phase 8)
		Complexity: "SMALL",
		BatonBin:   "baton",
	}

	out := GenerateInstructions(cfg)

	if !strings.Contains(out, "Claude Code Notes") {
		t.Error("expected Claude Code specific section")
	}
	if !strings.Contains(out, "Bash tool") {
		t.Error("expected Bash tool reference for Claude Code")
	}
	if !strings.Contains(out, "auth-task") {
		t.Error("expected task ID in output")
	}
}

func TestGenerateInstructionsOpenCode(t *testing.T) {
	phases := phase.DefaultPhases()
	cfg := InstructionConfig{
		Runtime:    RuntimeOpenCode,
		TaskID:     "oc-task",
		Spec:       &spec.Spec{What: "refactor", Why: "tech debt"},
		Phase:      phases[0],
		Complexity: "TRIVIAL",
		BatonBin:   "baton",
	}

	out := GenerateInstructions(cfg)

	if !strings.Contains(out, "OpenCode") {
		t.Error("expected OpenCode specific section")
	}
}

func TestGenerateInstructionsIncludesConstraints(t *testing.T) {
	phases := phase.DefaultPhases()
	cfg := InstructionConfig{
		Runtime: RuntimeGeneric,
		TaskID:  "constrained-task",
		Spec: &spec.Spec{
			What:               "build API",
			Why:                "needed by frontend",
			Constraints:        []string{"no external deps", "must be backwards compatible"},
			AcceptanceCriteria: []string{"all tests pass", "no breaking changes"},
		},
		Phase:      phases[0],
		Complexity: "MEDIUM",
		BatonBin:   "baton",
	}

	out := GenerateInstructions(cfg)

	if !strings.Contains(out, "no external deps") {
		t.Error("expected constraints in output")
	}
	if !strings.Contains(out, "all tests pass") {
		t.Error("expected acceptance criteria in output")
	}
}
