package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/task"
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
