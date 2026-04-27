package task

import (
	"testing"
	"time"
)

func TestStore_CreateAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	task := &Task{
		ID:        "task-001",
		Runtime:   "opencode",
		Model:     "kimi",
		Status:    "pending",
		CreatedAt: now,
	}

	if err := store.Create(task); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("task-001")
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != "task-001" {
		t.Errorf("expected id task-001, got %s", got.ID)
	}
	if got.Runtime != "opencode" {
		t.Errorf("expected runtime opencode, got %s", got.Runtime)
	}
	if got.Status != "pending" {
		t.Errorf("expected status pending, got %s", got.Status)
	}
}

func TestStore_Update(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	task := &Task{
		ID:        "task-002",
		Status:    "running",
		CreatedAt: time.Now().UTC(),
	}
	store.Create(task)

	task.Status = "completed"
	exitCode := 0
	task.ExitCode = &exitCode
	store.Update(task)

	got, _ := store.Get("task-002")
	if got.Status != "completed" {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Error("expected exit code 0")
	}
}

func TestStore_List(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	now := time.Now().UTC()
	store.Create(&Task{ID: "t1", Status: "completed", CreatedAt: now})
	store.Create(&Task{ID: "t2", Status: "failed", CreatedAt: now})
	store.Create(&Task{ID: "t3", Status: "completed", CreatedAt: now})

	all, _ := store.List("")
	if len(all) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(all))
	}

	completed, _ := store.List("completed")
	if len(completed) != 2 {
		t.Errorf("expected 2 completed tasks, got %d", len(completed))
	}

	failed, _ := store.List("failed")
	if len(failed) != 1 {
		t.Errorf("expected 1 failed task, got %d", len(failed))
	}
}

func TestStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	_, err := store.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestStore_AddAttempt(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	now := time.Now().UTC()
	store.Create(&Task{
		ID:        "task-retry",
		Status:    "needs_clarification",
		CreatedAt: now,
		Attempts:  []Attempt{{Attempt: 1, StartedAt: now, Status: "needs_clarification"}},
	})

	store.AddAttempt("task-retry", Attempt{
		Attempt:   2,
		StartedAt: time.Now().UTC(),
		Status:    "running",
	})

	got, _ := store.Get("task-retry")
	if len(got.Attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", len(got.Attempts))
	}
	if got.Attempts[1].Attempt != 2 {
		t.Errorf("expected attempt 2, got %d", got.Attempts[1].Attempt)
	}
}

func TestStore_CleanStale(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	old := time.Now().UTC().Add(-2 * time.Hour)
	recent := time.Now().UTC().Add(-5 * time.Minute)

	store.Create(&Task{ID: "stale-running", Status: "running", CreatedAt: old})
	store.Create(&Task{ID: "stale-pending", Status: "pending", CreatedAt: old})
	store.Create(&Task{ID: "fresh-running", Status: "running", CreatedAt: recent})
	store.Create(&Task{ID: "old-completed", Status: "completed", CreatedAt: old})

	cleaned, err := store.CleanStale(1 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if len(cleaned) != 2 {
		t.Errorf("expected 2 cleaned, got %d: %v", len(cleaned), cleaned)
	}

	got, _ := store.Get("stale-running")
	if got.Status != "failed" {
		t.Errorf("expected failed, got %s", got.Status)
	}

	fresh, _ := store.Get("fresh-running")
	if fresh.Status != "running" {
		t.Errorf("fresh task should still be running, got %s", fresh.Status)
	}

	completed, _ := store.Get("old-completed")
	if completed.Status != "completed" {
		t.Errorf("completed task should be unchanged, got %s", completed.Status)
	}
}
