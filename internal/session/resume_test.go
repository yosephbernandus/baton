package session

import (
	"path/filepath"
	"testing"

	"github.com/yosephbernandus/baton/internal/spec"
)

func TestSpecCoreHash(t *testing.T) {
	s1 := &spec.Spec{What: "add auth", Why: "security"}
	s2 := &spec.Spec{What: "add auth", Why: "security"}
	s3 := &spec.Spec{What: "add auth", Why: "compliance"}

	h1 := SpecCoreHash(s1)
	h2 := SpecCoreHash(s2)
	h3 := SpecCoreHash(s3)

	if h1 != h2 {
		t.Errorf("same spec should produce same hash: %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Error("different why should produce different hash")
	}
	if len(h1) != 16 {
		t.Errorf("hash length=%d, want 16", len(h1))
	}
}

func TestCheckResumableNoSession(t *testing.T) {
	s := &spec.Spec{What: "test", Why: "testing"}
	decision, m := CheckResumable("/nonexistent/session.yaml", s)

	if decision.Action != "fresh" {
		t.Errorf("action=%s, want fresh", decision.Action)
	}
	if m != nil {
		t.Error("manifest should be nil for missing session")
	}
}

func TestCheckResumableCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	m := New("test", "spec.yaml", "MEDIUM")
	m.MarkCompleted()
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	s := &spec.Spec{What: "test", Why: "testing"}
	decision, _ := CheckResumable(path, s)
	if decision.Action != "fresh" {
		t.Errorf("action=%s, want fresh (completed session)", decision.Action)
	}
}

func TestCheckResumableRateLimited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	m := New("test", "spec.yaml", "MEDIUM")
	m.SpecCoreHash = SpecCoreHash(&spec.Spec{What: "test", Why: "testing"})
	m.AdvancePhase(1)
	m.AdvancePhase(3)
	m.MarkRateLimited("429")
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	s := &spec.Spec{What: "test", Why: "testing"}
	decision, loaded := CheckResumable(path, s)

	if decision.Action != "resume" {
		t.Errorf("action=%s, want resume", decision.Action)
	}
	if decision.StartPhase != 3 {
		t.Errorf("start_phase=%d, want 3", decision.StartPhase)
	}
	if loaded == nil {
		t.Fatal("loaded manifest should not be nil")
	}
}

func TestCheckResumableSpecChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	originalSpec := &spec.Spec{What: "add auth", Why: "security"}
	m := New("test", "spec.yaml", "MEDIUM")
	m.SpecCoreHash = SpecCoreHash(originalSpec)
	m.MarkRateLimited("429")
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	changedSpec := &spec.Spec{What: "add auth", Why: "compliance"}
	decision, _ := CheckResumable(path, changedSpec)
	if decision.Action != "fresh" {
		t.Errorf("action=%s, want fresh (spec changed)", decision.Action)
	}
}

func TestCheckResumableLoopProtection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	s := &spec.Spec{What: "test", Why: "testing"}
	m := New("test", "spec.yaml", "MEDIUM")
	m.SpecCoreHash = SpecCoreHash(s)
	m.AdvancePhase(1)
	m.Pipeline.CurrentPhase = 3
	m.MarkRateLimited("429")
	m.PhaseResumeAttempts = map[int]int{3: 3}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	decision, _ := CheckResumable(path, s)
	if decision.Action != "fresh" {
		t.Errorf("action=%s, want fresh (resume loop)", decision.Action)
	}
}
