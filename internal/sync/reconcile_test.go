package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/task"
	"gopkg.in/yaml.v3"
)

func TestReconcileUpdatesStaleTask(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(filepath.Join(dir, "tasks"))

	_ = store.Create(&task.Task{ID: "t1", Status: "running", CreatedAt: time.Now()})
	_ = store.Create(&task.Task{ID: "t2", Status: "running", CreatedAt: time.Now()})
	_ = store.Create(&task.Task{ID: "t3", Status: "completed", CreatedAt: time.Now()})

	eventsPath := filepath.Join(dir, "events.ndjson")
	writeEvents(t, eventsPath, []rawEvent{
		{Timestamp: time.Now(), TaskID: "t1", EventType: "task_completed"},
		{Timestamp: time.Now(), TaskID: "t2", EventType: "task_killed"},
	})

	count, err := Reconcile(eventsPath, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("want 2 reconciled, got %d", count)
	}

	t1, _ := store.Get("t1")
	if t1.Status != "completed" {
		t.Errorf("t1: want completed, got %q", t1.Status)
	}
	t2, _ := store.Get("t2")
	if t2.Status != "killed" {
		t.Errorf("t2: want killed, got %q", t2.Status)
	}
	t3, _ := store.Get("t3")
	if t3.Status != "completed" {
		t.Errorf("t3: should remain completed, got %q", t3.Status)
	}
}

func TestReconcileNoEventsFile(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(filepath.Join(dir, "tasks"))

	count, err := Reconcile(filepath.Join(dir, "missing.ndjson"), store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0, got %d", count)
	}
}

func TestReconcileLastEventWins(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(filepath.Join(dir, "tasks"))

	_ = store.Create(&task.Task{ID: "t1", Status: "running", CreatedAt: time.Now()})

	eventsPath := filepath.Join(dir, "events.ndjson")
	writeEvents(t, eventsPath, []rawEvent{
		{Timestamp: time.Now(), TaskID: "t1", EventType: "task_failed"},
		{Timestamp: time.Now().Add(time.Second), TaskID: "t1", EventType: "task_completed"},
	})

	count, err := Reconcile(eventsPath, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 reconciled, got %d", count)
	}

	t1, _ := store.Get("t1")
	if t1.Status != "completed" {
		t.Errorf("t1: want completed (last event wins), got %q", t1.Status)
	}
}

func TestReconcileSubdirsStaleManifest(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "tasks")
	subdir := filepath.Join(taskDir, "test-task")
	_ = os.MkdirAll(subdir, 0o755)

	manifest := `session_id: test-task
status: running
worker_pid: 99999
updated_at: 2026-01-01T00:00:00Z
`
	_ = os.WriteFile(filepath.Join(subdir, "manifest.yaml"), []byte(manifest), 0o644)

	count, err := ReconcileSubdirs(taskDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 reconciled, got %d", count)
	}

	data, _ := os.ReadFile(filepath.Join(subdir, "manifest.yaml"))
	var m subdirManifest
	_ = yaml.Unmarshal(data, &m)
	if m.Status != "failed" {
		t.Errorf("want failed, got %q", m.Status)
	}
}

func TestReconcileSubdirsStaleWorkerState(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "tasks")
	subdir := filepath.Join(taskDir, "test-task")
	_ = os.MkdirAll(subdir, 0o755)

	ws := `task_id: test-task
state: started
worker_pid: 99999
`
	_ = os.WriteFile(filepath.Join(subdir, "worker-state.yaml"), []byte(ws), 0o644)

	count, err := ReconcileSubdirs(taskDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 reconciled, got %d", count)
	}

	data, _ := os.ReadFile(filepath.Join(subdir, "worker-state.yaml"))
	var state subdirWorkerState
	_ = yaml.Unmarshal(data, &state)
	if state.State != "failed" {
		t.Errorf("want failed, got %q", state.State)
	}
}

func TestReconcileSubdirsSkipsLivePID(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "tasks")
	subdir := filepath.Join(taskDir, "live-task")
	_ = os.MkdirAll(subdir, 0o755)

	manifest := fmt.Sprintf(`session_id: live-task
status: running
worker_pid: %d
`, os.Getpid())
	_ = os.WriteFile(filepath.Join(subdir, "manifest.yaml"), []byte(manifest), 0o644)

	count, err := ReconcileSubdirs(taskDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0 reconciled (live PID), got %d", count)
	}
}

func TestReconcileSubdirsSkipsCompletedManifest(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "tasks")
	subdir := filepath.Join(taskDir, "done-task")
	_ = os.MkdirAll(subdir, 0o755)

	manifest := `session_id: done-task
status: completed
worker_pid: 99999
`
	_ = os.WriteFile(filepath.Join(subdir, "manifest.yaml"), []byte(manifest), 0o644)

	count, err := ReconcileSubdirs(taskDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0 reconciled (already completed), got %d", count)
	}
}

func writeEvents(t *testing.T, path string, evts []rawEvent) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, ev := range evts {
		if err := enc.Encode(ev); err != nil {
			t.Fatal(err)
		}
	}
}
