package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"gopkg.in/yaml.v3"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		TaskDir: dir,
		SpecDir: filepath.Join(dir, "specs"),
		PhaseMachine: config.PhaseMachineConfig{
			Enabled:           true,
			ComplexityDefault: "SMALL",
		},
		WorkerProtocol: config.WorkerProtocolConfig{
			Enabled:      true,
			StuckTimeout: "5s",
			BatonBinary:  "baton",
		},
	}
}

func TestHeartbeat(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-heartbeat"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     1,
		PhaseName: "setup",
		Role:      "lead",
		State:     StateStarted,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	if err := Heartbeat(cfg, taskID, "reading files"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "progress.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected progress entry")
	}
}

func TestProgressUpdatesState(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-progress"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     8,
		PhaseName: "implementation",
		Role:      "developer",
		State:     StateStarted,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	if err := Progress(cfg, taskID, 50, "halfway done"); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadTaskState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateWorking {
		t.Errorf("expected state=working, got %s", loaded.State)
	}
}

func TestCompleteAndNext(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-complete"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:     taskID,
		Phase:      1,
		PhaseName:  "setup",
		Role:       "lead",
		State:      StateStarted,
		Complexity: "TRIVIAL",
		StartedAt:  time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	// Create manifest
	manifestData := []byte(`session_id: test-complete
started_at: 2024-01-01T00:00:00Z
updated_at: 2024-01-01T00:00:00Z
status: running
spec_path: test.yaml
complexity: TRIVIAL
pipeline:
  current_phase: 1
  phases_completed: []
  phases_skipped: [2,3,4,5,6,7,9,10,11,12,13,14,15]
  l2_cycles: 0
budget:
  l1_retries_total: 0
  l2_cycles_total: 0
`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Complete
	if err := Complete(cfg, taskID); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadTaskState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateCompleted {
		t.Errorf("expected state=completed, got %s", loaded.State)
	}

	// Next — should advance to phase 8 (implementation) for TRIVIAL
	nextState, prompt, err := Next(cfg, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if nextState.Phase != 8 {
		t.Errorf("expected next phase=8, got %d", nextState.Phase)
	}
	if prompt == "" {
		t.Error("expected non-empty prompt for next phase")
	}
}

func TestFail(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-fail"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     8,
		PhaseName: "implementation",
		Role:      "developer",
		State:     StateWorking,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	if err := Fail(cfg, taskID, "compilation error"); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadTaskState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateFailed {
		t.Errorf("expected state=failed, got %s", loaded.State)
	}

	// Check result.yaml
	var result ResultSignal
	data, err := os.ReadFile(filepath.Join(dir, "result.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("expected result status=failed, got %s", result.Status)
	}
	if result.Reason != "compilation error" {
		t.Errorf("expected reason 'compilation error', got %q", result.Reason)
	}
}

func TestObserve(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-observe"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     8,
		PhaseName: "implementation",
		Role:      "developer",
		State:     StateWorking,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	if err := Observe(cfg, taskID, "project uses hexagonal architecture"); err != nil {
		t.Fatal(err)
	}
	if err := Observe(cfg, taskID, "tests rely on golden files"); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadTaskState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Reflections) != 2 {
		t.Fatalf("expected 2 reflections, got %d", len(loaded.Reflections))
	}
	if loaded.Reflections[0] != "project uses hexagonal architecture" {
		t.Errorf("unexpected reflection: %q", loaded.Reflections[0])
	}
}

func TestStuckTimeout(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-stuck"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     8,
		PhaseName: "implementation",
		Role:      "developer",
		State:     StateWorking,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	// Stuck with very short timeout — no guidance will arrive
	_, err := Stuck(cfg, taskID, "which schema?", 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// State should revert since no guidance received — stuck.yaml should be cleaned up
	if _, err := os.Stat(filepath.Join(dir, "stuck.yaml")); !os.IsNotExist(err) {
		t.Error("expected stuck.yaml to be cleaned up after timeout")
	}
}

func TestStuckWithGuidance(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-stuck-guidance"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     8,
		PhaseName: "implementation",
		Role:      "developer",
		State:     StateWorking,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	// Pre-write guidance before calling Stuck
	guidance := GuidanceResponse{
		Answer: "use v2 schema",
		From:   "orchestrator",
		SentAt: time.Now(),
	}
	data, _ := yaml.Marshal(&guidance)
	if err := os.WriteFile(filepath.Join(dir, "guidance.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	answer, err := Stuck(cfg, taskID, "which schema?", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "use v2 schema" {
		t.Errorf("expected 'use v2 schema', got %q", answer)
	}

	// State should be back to working
	loaded, err := loadTaskState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateWorking {
		t.Errorf("expected state=working after guidance, got %s", loaded.State)
	}
}

func TestContext(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-context"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:      taskID,
		Phase:       8,
		PhaseName:   "implementation",
		Role:        "developer",
		State:       StateWorking,
		Complexity:  "MEDIUM",
		StartedAt:   time.Now(),
		Reflections: []string{"found existing helper in utils.go"},
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	ctx, err := Context(cfg, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}
}
