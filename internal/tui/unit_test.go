package tui

import (
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/events"
)

func newTestModel() *Model {
	return &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
		width:      120,
		height:     40,
	}
}

func TestIsTerminalStatus(t *testing.T) {
	terminal := []string{"completed", "failed", "killed", "timeout", "deferred"}
	for _, s := range terminal {
		if !isTerminalStatus(s) {
			t.Errorf("%q should be terminal", s)
		}
	}

	nonTerminal := []string{"running", "pending", "needs_clarification", "needs_human", ""}
	for _, s := range nonTerminal {
		if isTerminalStatus(s) {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

func TestProcessEventTaskCreated(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{
		TaskID:    "t1",
		Runtime:   "opencode",
		Model:     "kimi",
		EventType: "task_created",
	})

	if len(m.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(m.tasks))
	}
	ts := m.tasks["t1"]
	if ts.Status != "pending" {
		t.Errorf("expected pending, got %s", ts.Status)
	}
	if ts.Runtime != "opencode" {
		t.Errorf("expected opencode, got %s", ts.Runtime)
	}
	if ts.Model != "kimi" {
		t.Errorf("expected kimi, got %s", ts.Model)
	}
	if len(m.taskOrder) != 1 || m.taskOrder[0] != "t1" {
		t.Errorf("expected taskOrder [t1], got %v", m.taskOrder)
	}
}

func TestProcessEventTaskStarted(t *testing.T) {
	m := newTestModel()
	now := time.Now()
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "task_started",
		Timestamp: now,
		Data:      map[string]interface{}{"pid": float64(1234)},
	})

	ts := m.tasks["t1"]
	if ts.Status != "running" {
		t.Errorf("expected running, got %s", ts.Status)
	}
	if ts.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", ts.PID)
	}
}

func TestProcessEventOutput(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_created"})

	for i := 0; i < 10; i++ {
		m.processEvent(events.Event{
			TaskID:    "t1",
			EventType: "output",
			Data:      map[string]interface{}{"line": "output line"},
		})
	}

	ts := m.tasks["t1"]
	if len(ts.Output) != 10 {
		t.Errorf("expected 10 output lines, got %d", len(ts.Output))
	}
}

func TestProcessEventOutputTruncation(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_created"})

	for i := 0; i < 600; i++ {
		m.processEvent(events.Event{
			TaskID:    "t1",
			EventType: "output",
			Data:      map[string]interface{}{"line": "line"},
		})
	}

	ts := m.tasks["t1"]
	if len(ts.Output) != 500 {
		t.Errorf("expected 500 (truncated), got %d", len(ts.Output))
	}
}

func TestProcessEventTaskCompleted(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_started"})
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "task_completed",
		Data:      map[string]interface{}{"duration": "2m15s"},
	})

	ts := m.tasks["t1"]
	if ts.Status != "completed" {
		t.Errorf("expected completed, got %s", ts.Status)
	}
	if ts.Duration != "2m15s" {
		t.Errorf("expected 2m15s, got %s", ts.Duration)
	}
}

func TestProcessEventTaskFailed(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "task_failed",
		Data:      map[string]interface{}{"duration": "30s"},
	})

	if m.tasks["t1"].Status != "failed" {
		t.Errorf("expected failed, got %s", m.tasks["t1"].Status)
	}
}

func TestProcessEventTimeout(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "task_timeout",
		Data:      map[string]interface{}{"duration": "10m"},
	})

	if m.tasks["t1"].Status != "timeout" {
		t.Errorf("expected timeout, got %s", m.tasks["t1"].Status)
	}
}

func TestProcessEventClarification(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "needs_clarification",
		Data:      map[string]interface{}{"clarification": "which database?"},
	})

	ts := m.tasks["t1"]
	if ts.Status != "needs_clarification" {
		t.Errorf("expected needs_clarification, got %s", ts.Status)
	}
	if ts.Clarify != "which database?" {
		t.Errorf("expected clarification text, got %q", ts.Clarify)
	}
}

func TestProcessEventKilled(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_killed"})
	if m.tasks["t1"].Status != "killed" {
		t.Errorf("expected killed, got %s", m.tasks["t1"].Status)
	}
}

func TestProcessEventDeferred(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_deferred"})
	if m.tasks["t1"].Status != "deferred" {
		t.Errorf("expected deferred, got %s", m.tasks["t1"].Status)
	}
}

func TestProcessEventPhaseStarted(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "phase_started",
		Data:      map[string]interface{}{"phase_name": "implementation", "role": "developer"},
	})

	ts := m.tasks["t1"]
	if ts.Status != "running" {
		t.Errorf("expected running, got %s", ts.Status)
	}
	if ts.Progress != "implementation" {
		t.Errorf("expected implementation, got %s", ts.Progress)
	}
}

func TestProcessEventPhaseStuck(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "phase_stuck",
		Data:      map[string]interface{}{"phase_name": "testing"},
	})

	ts := m.tasks["t1"]
	if !ts.Stuck {
		t.Error("expected stuck=true")
	}
	if ts.Progress != "testing" {
		t.Errorf("expected testing, got %s", ts.Progress)
	}
}

func TestProcessEventWorkerHeartbeat(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_started"})
	m.tasks["t1"].Stuck = true

	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "worker_heartbeat",
		Data:      map[string]interface{}{"msg": "processing files"},
	})

	ts := m.tasks["t1"]
	if ts.Stuck {
		t.Error("heartbeat should clear stuck")
	}
	if ts.Progress != "processing files" {
		t.Errorf("expected progress update, got %s", ts.Progress)
	}
}

func TestProcessEventL2LoopBack(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "l2_loop_back"})

	ts := m.tasks["t1"]
	if ts.Progress != "L2 loop back" {
		t.Errorf("expected L2 loop back, got %s", ts.Progress)
	}
}

func TestProcessEventEmptyTaskID(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{EventType: "task_created"})

	if len(m.tasks) != 0 {
		t.Error("empty task ID should be ignored")
	}
}

func TestProcessEventWorkerPID(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_created"})
	m.processEvent(events.Event{
		TaskID:    "t1",
		EventType: "worker_pid",
		Data:      map[string]interface{}{"pid": float64(5678)},
	})

	if m.tasks["t1"].PID != 5678 {
		t.Errorf("expected PID 5678, got %d", m.tasks["t1"].PID)
	}
}

func TestProcessEventAdvisorConsulted(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "advisor_consulted"})

	if m.tasks["t1"].Progress != "consulting advisor" {
		t.Errorf("expected consulting advisor, got %s", m.tasks["t1"].Progress)
	}
}

func TestProcessEventRateLimited(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "phase_rate_limited"})

	ts := m.tasks["t1"]
	if ts.Progress != "rate limited" {
		t.Errorf("expected rate limited, got %s", ts.Progress)
	}
	if !ts.Stuck {
		t.Error("rate limited should set stuck")
	}
}

func TestVisibleTasksShowAll(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_created"})
	m.processEvent(events.Event{TaskID: "t2", EventType: "task_completed"})
	m.processEvent(events.Event{TaskID: "t3", EventType: "task_started"})

	m.showAll = true
	visible := m.visibleTasks()
	if len(visible) != 3 {
		t.Errorf("showAll should show 3, got %d", len(visible))
	}
}

func TestVisibleTasksHideTerminal(t *testing.T) {
	m := newTestModel()
	m.processEvent(events.Event{TaskID: "t1", EventType: "task_created"})
	m.processEvent(events.Event{TaskID: "t2", EventType: "task_completed"})
	m.processEvent(events.Event{TaskID: "t3", EventType: "task_started"})

	m.showAll = false
	visible := m.visibleTasks()
	if len(visible) != 2 {
		t.Errorf("should hide completed, expected 2, got %d: %v", len(visible), visible)
	}
}

func TestMaxVisibleTasks(t *testing.T) {
	m := newTestModel()
	m.height = 40
	m.splitRatio = 0.4

	max := m.maxVisibleTasks()
	if max <= 0 {
		t.Errorf("expected positive max, got %d", max)
	}
	if max > m.height {
		t.Errorf("max %d should not exceed height %d", max, m.height)
	}
}

func TestOutputViewportHeight(t *testing.T) {
	m := newTestModel()
	m.height = 40
	m.splitRatio = 0.4

	h := m.outputViewportHeight()
	if h <= 0 {
		t.Errorf("expected positive height, got %d", h)
	}
	if h > m.height {
		t.Errorf("viewport height %d should not exceed total height %d", h, m.height)
	}
}
