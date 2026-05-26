package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
	"gopkg.in/yaml.v3"
)

func TestE2EAsyncReconcileFullScenario(t *testing.T) {
	dir := t.TempDir()
	store, err := task.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	twoHoursAgo := now.Add(-2 * time.Hour)
	yesterday := now.Add(-24 * time.Hour)

	// --- Scenario 1: completed in YAML, events only show started ---
	_ = store.Create(&task.Task{
		ID: "s1-completed", Runtime: "claude-code", Model: "opus",
		Status: "completed", Duration: "5m30s", CreatedAt: yesterday,
	})

	// --- Scenario 2: killed in YAML, events only show created ---
	_ = store.Create(&task.Task{
		ID: "s2-killed", Runtime: "opencode", Model: "kimi",
		Status: "killed", CreatedAt: yesterday,
	})

	// --- Scenario 3: running in YAML, PID 99999 (dead) ---
	_ = store.Create(&task.Task{
		ID: "s3-dead-pid", Runtime: "claude-code", Model: "sonnet",
		Status: "running", PID: 99999, CreatedAt: now,
	})

	// --- Scenario 4: running, no PID, started yesterday, last event 2h ago (zombie) ---
	_ = store.Create(&task.Task{
		ID: "s4-zombie", Runtime: "opencode", Model: "kimi",
		Status: "running", CreatedAt: yesterday,
	})

	// --- Scenario 5: genuinely running, alive PID, recent events ---
	_ = store.Create(&task.Task{
		ID: "s5-alive", Runtime: "claude-code", Model: "opus",
		Status: "running", PID: os.Getpid(), CreatedAt: now,
	})

	// --- Scenario 6: running in YAML, PID alive but recycled (started yesterday, no recent events) ---
	_ = store.Create(&task.Task{
		ID: "s6-recycled", Runtime: "claude-code", Model: "opus",
		Status: "running", PID: os.Getpid(), CreatedAt: yesterday,
	})

	// --- Scenario 7: worker-protocol task (no YAML task file, only worker-state.yaml) ---
	workerDir := filepath.Join(dir, "s7-worker")
	_ = os.MkdirAll(workerDir, 0755)
	wsData, _ := yaml.Marshal(&workerState{
		State:     "executing",
		PhaseName: "implement",
		Role:      "developer",
		WorkerPID: 99998,
	})
	_ = os.WriteFile(filepath.Join(workerDir, "worker-state.yaml"), wsData, 0644)

	// --- Scenario 8: worker-protocol task that finished (state=done, dead PID) ---
	workerDir8 := filepath.Join(dir, "s8-worker-done")
	_ = os.MkdirAll(workerDir8, 0755)
	wsData8, _ := yaml.Marshal(&workerState{
		State:     "done",
		PhaseName: "verify",
		Role:      "tester",
		WorkerPID: 99997,
	})
	_ = os.WriteFile(filepath.Join(workerDir8, "worker-state.yaml"), wsData8, 0644)

	// --- Scenario 9: pending for hours, never started (stale pending) ---
	_ = store.Create(&task.Task{
		ID: "s9-stale-pending", Runtime: "opencode", Model: "kimi",
		Status: "pending", CreatedAt: yesterday,
	})

	// --- Scenario 10: pending but recent (should stay pending) ---
	_ = store.Create(&task.Task{
		ID: "s10-fresh-pending", Runtime: "opencode", Model: "kimi",
		Status: "pending", CreatedAt: now,
	})

	// --- Build events ---
	evts := []events.Event{
		{Timestamp: yesterday, TaskID: "s1-completed", Runtime: "claude-code", Model: "opus", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "s1-completed", Runtime: "claude-code", Model: "opus", EventType: "task_started"},

		{Timestamp: yesterday, TaskID: "s2-killed", Runtime: "opencode", Model: "kimi", EventType: "task_created"},

		{Timestamp: now, TaskID: "s3-dead-pid", Runtime: "claude-code", Model: "sonnet", EventType: "task_created"},
		{Timestamp: now.Add(time.Second), TaskID: "s3-dead-pid", Runtime: "claude-code", Model: "sonnet", EventType: "task_started"},
		{Timestamp: now.Add(2 * time.Second), TaskID: "s3-dead-pid", EventType: "worker_pid", Data: map[string]interface{}{"pid": float64(99999)}},

		{Timestamp: yesterday, TaskID: "s4-zombie", Runtime: "opencode", Model: "kimi", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "s4-zombie", Runtime: "opencode", Model: "kimi", EventType: "task_started"},
		{Timestamp: twoHoursAgo, TaskID: "s4-zombie", EventType: "worker_progress", Data: map[string]interface{}{"msg": "last seen"}},

		{Timestamp: now, TaskID: "s5-alive", Runtime: "claude-code", Model: "opus", EventType: "task_created"},
		{Timestamp: now.Add(time.Second), TaskID: "s5-alive", Runtime: "claude-code", Model: "opus", EventType: "task_started"},
		{Timestamp: now.Add(2 * time.Second), TaskID: "s5-alive", EventType: "worker_pid", Data: map[string]interface{}{"pid": float64(os.Getpid())}},
		{Timestamp: now.Add(3 * time.Second), TaskID: "s5-alive", EventType: "worker_progress", Data: map[string]interface{}{"msg": "actively working"}},

		{Timestamp: yesterday, TaskID: "s6-recycled", Runtime: "claude-code", Model: "opus", EventType: "task_created"},
		{Timestamp: yesterday.Add(time.Second), TaskID: "s6-recycled", Runtime: "claude-code", Model: "opus", EventType: "task_started"},
		{Timestamp: twoHoursAgo, TaskID: "s6-recycled", EventType: "worker_progress", Data: map[string]interface{}{"msg": "old activity"}},

		{Timestamp: now, TaskID: "s7-worker", Runtime: "developer", EventType: "task_created"},
		{Timestamp: now.Add(time.Second), TaskID: "s7-worker", EventType: "worker_started", Data: map[string]interface{}{"pid": float64(99998), "role": "developer"}},

		{Timestamp: now, TaskID: "s8-worker-done", Runtime: "tester", EventType: "task_created"},
		{Timestamp: now.Add(time.Second), TaskID: "s8-worker-done", EventType: "worker_started", Data: map[string]interface{}{"pid": float64(99997), "role": "tester"}},

		{Timestamp: yesterday, TaskID: "s9-stale-pending", Runtime: "opencode", Model: "kimi", EventType: "task_created"},

		{Timestamp: now, TaskID: "s10-fresh-pending", Runtime: "opencode", Model: "kimi", EventType: "task_created"},
	}

	reapCh := make(chan string, 20)
	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
		store:      store,
		taskDir:    dir,
		reapCh:     reapCh,
	}

	// Step 1: Process events
	for _, ev := range evts {
		m.processEvent(ev)
	}

	// Verify pre-reconcile: events-only state
	preExpect := map[string]string{
		"s1-completed":    "running",
		"s2-killed":       "pending",
		"s3-dead-pid":     "running",
		"s4-zombie":       "running",
		"s5-alive":        "running",
		"s6-recycled":     "running",
		"s7-worker":       "running",
		"s8-worker-done":  "running",
		"s9-stale-pending":  "pending",
		"s10-fresh-pending": "pending",
	}
	for id, expect := range preExpect {
		if m.tasks[id] == nil {
			t.Fatalf("[pre-reconcile] task %s not found", id)
		}
		if m.tasks[id].Status != expect {
			t.Errorf("[pre-reconcile] %s: want %q, got %q", id, expect, m.tasks[id].Status)
		}
	}
	t.Log("Pre-reconcile states verified")

	// Step 2: Run async reconciliation
	runReconcile(m)

	postReconcile := map[string]string{
		"s1-completed":     "completed", // YAML says completed
		"s2-killed":        "killed",    // YAML says killed
		"s3-dead-pid":      "failed",    // dead PID → failed
		"s4-zombie":        "running",   // no PID in YAML reconcile, zombie detected later
		"s5-alive":         "running",   // alive PID, recent
		"s6-recycled":      "running",   // PID alive (recycled), zombie detected later
		"s7-worker":        "failed",    // worker-state.yaml: executing + dead PID → failed
		"s8-worker-done":   "completed", // worker-state.yaml: done + dead PID → completed
		"s9-stale-pending":   "pending", // YAML=pending, not terminal → no change from reconcile
		"s10-fresh-pending":  "pending", // YAML=pending, recent → no change
	}
	for id, expect := range postReconcile {
		if m.tasks[id].Status != expect {
			t.Errorf("[post-reconcile] %s: want %q, got %q", id, expect, m.tasks[id].Status)
		}
	}

	// Verify duration and metadata pulled correctly
	if m.tasks["s1-completed"].Duration != "5m30s" {
		t.Errorf("s1-completed duration: want '5m30s', got %q", m.tasks["s1-completed"].Duration)
	}
	if m.tasks["s7-worker"].Progress != "implement" {
		t.Errorf("s7-worker progress: want 'implement', got %q", m.tasks["s7-worker"].Progress)
	}
	if m.tasks["s8-worker-done"].Progress != "verify" {
		t.Errorf("s8-worker-done progress: want 'verify', got %q", m.tasks["s8-worker-done"].Progress)
	}
	t.Log("Post-reconcile states verified")

	// Step 3: Run checkDeadProcesses for zombie detection
	m.checkDeadProcesses()

	postCheck := map[string]string{
		"s1-completed":     "completed",
		"s2-killed":        "killed",
		"s3-dead-pid":      "failed",
		"s4-zombie":        "failed",   // zombie: started yesterday, last event 2h ago
		"s5-alive":         "running",  // alive PID, recent events
		"s6-recycled":      "failed",   // zombie: alive PID but started yesterday, last event 2h ago
		"s7-worker":        "failed",
		"s8-worker-done":   "completed",
		"s9-stale-pending":   "failed", // stale pending: created yesterday, no events for 24h
		"s10-fresh-pending":  "pending", // fresh pending: created recently → stays pending
	}
	for id, expect := range postCheck {
		if m.tasks[id].Status != expect {
			t.Errorf("[post-checkDead] %s: want %q, got %q", id, expect, m.tasks[id].Status)
		}
	}
	t.Log("Post-checkDeadProcesses states verified")

	// Step 4: Verify active view filtering
	m.showAll = false
	active := m.visibleTasks()
	activeMap := make(map[string]bool)
	for _, id := range active {
		activeMap[id] = true
	}
	if len(active) != 2 || !activeMap["s5-alive"] || !activeMap["s10-fresh-pending"] {
		t.Errorf("Active view: want [s5-alive, s10-fresh-pending], got %v", active)
	}

	m.showAll = true
	all := m.visibleTasks()
	if len(all) != 10 {
		t.Errorf("All view: want 10, got %d", len(all))
	}
	t.Log("View filtering verified")

	// Step 5: Verify reap channel
	close(reapCh)
	reaped := make(map[string]bool)
	for id := range reapCh {
		reaped[id] = true
	}

	shouldReap := []string{"s3-dead-pid", "s4-zombie", "s6-recycled", "s7-worker", "s9-stale-pending"}
	for _, id := range shouldReap {
		if !reaped[id] {
			t.Errorf("%s should be in reap channel", id)
		}
	}
	shouldNotReap := []string{"s1-completed", "s2-killed", "s5-alive", "s8-worker-done", "s10-fresh-pending"}
	for _, id := range shouldNotReap {
		if reaped[id] {
			t.Errorf("%s should NOT be in reap channel", id)
		}
	}
	t.Logf("Reap channel verified: %d reaped", len(reaped))

	// Step 6: Verify per-task reconcile flag prevents re-check
	_ = store.Create(&task.Task{
		ID: "s5-alive", Runtime: "claude-code", Model: "opus",
		Status: "killed", CreatedAt: now,
	})
	m.tasks["s5-alive"].Status = "running" // restore running
	runReconcile(m)
	if m.tasks["s5-alive"].Status != "running" {
		t.Error("per-task reconciled flag should prevent re-reconciliation")
	}
	t.Log("Per-task reconcile flag verified")

	// Step 7: New task after reconcile gets checked
	_ = store.Create(&task.Task{
		ID: "s9-late", Runtime: "test-rt", Model: "test-m",
		Status: "completed", Duration: "1m", CreatedAt: now,
	})
	m.tasks["s9-late"] = &taskState{ID: "s9-late", Status: "running"}
	m.taskOrder = append(m.taskOrder, "s9-late")
	runReconcile(m)
	if m.tasks["s9-late"].Status != "completed" {
		t.Errorf("s9-late: late addition should reconcile, got %q", m.tasks["s9-late"].Status)
	}
	t.Log("Late task reconciliation verified")

	// Step 8: Verify event already set terminal status is not downgraded
	m.tasks["s9-late"].reconciled = false
	m.tasks["s9-late"].Status = "completed"
	_ = store.Create(&task.Task{
		ID: "s9-late", Runtime: "test-rt", Model: "test-m",
		Status: "running", CreatedAt: now,
	})
	runReconcile(m)
	if m.tasks["s9-late"].Status != "completed" {
		t.Errorf("s9-late: terminal status should not be downgraded, got %q", m.tasks["s9-late"].Status)
	}
	t.Log("Terminal status protection verified")

	fmt.Println("All E2E scenarios passed")
}

func TestE2ETimerDoesNotFreeze(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)

	now := time.Now().UTC()

	// Create many tasks to stress-test reconciliation timing
	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
		store:      store,
		taskDir:    dir,
	}

	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("task-%03d", i)
		_ = store.Create(&task.Task{
			ID: id, Runtime: "rt", Model: "m",
			Status: "completed", Duration: "1m", CreatedAt: now,
		})
		m.tasks[id] = &taskState{ID: id, Status: "running"}
		m.taskOrder = append(m.taskOrder, id)
	}

	// Async reconciliation should return a Cmd, not block
	start := time.Now()
	cmd := m.startReconciliation()
	setupDuration := time.Since(start)

	if cmd == nil {
		t.Fatal("startReconciliation returned nil with 100 unreconciled tasks")
	}

	// Setup (collecting batch) should be fast — no I/O
	if setupDuration > 10*time.Millisecond {
		t.Errorf("startReconciliation setup took %v (should be <10ms, no I/O)", setupDuration)
	}

	// Execute cmd (this does I/O but runs outside the Update loop)
	start = time.Now()
	msg := cmd()
	ioDuration := time.Since(start)
	t.Logf("I/O for 100 tasks took %v", ioDuration)

	results, ok := msg.(reconcileMsg)
	if !ok {
		t.Fatal("cmd did not return reconcileMsg")
	}

	if len(results) != 100 {
		t.Errorf("want 100 results, got %d", len(results))
	}

	// Apply results
	m.applyReconcileResults(results)

	completed := 0
	for _, ts := range m.tasks {
		if ts.Status == "completed" {
			completed++
		}
	}
	if completed != 100 {
		t.Errorf("want 100 completed, got %d", completed)
	}
	t.Log("100-task async reconcile verified")
}

func TestE2EKillFlowEmitsEventAndReconciles(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)
	now := time.Now().UTC()

	// Use PID 0 to avoid KillTask sending SIGKILL to our own process
	_ = store.Create(&task.Task{
		ID: "kill-e2e", Runtime: "rt", Model: "m",
		Status: "running", CreatedAt: now,
	})

	m := &Model{
		tasks:     map[string]*taskState{"kill-e2e": {ID: "kill-e2e", Status: "running"}},
		taskOrder: []string{"kill-e2e"},
		store:     store,
	}

	// Simulate kill: update store directly (KillTask would also signal the process)
	tk, _ := store.Get("kill-e2e")
	tk.Status = "killed"
	completedAt := now
	tk.CompletedAt = &completedAt
	_ = store.Update(tk)

	// Verify store updated
	tk2, _ := store.Get("kill-e2e")
	if tk2.Status != "killed" {
		t.Fatalf("store kill failed: %s", tk2.Status)
	}
	if tk2.CompletedAt == nil {
		t.Fatal("CompletedAt not set after kill")
	}

	// Process kill event (what TUI would receive from emitter)
	m.processEvent(events.Event{
		Timestamp: now, TaskID: "kill-e2e",
		EventType: "task_killed",
	})
	if m.tasks["kill-e2e"].Status != "killed" {
		t.Errorf("TUI should show killed after event, got %q", m.tasks["kill-e2e"].Status)
	}

	// Even without event, reconciliation catches it
	m.tasks["kill-e2e"].Status = "running"
	m.tasks["kill-e2e"].reconciled = false
	runReconcile(m)
	if m.tasks["kill-e2e"].Status != "killed" {
		t.Errorf("Reconcile should catch killed status, got %q", m.tasks["kill-e2e"].Status)
	}

	t.Log("Kill flow E2E verified")
}

func TestE2EPipelinePhaseEvents(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)
	now := time.Now().UTC()

	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
		store:      store,
		taskDir:    dir,
	}

	taskID := "spec-phase-8"

	// Phase started → running
	m.processEvent(events.Event{
		Timestamp: now, TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_started",
		Data: map[string]interface{}{"phase_name": "implementation", "role": "developer"},
	})
	if m.tasks[taskID].Status != "running" {
		t.Errorf("phase_started: want running, got %q", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].Progress != "implementation" {
		t.Errorf("phase_started: want progress 'implementation', got %q", m.tasks[taskID].Progress)
	}
	if m.tasks[taskID].Runtime != "developer" {
		t.Errorf("phase_started: want runtime 'developer', got %q", m.tasks[taskID].Runtime)
	}

	// Phase completed → completed
	m.processEvent(events.Event{
		Timestamp: now.Add(time.Minute), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_completed",
		Data: map[string]interface{}{"phase_name": "implementation"},
	})
	if m.tasks[taskID].Status != "completed" {
		t.Errorf("phase_completed: want completed, got %q", m.tasks[taskID].Status)
	}

	// Terminal status → reconciliation skips (no need to check store)
	runReconcile(m)

	// --- RERUN: phase_started resets reconciled + status ---
	m.processEvent(events.Event{
		Timestamp: now.Add(2 * time.Minute), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_started",
		Data: map[string]interface{}{"phase_name": "implementation", "role": "developer"},
	})
	if m.tasks[taskID].Status != "running" {
		t.Errorf("rerun phase_started: want running, got %q", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].reconciled {
		t.Error("phase_started should reset reconciled flag")
	}

	// Phase failed
	m.processEvent(events.Event{
		Timestamp: now.Add(3 * time.Minute), TaskID: taskID,
		EventType: "phase_failed",
	})
	if m.tasks[taskID].Status != "failed" {
		t.Errorf("phase_failed: want failed, got %q", m.tasks[taskID].Status)
	}

	// Phase retry resets to running
	m.processEvent(events.Event{
		Timestamp: now.Add(4 * time.Minute), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_retry",
	})
	if m.tasks[taskID].Status != "running" {
		t.Errorf("phase_retry: want running, got %q", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].reconciled {
		t.Error("phase_retry should reset reconciled flag")
	}

	t.Log("Pipeline phase events verified")
}

func TestE2EPipelineRerunReconcile(t *testing.T) {
	dir := t.TempDir()
	store, _ := task.NewStore(dir)
	now := time.Now().UTC()

	taskID := "myspec-phase-8"

	// First run: task completed in store
	_ = store.Create(&task.Task{
		ID: taskID, Runtime: "claude-code", Model: "opus",
		Status: "completed", Duration: "5m", CreatedAt: now,
	})

	m := &Model{
		tasks:      make(map[string]*taskState),
		splitRatio: 0.4,
		store:      store,
		taskDir:    dir,
	}

	// Simulate event replay: phase completed from first run
	m.processEvent(events.Event{
		Timestamp: now, TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_completed",
		Data: map[string]interface{}{"phase_name": "implementation"},
	})

	// Reconcile picks up completed from store
	runReconcile(m)
	if m.tasks[taskID].Status != "completed" {
		t.Fatalf("first run: want completed, got %q", m.tasks[taskID].Status)
	}

	// --- Pipeline reruns the same phase ---
	// Store updates to running
	tk, _ := store.Get(taskID)
	tk.Status = "running"
	tk.CompletedAt = nil
	_ = store.Update(tk)

	// New phase_started event arrives
	m.processEvent(events.Event{
		Timestamp: now.Add(10 * time.Minute), TaskID: taskID, Runtime: "claude-code", Model: "opus",
		EventType: "phase_started",
		Data: map[string]interface{}{"phase_name": "implementation", "role": "developer"},
	})

	if m.tasks[taskID].Status != "running" {
		t.Errorf("rerun: want running, got %q", m.tasks[taskID].Status)
	}
	if m.tasks[taskID].reconciled {
		t.Error("phase_started should have reset reconciled flag")
	}

	// Reconcile should now see running in store (not completed)
	runReconcile(m)
	if m.tasks[taskID].Status != "running" {
		t.Errorf("rerun reconcile: want running, got %q", m.tasks[taskID].Status)
	}

	t.Log("Pipeline rerun reconcile verified")
}
