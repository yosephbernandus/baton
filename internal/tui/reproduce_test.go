package tui

import (
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/events"
)

func TestReproducePipelinePhaseNotShowing(t *testing.T) {
	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
	}

	now := time.Now().UTC()
	taskID := "s7-01-wasm-rendering-fix-phase-8"

	// Real event sequence from buatin-slide pipeline run:
	// 1. phase_started (pipeline emits)
	// 2. task_started (runner emits)
	// 3. task_failed (reap goroutine — false positive)
	// 4. task_completed (runner finishes)
	// 5. phase_completed (pipeline emits)

	m.processEvent(events.Event{
		Timestamp: now, TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_started",
		Data: map[string]interface{}{
			"complexity": "MEDIUM", "phase_id": float64(8),
			"phase_name": "implementation", "role": "developer",
		},
	})

	// BUG (old code): phase_started was unhandled, so status stayed ""
	// FIX (new code): phase_started sets status=running, progress=implementation, runtime=developer
	if m.tasks[taskID].Status != "running" {
		t.Errorf("after phase_started: want 'running', got %q — phase_started was not handled", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].Progress != "implementation" {
		t.Errorf("after phase_started: want progress 'implementation', got %q", m.tasks[taskID].Progress)
	}
	if m.tasks[taskID].Runtime != "developer" {
		t.Errorf("after phase_started: want runtime 'developer', got %q", m.tasks[taskID].Runtime)
	}

	m.processEvent(events.Event{
		Timestamp: now.Add(time.Second), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "task_started", Data: map[string]interface{}{"attempt": float64(1)},
	})
	if m.tasks[taskID].Status != "running" {
		t.Errorf("after task_started: want 'running', got %q", m.tasks[taskID].Status)
	}

	// Reap goroutine false positive
	m.processEvent(events.Event{
		Timestamp: now.Add(2 * time.Minute), TaskID: taskID,
		EventType: "task_failed", Data: map[string]interface{}{"reason": "process_dead"},
	})

	// Runner actually completes
	m.processEvent(events.Event{
		Timestamp: now.Add(2*time.Minute + 5*time.Second), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "task_completed", Data: map[string]interface{}{"duration": "2m0s", "exit_code": float64(0)},
	})

	m.processEvent(events.Event{
		Timestamp: now.Add(2*time.Minute + 6*time.Second), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_completed",
		Data: map[string]interface{}{
			"phase_id": float64(8), "phase_name": "implementation", "role": "developer",
		},
	})

	if m.tasks[taskID].Status != "completed" {
		t.Errorf("final: want 'completed', got %q", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].Duration != "2m0s" {
		t.Errorf("final: want duration '2m0s', got %q", m.tasks[taskID].Duration)
	}
}

func TestReproducePipelineRerunStaleCompleted(t *testing.T) {
	dir := t.TempDir()

	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
	}

	now := time.Now().UTC()
	taskID := "theme-editor-section-resize-handle-phase-8"

	// --- First run: completes normally ---
	m.processEvent(events.Event{
		Timestamp: now, TaskID: taskID, Runtime: "opencode", Model: "crofai/qwen3.5",
		EventType: "phase_started",
		Data: map[string]interface{}{"phase_name": "implementation", "role": "developer"},
	})
	m.processEvent(events.Event{
		Timestamp: now.Add(time.Second), TaskID: taskID, Runtime: "opencode", Model: "crofai/qwen3.5",
		EventType: "task_started", Data: map[string]interface{}{"attempt": float64(1)},
	})
	m.processEvent(events.Event{
		Timestamp: now.Add(14 * time.Minute), TaskID: taskID, Runtime: "opencode", Model: "crofai/qwen3.5",
		EventType: "task_completed", Data: map[string]interface{}{"duration": "14m17s", "exit_code": float64(0)},
	})
	m.processEvent(events.Event{
		Timestamp: now.Add(14*time.Minute + time.Second), TaskID: taskID,
		EventType: "phase_completed",
		Data: map[string]interface{}{"phase_name": "implementation"},
	})

	if m.tasks[taskID].Status != "completed" {
		t.Fatalf("first run: want completed, got %q", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].Duration != "14m17s" {
		t.Errorf("first run: want duration '14m17s', got %q", m.tasks[taskID].Duration)
	}

	// Reconcile runs, marks task as reconciled
	runReconcile(m)

	// --- Pipeline reruns (L2 loop or manual rerun) ---
	// BUG (old code): no handler for phase_started, and reconciled=true prevents
	// re-checking. Task stays "completed" even though it's running again.
	rerunTime := now.Add(20 * time.Minute)

	m.processEvent(events.Event{
		Timestamp: rerunTime, TaskID: taskID, Runtime: "opencode", Model: "crofai/qwen3.5",
		EventType: "phase_started",
		Data: map[string]interface{}{"phase_name": "implementation", "role": "developer"},
	})

	// FIX: phase_started resets status to running AND clears reconciled flag
	if m.tasks[taskID].Status != "running" {
		t.Errorf("rerun: want 'running', got %q — phase_started should override completed on rerun", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].reconciled {
		t.Error("rerun: reconciled should be reset so store re-check happens")
	}

	_ = dir
}

func TestReproduceBlankTasksInMonitor(t *testing.T) {
	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
	}

	now := time.Now().UTC()

	// Reproduce: events that only emit pipeline-specific types (no task_created)
	// Old code had no handler → task created with empty status

	evts := []events.Event{
		// phase_stuck — old code: unhandled, status stays ""
		{Timestamp: now, TaskID: "stuck-task", EventType: "phase_stuck",
			Data: map[string]interface{}{"phase_name": "testing"}},
		// phase_rate_limited — old code: unhandled
		{Timestamp: now, TaskID: "ratelimit-task", EventType: "phase_rate_limited"},
		// phase_blocked — old code: unhandled
		{Timestamp: now, TaskID: "blocked-task", EventType: "phase_blocked",
			Data: map[string]interface{}{"phase_name": "review"}},
		// advisor_consulted — old code: unhandled
		{Timestamp: now, TaskID: "advisor-task", EventType: "advisor_consulted"},
	}

	for _, ev := range evts {
		m.processEvent(ev)
	}

	// Verify no task has empty status
	for id, ts := range m.tasks {
		if ts.Status == "" {
			t.Errorf("%s has empty status — event type was unhandled", id)
		}
	}

	// phase_stuck should set Stuck flag
	if !m.tasks["stuck-task"].Stuck {
		t.Error("stuck-task should have Stuck=true")
	}
	if !m.tasks["ratelimit-task"].Stuck {
		t.Error("ratelimit-task should have Stuck=true")
	}
	if !m.tasks["blocked-task"].Stuck {
		t.Error("blocked-task should have Stuck=true")
	}
}
