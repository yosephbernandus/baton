package annealing

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/feedback"
)

func TestGeneratePatchesEmpty(t *testing.T) {
	a := New(Config{PatchDir: t.TempDir()})
	pf, err := a.GeneratePatches(&feedback.Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Patches) != 0 {
		t.Errorf("expected 0 patches, got %d", len(pf.Patches))
	}
}

func TestGeneratePatchesNilAnalysis(t *testing.T) {
	a := New(Config{PatchDir: t.TempDir()})
	_, err := a.GeneratePatches(nil)
	if err == nil {
		t.Error("expected error for nil analysis")
	}
}

func TestGeneratePatchFromRuntimeMismatch(t *testing.T) {
	a := New(Config{PatchDir: t.TempDir(), MinConfidence: "low"})
	analysis := &feedback.Analysis{
		Patterns: []feedback.Pattern{
			{
				Type:        "runtime_domain_mismatch",
				Description: "opencode/kimi fails 60% of frontend tasks",
				Confidence:  "high",
				Occurrences: 6,
				Suggestion:  "Route frontend tasks to claude-code/sonnet",
			},
		},
	}

	pf, err := a.GeneratePatches(analysis)
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(pf.Patches))
	}
	p := pf.Patches[0]
	if p.Pattern != "runtime_domain_mismatch" {
		t.Errorf("pattern=%q", p.Pattern)
	}
	if p.Risk != "low" {
		t.Errorf("risk=%q", p.Risk)
	}
	if p.TargetPath != "routing.rules" {
		t.Errorf("target=%q", p.TargetPath)
	}
}

func TestGeneratePatchRetryBudget(t *testing.T) {
	a := New(Config{PatchDir: t.TempDir(), MinConfidence: "low"})
	analysis := &feedback.Analysis{
		Patterns: []feedback.Pattern{
			{
				Type:        "retry_budget_insufficient",
				Description: "LARGE tasks exhaust L1 budget",
				Confidence:  "medium",
				Occurrences: 4,
				Suggestion:  "Increase max_l1_retries",
			},
		},
	}

	pf, err := a.GeneratePatches(analysis)
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(pf.Patches))
	}
	if pf.Patches[0].TargetPath != "phase_machine.max_l1_retries" {
		t.Errorf("target=%q", pf.Patches[0].TargetPath)
	}
	if pf.Patches[0].ProposedValue != 5 {
		t.Errorf("proposed=%v", pf.Patches[0].ProposedValue)
	}
}

func TestConfidenceFiltering(t *testing.T) {
	a := New(Config{PatchDir: t.TempDir(), MinConfidence: "high"})
	analysis := &feedback.Analysis{
		Patterns: []feedback.Pattern{
			{Type: "retry_budget_insufficient", Confidence: "low", Occurrences: 3, Suggestion: "x"},
			{Type: "runtime_domain_mismatch", Confidence: "high", Occurrences: 5, Suggestion: "y"},
		},
	}

	pf, err := a.GeneratePatches(analysis)
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Patches) != 1 {
		t.Fatalf("expected 1 patch (high only), got %d", len(pf.Patches))
	}
	if pf.Patches[0].Confidence != "high" {
		t.Error("should only include high confidence patches")
	}
}

func TestAutoApplyEligible(t *testing.T) {
	a := New(Config{AutoApplyMaxRisk: "low"})
	patches := []Patch{
		{ID: "p1", Risk: "low", TargetPath: "routing.rules"},
		{ID: "p2", Risk: "medium", TargetPath: "role_models"},
		{ID: "p3", Risk: "low", TargetPath: "default_timeout"},
		{ID: "p4", Risk: "low", TargetPath: "phase_machine.enabled"}, // safety: blocked
		{ID: "p5", Risk: "low", TargetPath: "escalation_advisor.enabled"}, // safety: blocked
		{ID: "p6", Risk: "low", TargetPath: "routing.rules", Applied: true}, // already applied
	}

	eligible := a.AutoApplyEligible(patches)
	if len(eligible) != 2 {
		t.Fatalf("expected 2 eligible, got %d", len(eligible))
	}
	ids := map[string]bool{}
	for _, p := range eligible {
		ids[p.ID] = true
	}
	if !ids["p1"] || !ids["p3"] {
		t.Errorf("expected p1 and p3, got %v", ids)
	}
}

func TestAutoApplyNoRiskDefault(t *testing.T) {
	a := New(Config{})
	patches := []Patch{
		{ID: "p1", Risk: "low", TargetPath: "routing.rules"},
		{ID: "p2", Risk: "medium", TargetPath: "role_models"},
	}
	eligible := a.AutoApplyEligible(patches)
	if len(eligible) != 1 {
		t.Fatalf("expected 1 (default max_risk=low), got %d", len(eligible))
	}
}

func TestSavePatchFile(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{PatchDir: dir})
	pf := &PatchFile{
		GeneratedAt: time.Now().UTC(),
		Patches: []Patch{
			{ID: "p1", Pattern: "test", Description: "test patch"},
		},
	}
	err := a.savePatchFile(pf)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "suggested-patches.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("empty patch file")
	}
}

func TestLoadPatches(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{PatchDir: dir})

	pf := &PatchFile{
		GeneratedAt: time.Now().UTC(),
		Patches: []Patch{
			{ID: "p1", Pattern: "test", Description: "loaded"},
		},
	}
	_ = a.savePatchFile(pf)

	loaded, err := a.LoadPatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(loaded.Patches))
	}
	if loaded.Patches[0].Description != "loaded" {
		t.Errorf("desc=%q", loaded.Patches[0].Description)
	}
}

func TestLoadPatchesNotFound(t *testing.T) {
	a := New(Config{PatchDir: t.TempDir()})
	_, err := a.LoadPatches()
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestMeetsConfidence(t *testing.T) {
	tests := []struct {
		actual, min string
		want        bool
	}{
		{"high", "low", true},
		{"high", "medium", true},
		{"high", "high", true},
		{"medium", "high", false},
		{"low", "medium", false},
		{"low", "low", true},
	}
	for _, tt := range tests {
		if got := meetsConfidence(tt.actual, tt.min); got != tt.want {
			t.Errorf("meetsConfidence(%q,%q)=%v want %v", tt.actual, tt.min, got, tt.want)
		}
	}
}

func TestIsSafetyConfig(t *testing.T) {
	if !isSafetyConfig("phase_machine.enabled") {
		t.Error("phase_machine.enabled should be safety config")
	}
	if !isSafetyConfig("escalation_advisor.enabled") {
		t.Error("escalation_advisor should be safety config")
	}
	if isSafetyConfig("routing.rules") {
		t.Error("routing.rules is not safety config")
	}
}
