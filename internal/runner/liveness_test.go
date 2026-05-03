package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
)

func setupTestRunner(t *testing.T) (*Runner, *task.Store, string) {
	t.Helper()
	dir := t.TempDir()

	taskDir := filepath.Join(dir, "tasks")
	eventLog := filepath.Join(dir, "events.ndjson")

	store, err := task.NewStore(taskDir)
	if err != nil {
		t.Fatal(err)
	}

	emitter, err := events.NewEmitter(eventLog)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {
				Command:    "bash",
				PromptFlag: "-c",
				Models:     []string{"default"},
			},
		},
		Orchestrator: config.OrchestratorConfig{Runtime: "test", Model: "test"},
		TaskDir:      taskDir,
		EventLog:     eventLog,
		ClarifyExit:  10,
	}

	r := New(cfg, emitter, store)
	return r, store, dir
}

func createTestTask(t *testing.T, store *task.Store, taskID string) {
	t.Helper()
	now := time.Now().UTC()
	tk := &task.Task{
		ID:        taskID,
		Runtime:   "mock",
		Model:     "default",
		Status:    "running",
		CreatedAt: now,
		Attempts:  []task.Attempt{{Attempt: 1, StartedAt: now, Status: "running"}},
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}
}

func TestLiveness_ActiveWorkerNotKilled(t *testing.T) {
	r, store, _ := setupTestRunner(t)

	taskID := "test-active"
	createTestTask(t, store, taskID)

	// Worker prints BATON:H every 500ms for 3s. Silence timeout is 1s.
	// Worker should NOT be killed because it's active.
	script := `for i in $(seq 1 6); do echo "BATON:H:alive $i"; sleep 0.5; done; echo "BATON:M:done"`

	liveness := LivenessConfig{
		SilenceTimeout:  1 * time.Second,
		AbsoluteTimeout: 30 * time.Second,
		SilenceWarning:  500 * time.Millisecond,
		TickInterval:    100 * time.Millisecond,
	}

	os.MkdirAll(".baton/tasks/"+taskID, 0o755)
	defer os.RemoveAll(".baton/tasks/" + taskID)

	result, err := r.Run(context.Background(), taskID, "mock", "default", script, nil, liveness)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s (exit %d)", result.Status, result.ExitCode)
	}
}

func TestLiveness_SilentWorkerKilled(t *testing.T) {
	r, store, _ := setupTestRunner(t)

	taskID := "test-silent"
	createTestTask(t, store, taskID)

	// Worker prints one BATON marker then sleeps forever.
	// Silence timeout is 2s, ticker is 30s default but we test with tight timing.
	// We need the worker to go silent after being protocol-aware.
	script := `echo "BATON:H:starting"; sleep 300`

	liveness := LivenessConfig{
		SilenceTimeout:  2 * time.Second,
		AbsoluteTimeout: 10 * time.Second,
		SilenceWarning:  1 * time.Second,
		TickInterval:    100 * time.Millisecond,
	}

	os.MkdirAll(".baton/tasks/"+taskID, 0o755)
	defer os.RemoveAll(".baton/tasks/" + taskID)

	start := time.Now()
	result, err := r.Run(context.Background(), taskID, "mock", "default", script, nil, liveness)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "timeout" {
		t.Errorf("expected timeout, got %s", result.Status)
	}
	if elapsed > 8*time.Second {
		t.Errorf("took too long: %s (expected ~2-3s for silence timeout)", elapsed)
	}
}

func TestLiveness_NonProtocolUsesAbsoluteTimeout(t *testing.T) {
	r, store, _ := setupTestRunner(t)

	taskID := "test-nonprotocol"
	createTestTask(t, store, taskID)

	// Worker prints regular output (no BATON: markers) then sleeps.
	// Silence timeout should NOT apply because worker is not protocol-aware.
	// Absolute timeout of 2s should kill it.
	script := `echo "regular output"; sleep 300`

	liveness := LivenessConfig{
		SilenceTimeout:  1 * time.Second,
		AbsoluteTimeout: 2 * time.Second,
		SilenceWarning:  500 * time.Millisecond,
		TickInterval:    100 * time.Millisecond,
	}

	os.MkdirAll(".baton/tasks/"+taskID, 0o755)
	defer os.RemoveAll(".baton/tasks/" + taskID)

	start := time.Now()
	result, err := r.Run(context.Background(), taskID, "mock", "default", script, nil, liveness)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "timeout" {
		t.Errorf("expected timeout, got %s", result.Status)
	}
	if elapsed < 1500*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("unexpected elapsed time: %s (expected ~2s)", elapsed)
	}
}

func TestLiveness_StuckWorkerNotKilled(t *testing.T) {
	r, store, _ := setupTestRunner(t)

	taskID := "test-stuck"
	createTestTask(t, store, taskID)

	// Worker prints BATON:S (stuck) then waits 3s.
	// Silence timeout is 1s but stuck state should pause the timer.
	// After 3s, worker prints more output and exits.
	script := `echo "BATON:H:starting"; echo "BATON:S:which schema?"; sleep 3; echo "BATON:M:done"`

	liveness := LivenessConfig{
		SilenceTimeout:  1 * time.Second,
		AbsoluteTimeout: 30 * time.Second,
		SilenceWarning:  500 * time.Millisecond,
		TickInterval:    100 * time.Millisecond,
	}

	os.MkdirAll(".baton/tasks/"+taskID, 0o755)
	defer os.RemoveAll(".baton/tasks/" + taskID)

	result, err := r.Run(context.Background(), taskID, "mock", "default", script, nil, liveness)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s", result.Status)
	}
}
