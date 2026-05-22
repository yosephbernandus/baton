package tui

import (
	"os"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
)

func TestIntegrationFullReconcileFlow(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	oldEvent := now.Add(-2 * time.Hour)

	// --- Setup task YAML (source of truth) ---

	// Task A: completed in YAML, but events only show task_started (no task_completed)
	_ = store.Create(&task.Task{
		ID: "task-A", Runtime: "claude-code", Model: "opus",
		Status: "completed", Duration: "30m0s", CreatedAt: yesterday,
	})

	// Task B: killed in YAML, events only show task_created (pending)
	_ = store.Create(&task.Task{
		ID: "task-B", Runtime: "opencode", Model: "kimi",
		Status: "killed", CreatedAt: yesterday,
	})

	// Task C: running in YAML with dead PID, events show running
	_ = store.Create(&task.Task{
		ID: "task-C", Runtime: "claude-code", Model: "sonnet",
		Status: "running", PID: 99999, CreatedAt: yesterday,
	})

	// Task D: running in YAML, PID is current process (alive but recycled scenario),
	// started 24h ago, no events for 2h → zombie
	_ = store.Create(&task.Task{
		ID: "task-D", Runtime: "claude-code", Model: "opus",
		Status: "running", PID: os.Getpid(), CreatedAt: yesterday,
	})

	// Task E: genuinely running (alive PID, recent events) → should stay running
	_ = store.Create(&task.Task{
		ID: "task-E", Runtime: "claude-code", Model: "opus",
		Status: "running", PID: os.Getpid(), CreatedAt: now,
	})

	// Task F: running, no PID, started 24h ago → zombie
	_ = store.Create(&task.Task{
		ID: "task-F", Runtime: "opencode", Model: "kimi",
		Status: "running", CreatedAt: yesterday,
	})

	// --- Setup events (what TUI sees) ---
	evts := []events.Event{
		{Timestamp: yesterday, TaskID: "task-A", Runtime: "claude-code", Model: "opus", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "task-A", Runtime: "claude-code", Model: "opus", EventType: "task_started"},
		// No task_completed for A

		{Timestamp: yesterday, TaskID: "task-B", Runtime: "opencode", Model: "kimi", EventType: "task_created"},
		// No task_started, no task_killed for B

		{Timestamp: yesterday, TaskID: "task-C", Runtime: "claude-code", Model: "sonnet", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "task-C", Runtime: "claude-code", Model: "sonnet", EventType: "task_started"},
		{Timestamp: yesterday.Add(2 * time.Second), TaskID: "task-C", EventType: "worker_pid", Data: map[string]interface{}{"pid": float64(99999)}},

		{Timestamp: yesterday, TaskID: "task-D", Runtime: "claude-code", Model: "opus", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "task-D", Runtime: "claude-code", Model: "opus", EventType: "task_started"},
		{Timestamp: oldEvent, TaskID: "task-D", EventType: "worker_progress", Data: map[string]interface{}{"msg": "last seen"}},

		{Timestamp: now, TaskID: "task-E", Runtime: "claude-code", Model: "opus", EventType: "task_created"},
		{Timestamp: now.Add(time.Second), TaskID: "task-E", Runtime: "claude-code", Model: "opus", EventType: "task_started"},
		{Timestamp: now.Add(2 * time.Second), TaskID: "task-E", EventType: "worker_progress", Data: map[string]interface{}{"msg": "working"}},

		{Timestamp: yesterday, TaskID: "task-F", Runtime: "opencode", Model: "kimi", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "task-F", Runtime: "opencode", Model: "kimi", EventType: "task_started"},
		{Timestamp: oldEvent, TaskID: "task-F", EventType: "worker_progress", Data: map[string]interface{}{"msg": "last activity"}},
	}

	// --- Create model, process events ---
	reapCh := make(chan string, 20)
	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
		store:      store,
		reapCh:     reapCh,
	}

	for _, ev := range evts {
		m.processEvent(ev)
	}

	// Verify event-only state (before reconciliation)
	preReconcile := map[string]string{
		"task-A": "running",
		"task-B": "pending",
		"task-C": "running",
		"task-D": "running",
		"task-E": "running",
		"task-F": "running",
	}
	for id, expect := range preReconcile {
		if m.tasks[id].Status != expect {
			t.Errorf("[pre-reconcile] %s: expected %q, got %q", id, expect, m.tasks[id].Status)
		}
	}
	t.Log("Pre-reconcile states correct")

	// --- Run reconciliation (first tick) ---
	m.reconcileWithStore()

	postReconcile := map[string]string{
		"task-A": "completed", // YAML=completed → fixed
		"task-B": "killed",    // YAML=killed → fixed
		"task-C": "failed",    // dead PID → failed
		"task-D": "running",   // PID alive (recycled), reconcile only checks PID, zombie check is in checkDeadProcesses
		"task-E": "running",   // genuinely running
		"task-F": "running",   // no PID, zombie check in checkDeadProcesses
	}
	for id, expect := range postReconcile {
		if m.tasks[id].Status != expect {
			t.Errorf("[post-reconcile] %s: expected %q, got %q", id, expect, m.tasks[id].Status)
		}
	}
	t.Log("Post-reconcile states correct")

	// Verify duration pulled from store for task-A
	if m.tasks["task-A"].Duration != "30m0s" {
		t.Errorf("task-A duration: expected '30m0s', got %q", m.tasks["task-A"].Duration)
	}

	// --- Run checkDeadProcesses (subsequent tick) ---
	m.checkDeadProcesses()

	postCheck := map[string]string{
		"task-A": "completed",
		"task-B": "killed",
		"task-C": "failed",
		"task-D": "failed",   // zombie: alive PID but started 24h ago, no events 2h → failed
		"task-E": "running",  // alive PID, recent events → still running
		"task-F": "failed",   // zombie: no PID, started 24h ago, no events 2h → failed
	}
	for id, expect := range postCheck {
		if m.tasks[id].Status != expect {
			t.Errorf("[post-checkDead] %s: expected %q, got %q", id, expect, m.tasks[id].Status)
		}
	}
	t.Log("Post-checkDeadProcesses states correct")

	// --- Verify active view filtering ---
	m.showAll = false
	active := m.visibleTasks()
	activeMap := make(map[string]bool)
	for _, id := range active {
		activeMap[id] = true
	}

	// Only task-E should be active
	if len(active) != 1 || !activeMap["task-E"] {
		t.Errorf("Active view: expected only [task-E], got %v", active)
	}
	t.Log("Active view filtering correct")

	// All view should show everything
	m.showAll = true
	all := m.visibleTasks()
	if len(all) != 6 {
		t.Errorf("All view: expected 6 tasks, got %d", len(all))
	}

	// --- Verify reap channel ---
	close(reapCh)
	reaped := make(map[string]bool)
	for id := range reapCh {
		reaped[id] = true
	}

	expectedReaped := []string{"task-C", "task-D", "task-F"}
	for _, id := range expectedReaped {
		if !reaped[id] {
			t.Errorf("%s should have been sent to reap channel", id)
		}
	}
	if reaped["task-A"] || reaped["task-B"] || reaped["task-E"] {
		t.Error("task-A, B, E should NOT be in reap channel")
	}
	t.Logf("Reap channel correct: %v", expectedReaped)
}
