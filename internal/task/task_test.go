package task

import (
	"fmt"
	"os"
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
	_ = store.Create(task)

	task.Status = "completed"
	exitCode := 0
	task.ExitCode = &exitCode
	_ = store.Update(task)

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
	_ = store.Create(&Task{ID: "t1", Status: "completed", CreatedAt: now})
	_ = store.Create(&Task{ID: "t2", Status: "failed", CreatedAt: now})
	_ = store.Create(&Task{ID: "t3", Status: "completed", CreatedAt: now})

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
	_ = store.Create(&Task{
		ID:        "task-retry",
		Status:    "needs_clarification",
		CreatedAt: now,
		Attempts:  []Attempt{{Attempt: 1, StartedAt: now, Status: "needs_clarification"}},
	})

	_ = store.AddAttempt("task-retry", Attempt{
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

	_ = store.Create(&Task{ID: "stale-running", Status: "running", CreatedAt: old})
	_ = store.Create(&Task{ID: "stale-pending", Status: "pending", CreatedAt: old})
	_ = store.Create(&Task{ID: "fresh-running", Status: "running", CreatedAt: recent})
	_ = store.Create(&Task{ID: "old-completed", Status: "completed", CreatedAt: old})

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

func TestStore_KillTask(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	now := time.Now().UTC()
	_ = store.Create(&Task{
		ID:        "kill-me",
		Status:    "running",
		CreatedAt: now,
		PID:       0,
	})

	if err := store.KillTask("kill-me"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get("kill-me")
	if got.Status != "killed" {
		t.Errorf("expected killed, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestStore_KillTaskNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	err := store.KillTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestStore_TouchActivity(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	now := time.Now().UTC()
	_ = store.Create(&Task{
		ID:        "active-task",
		Status:    "running",
		CreatedAt: now,
	})

	if err := store.TouchActivity("active-task"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get("active-task")
	if got.LastActivity == nil {
		t.Fatal("expected LastActivity to be set")
	}
	if got.LastActivity.Before(now) {
		t.Error("LastActivity should be after creation time")
	}
}

func TestStore_TouchActivityNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	err := store.TouchActivity("ghost")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestStore_ReapDead(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	now := time.Now().UTC()
	// PID 0 or negative → skipped (not running process)
	_ = store.Create(&Task{
		ID:        "no-pid",
		Status:    "running",
		CreatedAt: now,
		PID:       0,
	})
	// PID of a dead process — use a very high PID unlikely to exist
	_ = store.Create(&Task{
		ID:        "dead-pid",
		Status:    "running",
		CreatedAt: now,
		PID:       999999999,
	})
	// Non-running task should be ignored
	_ = store.Create(&Task{
		ID:        "completed-task",
		Status:    "completed",
		CreatedAt: now,
		PID:       999999999,
	})

	reaped, err := store.ReapDead()
	if err != nil {
		t.Fatal(err)
	}

	// dead-pid should be reaped (PID 999999999 doesn't exist)
	found := false
	for _, id := range reaped {
		if id == "dead-pid" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dead-pid to be reaped, got: %v", reaped)
	}

	got, _ := store.Get("dead-pid")
	if got.Status != "failed" {
		t.Errorf("expected failed, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}

	// no-pid should NOT be reaped (PID <= 0 is skipped)
	noPid, _ := store.Get("no-pid")
	if noPid.Status != "running" {
		t.Errorf("no-pid should remain running, got %s", noPid.Status)
	}

	// completed should be unchanged
	comp, _ := store.Get("completed-task")
	if comp.Status != "completed" {
		t.Errorf("completed should be unchanged, got %s", comp.Status)
	}
}

func TestProcessAlive(t *testing.T) {
	// Current process should be alive
	if !ProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}

	// PID 0 → false
	if ProcessAlive(0) {
		t.Error("PID 0 should return false")
	}

	// Negative PID → false
	if ProcessAlive(-1) {
		t.Error("negative PID should return false")
	}

	// Very high PID → false (doesn't exist)
	if ProcessAlive(999999999) {
		t.Error("PID 999999999 should not be alive")
	}
}

func TestProcessGroupAlive(t *testing.T) {
	// PID 0 → false
	if ProcessGroupAlive(0) {
		t.Error("PID 0 should return false")
	}

	// Negative PID → false
	if ProcessGroupAlive(-1) {
		t.Error("negative PID should return false")
	}

	// Very high PID → false
	if ProcessGroupAlive(999999999) {
		t.Error("PID 999999999 should not have a process group")
	}
}

func TestStore_ListFilterNoMatch(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	now := time.Now().UTC()
	_ = store.Create(&Task{ID: "t1", Status: "completed", CreatedAt: now})

	running, _ := store.List("running")
	if len(running) != 0 {
		t.Errorf("expected 0 running tasks, got %d", len(running))
	}
}

func TestStore_ListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	tasks, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks in empty dir, got %d", len(tasks))
	}
}

func TestStore_OutputTailCapped(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	tk := &Task{
		ID:         "tail-test",
		Status:     "completed",
		OutputTail: lines,
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("tail-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.OutputTail) != maxOutputTailLines {
		t.Errorf("expected %d lines, got %d", maxOutputTailLines, len(got.OutputTail))
	}
}

func TestStore_ListRecent(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	for i := 0; i < 10; i++ {
		tk := &Task{
			ID:        fmt.Sprintf("task-%03d", i),
			Status:    "completed",
			CreatedAt: time.Now().UTC(),
		}
		_ = store.Create(tk)
	}

	tasks, err := store.ListRecent(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestStore_ListRecentAll(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	for i := 0; i < 5; i++ {
		tk := &Task{
			ID:        fmt.Sprintf("task-%03d", i),
			Status:    "completed",
			CreatedAt: time.Now().UTC(),
		}
		_ = store.Create(tk)
	}

	tasks, err := store.ListRecent(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}
}

func TestStore_ListRecentDefault(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	tasks, err := store.ListRecent(0)
	if err != nil {
		t.Fatal(err)
	}
	if tasks != nil {
		t.Errorf("expected nil for empty dir, got %v", tasks)
	}
}
