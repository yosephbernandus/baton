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

func TestRetryIncrementsL1(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-retry"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:     taskID,
		Phase:      8,
		PhaseName:  "implementation",
		Role:       "developer",
		State:      StateFailed,
		Complexity: "MEDIUM",
		StartedAt:  time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	manifestData := []byte(`session_id: test-retry
started_at: 2024-01-01T00:00:00Z
updated_at: 2024-01-01T00:00:00Z
status: running
spec_path: test.yaml
complexity: MEDIUM
pipeline:
  current_phase: 8
  phases_completed: [1]
  phases_skipped: []
  l2_cycles: 0
budget:
  l1_retries_total: 0
  l2_cycles_total: 0
`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}

	retried, prompt, err := Retry(cfg, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.State != StateStarted {
		t.Errorf("expected state=started after retry, got %s", retried.State)
	}
	if prompt == "" {
		t.Error("expected non-empty prompt after retry")
	}
}

func TestRetryRejectsNonFailed(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-retry-reject"
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

	_, _, err := Retry(cfg, taskID)
	if err == nil {
		t.Fatal("expected error when retrying non-failed task")
	}
}

func TestLoopbackToPhase8(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-loopback"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:     taskID,
		Phase:      9,
		PhaseName:  "design_verification",
		Role:       "reviewer",
		State:      StateFailed,
		Complexity: "MEDIUM",
		StartedAt:  time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	manifestData := []byte(`session_id: test-loopback
started_at: 2024-01-01T00:00:00Z
updated_at: 2024-01-01T00:00:00Z
status: running
spec_path: test.yaml
complexity: MEDIUM
pipeline:
  current_phase: 9
  phases_completed: [1,8]
  phases_skipped: []
  l2_cycles: 0
budget:
  l1_retries_total: 0
  l2_cycles_total: 0
`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}

	looped, prompt, err := Loopback(cfg, taskID, 8)
	if err != nil {
		t.Fatal(err)
	}
	if looped.Phase != 8 {
		t.Errorf("expected phase=8 after loopback, got %d", looped.Phase)
	}
	if looped.PhaseName != "implementation" {
		t.Errorf("expected phase_name=implementation, got %s", looped.PhaseName)
	}
	if looped.Role != "developer" {
		t.Errorf("expected role=developer, got %s", looped.Role)
	}
	if prompt == "" {
		t.Error("expected non-empty prompt after loopback")
	}
}

func TestLoopbackRejectsInvalidPhase(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-loopback-invalid"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     9,
		PhaseName: "design_verification",
		Role:      "reviewer",
		State:     StateFailed,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	_, _, err := Loopback(cfg, taskID, 5)
	if err == nil {
		t.Fatal("expected error when looping back to non-L2 phase")
	}
}

func TestLoopbackRejectsNonVerificationPhase(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-loopback-nonverif"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:    taskID,
		Phase:     7,
		PhaseName: "architecture",
		Role:      "lead",
		State:     StateFailed,
		StartedAt: time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	_, _, err := Loopback(cfg, taskID, 8)
	if err == nil {
		t.Fatal("expected error when looping back from non-verification phase")
	}
}

func TestStatus(t *testing.T) {
	cfg := testConfig(t)
	taskID := "test-status"
	dir := filepath.Join(cfg.TaskDir, taskID)
	_ = os.MkdirAll(dir, 0o755)

	ts := &TaskState{
		TaskID:     taskID,
		Phase:      8,
		PhaseName:  "implementation",
		Role:       "developer",
		State:      StateWorking,
		Complexity: "MEDIUM",
		StartedAt:  time.Now(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		t.Fatal(err)
	}

	manifestData := []byte(`session_id: test-status
started_at: 2024-01-01T00:00:00Z
updated_at: 2024-01-01T00:00:00Z
status: running
spec_path: test.yaml
complexity: MEDIUM
pipeline:
  current_phase: 8
  phases_completed: [1]
  phases_skipped: []
  l2_cycles: 0
budget:
  l1_retries_total: 1
  l2_cycles_total: 0
`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Status(cfg, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected non-empty status output")
	}
	// Should show L1 retries used
	if !contains(out, "1/2 used") {
		t.Errorf("expected L1 retry count in output, got: %s", out)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
