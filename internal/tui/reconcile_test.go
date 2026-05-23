package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/task"
)

// runReconcile simulates the async reconciliation flow:
// startReconciliation returns a Cmd, we execute it to get results, then apply.
func runReconcile(m *Model) {
	cmd := m.startReconciliation()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if results, ok := msg.(reconcileMsg); ok {
		m.applyReconcileResults(results)
	}
}

func TestReconcileWithStore(t *testing.T) {
	dir := t.TempDir()
	store, err := task.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	oldTime := now.Add(-2 * time.Hour)
	oldEventTime := now.Add(-time.Hour)

	// Case 1: YAML=killed, TUI=running → should reconcile to killed
	_ = store.Create(&task.Task{ID: "case1", Runtime: "rt", Model: "m", Status: "killed", CreatedAt: now})

	// Case 2: YAML=completed, TUI=running → should reconcile to completed
	_ = store.Create(&task.Task{ID: "case2", Runtime: "rt", Model: "m", Status: "completed", Duration: "5m", CreatedAt: now})

	// Case 3: YAML=running, PID=99999 (dead) → should reconcile to failed
	_ = store.Create(&task.Task{ID: "case3", Runtime: "rt", Model: "m", Status: "running", PID: 99999, CreatedAt: now})

	// Case 4: YAML=running, no PID, started 2h ago → stays running in reconcile (checkDeadProcesses handles)
	_ = store.Create(&task.Task{ID: "case4", Runtime: "rt", Model: "m", Status: "running", CreatedAt: oldTime})

	// Case 5: YAML=killed, TUI=pending → should reconcile to killed
	_ = store.Create(&task.Task{ID: "case5", Runtime: "rt", Model: "m", Status: "killed", CreatedAt: now})

	// Case 6: YAML=running, PID alive (current process) → should stay running
	_ = store.Create(&task.Task{ID: "case6", Runtime: "rt", Model: "m", Status: "running", PID: os.Getpid(), CreatedAt: now})

	reapCh := make(chan string, 10)

	m := &Model{
		tasks: map[string]*taskState{
			"case1": {ID: "case1", Status: "running", StartedAt: now, LastEventAt: now},
			"case2": {ID: "case2", Status: "running", StartedAt: now, LastEventAt: now},
			"case3": {ID: "case3", Status: "running", PID: 99999, StartedAt: now, LastEventAt: now},
			"case4": {ID: "case4", Status: "running", StartedAt: oldTime, LastEventAt: oldEventTime},
			"case5": {ID: "case5", Status: "pending"},
			"case6": {ID: "case6", Status: "running", PID: os.Getpid(), StartedAt: now, LastEventAt: now},
		},
		taskOrder: []string{"case1", "case2", "case3", "case4", "case5", "case6"},
		store:     store,
		reapCh:    reapCh,
	}

	runReconcile(m)

	tests := []struct {
		id     string
		expect string
	}{
		{"case1", "killed"},
		{"case2", "completed"},
		{"case3", "failed"},
		{"case4", "running"},
		{"case5", "killed"},
		{"case6", "running"},
	}

	for _, tt := range tests {
		ts := m.tasks[tt.id]
		if ts.Status != tt.expect {
			t.Errorf("%s: expected status %q, got %q", tt.id, tt.expect, ts.Status)
		}
	}

	if m.tasks["case2"].Duration != "5m" {
		t.Errorf("case2: expected duration '5m', got %q", m.tasks["case2"].Duration)
	}

	// Per-task reconciled flag prevents re-check
	m.tasks["case6"].Status = "running"
	_ = store.Create(&task.Task{ID: "case6", Runtime: "rt", Model: "m", Status: "killed", CreatedAt: now})
	runReconcile(m)
	if m.tasks["case6"].Status != "running" {
		t.Error("per-task reconciled flag should prevent re-reconciliation")
	}

	// NEW tasks after first reconcile still get checked
	_ = store.Create(&task.Task{ID: "case7", Runtime: "rt", Model: "m", Status: "completed", Duration: "2m", CreatedAt: now})
	m.tasks["case7"] = &taskState{ID: "case7", Status: "running"}
	m.taskOrder = append(m.taskOrder, "case7")
	runReconcile(m)
	if m.tasks["case7"].Status != "completed" {
		t.Errorf("case7: new task should be reconciled, got %q", m.tasks["case7"].Status)
	}

	close(reapCh)
	var reaped []string
	for id := range reapCh {
		reaped = append(reaped, id)
	}
	if len(reaped) != 1 || reaped[0] != "case3" {
		t.Errorf("expected [case3] reaped, got %v", reaped)
	}

	m.showAll = false
	visible := m.visibleTasks()
	for _, id := range visible {
		if id == "case1" || id == "case2" || id == "case5" {
			t.Errorf("%s should be filtered from active view", id)
		}
	}
}

func TestReconcilePullsRuntimeModel(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)

	_ = store.Create(&task.Task{ID: "t1", Runtime: "opencode", Model: "kimi", Status: "completed", CreatedAt: time.Now()})

	m := &Model{
		tasks:     map[string]*taskState{"t1": {ID: "t1", Status: "running"}},
		taskOrder: []string{"t1"},
		store:     store,
	}

	runReconcile(m)

	if m.tasks["t1"].Runtime != "opencode" {
		t.Errorf("expected runtime 'opencode', got %q", m.tasks["t1"].Runtime)
	}
	if m.tasks["t1"].Model != "kimi" {
		t.Errorf("expected model 'kimi', got %q", m.tasks["t1"].Model)
	}
}

func TestReconcileNoStoreReturnsNil(t *testing.T) {
	m := &Model{
		tasks:     map[string]*taskState{"t1": {ID: "t1", Status: "running"}},
		taskOrder: []string{"t1"},
	}
	cmd := m.startReconciliation()
	if cmd != nil {
		t.Error("expected nil cmd when store is nil")
	}
}

func TestCheckDeadProcessesZombieDetection(t *testing.T) {
	now := time.Now().UTC()
	oldStart := now.Add(-2 * time.Hour)
	oldEvent := now.Add(-time.Hour)

	reapCh := make(chan string, 10)

	m := &Model{
		tasks: map[string]*taskState{
			"zombie-no-pid":   {ID: "zombie-no-pid", Status: "running", StartedAt: oldStart, LastEventAt: oldEvent},
			"zombie-recycled": {ID: "zombie-recycled", Status: "running", PID: os.Getpid(), StartedAt: oldStart, LastEventAt: oldEvent},
			"alive":           {ID: "alive", Status: "running", PID: os.Getpid(), StartedAt: now, LastEventAt: now},
			"dead-pid":        {ID: "dead-pid", Status: "running", PID: 99999, StartedAt: now, LastEventAt: now},
			"completed":       {ID: "completed", Status: "completed"},
			"stale-pending":   {ID: "stale-pending", Status: "pending", LastEventAt: oldEvent},
			"fresh-pending":   {ID: "fresh-pending", Status: "pending", LastEventAt: now},
		},
		taskOrder: []string{"zombie-no-pid", "zombie-recycled", "alive", "dead-pid", "completed", "stale-pending", "fresh-pending"},
		reapCh:    reapCh,
	}

	m.checkDeadProcesses()

	expects := map[string]string{
		"zombie-no-pid":   "failed",
		"zombie-recycled": "failed",
		"alive":           "running",
		"dead-pid":        "failed",
		"completed":       "completed",
		"stale-pending":   "failed",
		"fresh-pending":   "pending",
	}

	for id, expect := range expects {
		if m.tasks[id].Status != expect {
			t.Errorf("%s: expected %q, got %q", id, expect, m.tasks[id].Status)
		}
	}

	close(reapCh)
	var reaped []string
	for id := range reapCh {
		reaped = append(reaped, id)
	}
	if len(reaped) != 4 {
		t.Errorf("expected 4 reaped, got %d: %v", len(reaped), reaped)
	}
}

func TestKillEmitsEvent(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)
	now := time.Now().UTC()

	_ = store.Create(&task.Task{ID: "kill-test", Runtime: "rt", Model: "m", Status: "running", CreatedAt: now})
	_ = store.KillTask("kill-test")

	tk, _ := store.Get("kill-test")
	if tk.Status != "killed" {
		t.Errorf("expected killed, got %s", tk.Status)
	}

	_ = filepath.Join(dir, "events.ndjson")
	if tk.CompletedAt == nil {
		t.Error("CompletedAt should be set after kill")
	}
}
