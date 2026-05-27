package phase

import (
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/session"
)

func TestCompactionGateDefaults(t *testing.T) {
	g := NewCompactionGate(0, 0, nil)
	for _, id := range []int{3, 8, 13} {
		if !g.IsGatePhase(id) {
			t.Errorf("phase %d should be a default gate phase", id)
		}
	}
	if g.IsGatePhase(1) {
		t.Error("phase 1 should not be a gate phase")
	}
	if g.IsGatePhase(16) {
		t.Error("phase 16 should not be a gate phase")
	}
}

func TestCompactionGateCustomPhases(t *testing.T) {
	g := NewCompactionGate(0.9, 200000, []int{5, 10})
	if !g.IsGatePhase(5) || !g.IsGatePhase(10) {
		t.Error("custom gate phases not set")
	}
	if g.IsGatePhase(3) {
		t.Error("phase 3 should not be a gate with custom phases")
	}
}

func TestCompactionGateThreshold(t *testing.T) {
	g := NewCompactionGate(0.85, 1000, nil)
	if g.ShouldCompact(800) {
		t.Error("800 < 850 threshold, should not compact")
	}
	if !g.ShouldCompact(900) {
		t.Error("900 > 850 threshold, should compact")
	}
	if g.ShouldCompact(850) {
		t.Error("850 == 850 threshold, should not compact (strict >)")
	}
}

func TestCompactionGateTokenLimit(t *testing.T) {
	g := NewCompactionGate(0.85, 200000, nil)
	if limit := g.TokenLimit(); limit != 170000 {
		t.Errorf("token limit=%d, want 170000", limit)
	}
}

func TestEstimateTokensEmpty(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Error("empty string should be 0 tokens")
	}
}

func TestEstimateTokens(t *testing.T) {
	tokens := EstimateTokens("hello world foo bar")
	if tokens < 4 || tokens > 10 {
		t.Errorf("4 words should estimate ~5 tokens, got %d", tokens)
	}
}

func TestCompactRecordsPreservesEssentials(t *testing.T) {
	records := []session.PhaseRecord{
		{
			ID:           1,
			Name:         "setup",
			Status:       "completed",
			Notes:        []string{"found 5 files", "also checked config"},
			Errors:       []string{"warning: deprecated API"},
			FilesChanged: []string{"main.go"},
			Attempts:     1,
		},
	}

	compacted := CompactRecords(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 record, got %d", len(compacted))
	}
	r := compacted[0]
	if r.ID != 1 || r.Name != "setup" || r.Status != "completed" {
		t.Error("essential fields not preserved")
	}
	if len(r.FilesChanged) != 1 || r.FilesChanged[0] != "main.go" {
		t.Error("files should be preserved")
	}
	if r.Attempts != 1 {
		t.Error("attempts should be preserved")
	}
	if len(r.Notes) != 1 {
		t.Errorf("expected 1 compacted note, got %d", len(r.Notes))
	}
	if r.Notes[0] != "found 5 files" {
		t.Errorf("first note should be kept, got %q", r.Notes[0])
	}
	if len(r.Errors) != 0 {
		t.Error("errors should be dropped in compacted form")
	}
}

func TestCompactRecordsTruncatesLongNotes(t *testing.T) {
	longNote := strings.Repeat("x", 200)
	records := []session.PhaseRecord{
		{ID: 1, Name: "setup", Status: "completed", Notes: []string{longNote}},
	}

	compacted := CompactRecords(records)
	if len(compacted[0].Notes[0]) > 110 {
		t.Errorf("long note should be truncated to ~103, got %d", len(compacted[0].Notes[0]))
	}
	if !strings.HasSuffix(compacted[0].Notes[0], "...") {
		t.Error("truncated note should end with ...")
	}
}

func TestCompactRecordsEmpty(t *testing.T) {
	compacted := CompactRecords(nil)
	if len(compacted) != 0 {
		t.Error("nil input should produce empty output")
	}
}
