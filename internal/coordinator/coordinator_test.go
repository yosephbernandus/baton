package coordinator

import (
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/spec"
)

func testConfig() *config.Config {
	return &config.Config{
		Orchestrator: config.OrchestratorConfig{
			Runtime: "claude-code",
			Model:   "opus",
		},
		RoleModels: map[string]config.RoleModelConfig{
			"developer": {Runtime: "opencode", Model: "kimi"},
			"tester":    {Runtime: "opencode", Model: "kimi"},
		},
		PhaseMachine: config.PhaseMachineConfig{
			MaxL1Retries: 2,
			MaxL2Cycles:  3,
		},
	}
}

func TestBuildDispatchMapLeadIsSelf(t *testing.T) {
	cfg := testConfig()
	phases := phase.DefaultPhases()
	m := BuildDispatchMap(cfg, phases)

	// Phase 1 (setup, lead) should be Self
	if !m[1].Self {
		t.Error("expected phase 1 (lead) to be Self")
	}
	// Phase 16 (completion, lead) should be Self
	if !m[16].Self {
		t.Error("expected phase 16 (lead) to be Self")
	}
}

func TestBuildDispatchMapDeveloperDispatches(t *testing.T) {
	cfg := testConfig()
	phases := phase.DefaultPhases()
	m := BuildDispatchMap(cfg, phases)

	// Phase 8 (implementation, developer) should dispatch to opencode
	dt := m[8]
	if dt.Self {
		t.Error("expected phase 8 (developer) to be dispatched")
	}
	if dt.Runtime != "opencode" {
		t.Errorf("expected runtime=opencode, got %s", dt.Runtime)
	}
	if dt.Model != "kimi" {
		t.Errorf("expected model=kimi, got %s", dt.Model)
	}
}

func TestBuildDispatchMapReviewerIsSelf(t *testing.T) {
	cfg := testConfig()
	phases := phase.DefaultPhases()
	m := BuildDispatchMap(cfg, phases)

	// Phase 9 (design_verification, reviewer) should be Self
	if !m[9].Self {
		t.Error("expected phase 9 (reviewer) to be Self")
	}
}

func TestBuildDispatchMapTesterDispatches(t *testing.T) {
	cfg := testConfig()
	phases := phase.DefaultPhases()
	m := BuildDispatchMap(cfg, phases)

	// Phase 13 (testing, tester) should dispatch
	dt := m[13]
	if dt.Self {
		t.Error("expected phase 13 (tester) to be dispatched")
	}
	if dt.Runtime != "opencode" {
		t.Errorf("expected runtime=opencode, got %s", dt.Runtime)
	}
}

func TestBuildDispatchMapFallbackSelf(t *testing.T) {
	cfg := &config.Config{
		Orchestrator: config.OrchestratorConfig{Runtime: "claude-code"},
	}
	phases := phase.DefaultPhases()
	m := BuildDispatchMap(cfg, phases)

	// With no role_models, everything should be Self
	for _, ph := range phases {
		if !m[ph.ID].Self {
			t.Errorf("expected phase %d to be Self when no role_models configured", ph.ID)
		}
	}
}

func TestBuildDispatchMapSameRuntimeIsSelf(t *testing.T) {
	cfg := &config.Config{
		Orchestrator: config.OrchestratorConfig{Runtime: "claude-code"},
		RoleModels: map[string]config.RoleModelConfig{
			"developer": {Runtime: "claude-code", Model: "sonnet"},
		},
	}
	phases := phase.DefaultPhases()
	m := BuildDispatchMap(cfg, phases)

	// Developer mapped to same runtime as orchestrator = Self
	if !m[8].Self {
		t.Error("expected phase 8 to be Self when developer runtime matches orchestrator")
	}
}

func TestGenerateContainsAllSections(t *testing.T) {
	cfg := CoordinatorConfig{
		TaskID:     "test-task",
		Spec:       &spec.Spec{What: "implement auth", Why: "security"},
		Complexity: "MEDIUM",
		BatonBin:   "baton",
		Config:     testConfig(),
	}

	out := GenerateCoordinatorInstructions(cfg)

	sections := []string{
		"# Baton Coordinator Protocol",
		"## Task",
		"implement auth",
		"## Active Phases",
		"## Self-Phase Protocol",
		"## Dispatch Protocol",
		"## Retry Protocol",
		"2 retries per phase",
		"## L2 Loop Protocol",
		"## Stuck Protocol",
		"## Command Reference",
		"## Phase Guidance",
		"## Mandatory Phase Exit Questions",
	}

	for _, section := range sections {
		if !strings.Contains(out, section) {
			t.Errorf("expected output to contain %q", section)
		}
	}
}

func TestPhaseTableRespectsComplexity(t *testing.T) {
	cfg := CoordinatorConfig{
		TaskID:     "trivial-task",
		Spec:       &spec.Spec{What: "fix typo", Why: "readability"},
		Complexity: "TRIVIAL",
		BatonBin:   "baton",
		Config:     testConfig(),
	}

	out := GenerateCoordinatorInstructions(cfg)

	// TRIVIAL should skip phases 2-7, 9-15 — only 1, 8, 16 active
	if !strings.Contains(out, "| 1 | setup") {
		t.Error("expected phase 1 in TRIVIAL output")
	}
	if !strings.Contains(out, "| 8 | implementation") {
		t.Error("expected phase 8 in TRIVIAL output")
	}
	if !strings.Contains(out, "| 16 | completion") {
		t.Error("expected phase 16 in TRIVIAL output")
	}
	// Phase 2 (triage) should be skipped for TRIVIAL
	if strings.Contains(out, "| 2 | triage") {
		t.Error("phase 2 should be skipped for TRIVIAL")
	}
}

func TestDispatchProtocolIncludesRuntime(t *testing.T) {
	cfg := CoordinatorConfig{
		TaskID:     "dispatch-test",
		Spec:       &spec.Spec{What: "build API", Why: "need it"},
		Complexity: "MEDIUM",
		BatonBin:   "baton",
		Config:     testConfig(),
	}

	out := GenerateCoordinatorInstructions(cfg)

	if !strings.Contains(out, "--runtime opencode --model kimi") {
		t.Error("expected dispatch target info in output")
	}
}
