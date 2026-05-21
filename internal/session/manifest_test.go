package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManifest(t *testing.T) {
	m := New("test-001", "specs/task.yaml", "MEDIUM")
	if m.Status != "running" {
		t.Errorf("status=%s, want running", m.Status)
	}
	if m.SpecPath != "specs/task.yaml" {
		t.Errorf("spec_path=%s, want specs/task.yaml", m.SpecPath)
	}
	if m.Complexity != "MEDIUM" {
		t.Errorf("complexity=%s, want MEDIUM", m.Complexity)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	m := New("test-001", "specs/task.yaml", "SMALL")
	m.AdvancePhase(1)
	m.AdvancePhase(2)
	m.SetSkipped([]int{5})
	m.RecordL1Retry()
	m.RecordL2Cycle()

	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.SessionID != "test-001" {
		t.Errorf("session_id=%s, want test-001", loaded.SessionID)
	}
	if loaded.Status != "running" {
		t.Errorf("status=%s, want running", loaded.Status)
	}
	if loaded.Pipeline.CurrentPhase != 2 {
		t.Errorf("current_phase=%d, want 2", loaded.Pipeline.CurrentPhase)
	}
	if len(loaded.Pipeline.PhasesCompleted) != 2 {
		t.Errorf("phases_completed=%v, want [1 2]", loaded.Pipeline.PhasesCompleted)
	}
	if len(loaded.Pipeline.PhasesSkipped) != 1 || loaded.Pipeline.PhasesSkipped[0] != 5 {
		t.Errorf("phases_skipped=%v, want [5]", loaded.Pipeline.PhasesSkipped)
	}
	if loaded.Budget.L1RetriesTotal != 1 {
		t.Errorf("l1_retries=%d, want 1", loaded.Budget.L1RetriesTotal)
	}
	if loaded.Budget.L2CyclesTotal != 1 {
		t.Errorf("l2_cycles=%d, want 1", loaded.Budget.L2CyclesTotal)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	m := New("test-001", "spec.yaml", "TRIVIAL")
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	// Tmp file should not exist after save
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after atomic save")
	}

	// Main file should exist
	if _, err := os.Stat(path); err != nil {
		t.Errorf("manifest file should exist: %v", err)
	}
}

func TestAdvancePhaseDedup(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.AdvancePhase(1)
	m.AdvancePhase(1)
	m.AdvancePhase(1)
	if len(m.Pipeline.PhasesCompleted) != 1 {
		t.Errorf("should dedup, got %v", m.Pipeline.PhasesCompleted)
	}
}

func TestMarkCompleted(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.MarkCompleted()
	if m.Status != "completed" {
		t.Errorf("status=%s, want completed", m.Status)
	}
}

func TestMarkFailed(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.MarkFailed("phase 8 failed")
	if m.Status != "failed" {
		t.Errorf("status=%s, want failed", m.Status)
	}
}

func TestMarkCrashed(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.MarkCrashed()
	if m.Status != "crashed" {
		t.Errorf("status=%s, want crashed", m.Status)
	}
}

func TestIsResumable(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	if !m.IsResumable() {
		t.Error("running should be resumable")
	}
	m.MarkCrashed()
	if !m.IsResumable() {
		t.Error("crashed should be resumable")
	}
	m.MarkRateLimited("429")
	if !m.IsResumable() {
		t.Error("rate_limited should be resumable")
	}
	m.MarkCompleted()
	if m.IsResumable() {
		t.Error("completed should not be resumable")
	}
}

func TestLastCompletedPhase(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	if m.LastCompletedPhase() != 0 {
		t.Errorf("empty should be 0, got %d", m.LastCompletedPhase())
	}
	m.AdvancePhase(1)
	m.AdvancePhase(8)
	m.AdvancePhase(3)
	if m.LastCompletedPhase() != 8 {
		t.Errorf("got %d, want 8", m.LastCompletedPhase())
	}
}

func TestLoopBackTo(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.AdvancePhase(8)
	m.AdvancePhase(10)
	m.LoopBackTo(8)
	if m.Pipeline.CurrentPhase != 8 {
		t.Errorf("current_phase=%d, want 8", m.Pipeline.CurrentPhase)
	}
}

func TestLoadNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/session.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "session.yaml")

	m := New("test", "spec.yaml", "MEDIUM")
	if err := m.Save(path); err != nil {
		t.Fatalf("Save should create dirs: %v", err)
	}
}

func TestSessionPath(t *testing.T) {
	got := SessionPath("auth-api")
	want := filepath.Join(".baton", "sessions", "auth-api.yaml")
	if got != want {
		t.Errorf("SessionPath=%q, want %q", got, want)
	}
}

func TestAddPhaseRecord(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.AddPhaseRecord(PhaseRecord{ID: 1, Name: "setup", Status: "completed", Attempts: 1})
	m.AddPhaseRecord(PhaseRecord{ID: 8, Name: "implementation", Status: "completed", Attempts: 2})

	if len(m.PhaseRecords) != 2 {
		t.Fatalf("expected 2 records, got %d", len(m.PhaseRecords))
	}

	m.AddPhaseRecord(PhaseRecord{ID: 1, Name: "setup", Status: "completed", Attempts: 3})
	if len(m.PhaseRecords) != 2 {
		t.Fatalf("should replace existing, got %d", len(m.PhaseRecords))
	}
	if m.PhaseRecords[0].Attempts != 3 {
		t.Errorf("should have updated attempts to 3, got %d", m.PhaseRecords[0].Attempts)
	}
}

func TestAddPipelineFiles(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.AddPipelineFiles([]string{"a.go", "b.go"})
	m.AddPipelineFiles([]string{"b.go", "c.go"})

	if len(m.PipelineFiles) != 3 {
		t.Errorf("expected 3 unique files, got %v", m.PipelineFiles)
	}
}

func TestMarkRateLimited(t *testing.T) {
	m := New("test", "spec.yaml", "MEDIUM")
	m.MarkRateLimited("429")
	if m.Status != "rate_limited" {
		t.Errorf("status=%s, want rate_limited", m.Status)
	}
}

func TestPhaseRecordSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")

	m := New("test-pr", "spec.yaml", "LARGE")
	m.GitHead = "abc1234"
	m.SpecCoreHash = "d4e5f6a7"
	m.ResumeCount = 2
	m.AddPhaseRecord(PhaseRecord{
		ID:       1,
		Name:     "setup",
		Status:   "completed",
		Notes:    []string{"Go project", "45 files"},
		Attempts: 1,
		Duration: "12s",
	})
	m.AddPipelineFiles([]string{"main.go"})

	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.GitHead != "abc1234" {
		t.Errorf("git_head=%s, want abc1234", loaded.GitHead)
	}
	if loaded.SpecCoreHash != "d4e5f6a7" {
		t.Errorf("spec_core_hash=%s", loaded.SpecCoreHash)
	}
	if loaded.ResumeCount != 2 {
		t.Errorf("resume_count=%d, want 2", loaded.ResumeCount)
	}
	if len(loaded.PhaseRecords) != 1 {
		t.Fatalf("phase_records=%d, want 1", len(loaded.PhaseRecords))
	}
	if loaded.PhaseRecords[0].Name != "setup" {
		t.Errorf("phase_records[0].name=%s", loaded.PhaseRecords[0].Name)
	}
	if len(loaded.PipelineFiles) != 1 || loaded.PipelineFiles[0] != "main.go" {
		t.Errorf("pipeline_files=%v", loaded.PipelineFiles)
	}
}
