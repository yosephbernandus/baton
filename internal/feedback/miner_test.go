package feedback

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEvents(t *testing.T, dir string, events []rawEvent) string {
	t.Helper()
	path := filepath.Join(dir, "events.ndjson")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, ev := range events {
		data, _ := json.Marshal(ev)
		_, _ = f.Write(data)
		_, _ = f.WriteString("\n")
	}
	return path
}

func now() time.Time { return time.Now().UTC() }

func TestAnalyzeEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.ndjson")
	_ = os.WriteFile(path, nil, 0o644)

	m := NewMiner(path, 24*time.Hour, 3)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if a.EventWindow.TotalEvents != 0 {
		t.Errorf("expected 0 events, got %d", a.EventWindow.TotalEvents)
	}
}

func TestAnalyzeNoFile(t *testing.T) {
	m := NewMiner("/nonexistent/events.ndjson", 24*time.Hour, 3)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if a.EventWindow.TotalEvents != 0 {
		t.Errorf("expected 0 events")
	}
}

func TestAnalyzeRuntimeMetrics(t *testing.T) {
	dir := t.TempDir()
	ts := now()
	path := writeEvents(t, dir, []rawEvent{
		{Timestamp: ts, TaskID: "t1", Runtime: "opencode", Model: "kimi", EventType: "task_completed"},
		{Timestamp: ts, TaskID: "t2", Runtime: "opencode", Model: "kimi", EventType: "task_completed"},
		{Timestamp: ts, TaskID: "t3", Runtime: "opencode", Model: "kimi", EventType: "task_failed"},
		{Timestamp: ts, TaskID: "t4", Runtime: "claude", Model: "sonnet", EventType: "task_completed"},
	})

	m := NewMiner(path, 24*time.Hour, 3)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}

	if a.EventWindow.TotalTasks != 4 {
		t.Errorf("tasks=%d", a.EventWindow.TotalTasks)
	}

	kimi, ok := a.RuntimePerformance["opencode/kimi"]
	if !ok || kimi == nil {
		t.Fatal("missing opencode/kimi metrics")
		return
	}
	if kimi.Tasks != 3 {
		t.Errorf("kimi tasks=%d", kimi.Tasks)
	}
	if kimi.Successes != 2 {
		t.Errorf("kimi successes=%d", kimi.Successes)
	}
	if kimi.SuccessRate < 0.66 || kimi.SuccessRate > 0.67 {
		t.Errorf("kimi success_rate=%f", kimi.SuccessRate)
	}

	sonnet, ok := a.RuntimePerformance["claude/sonnet"]
	if !ok || sonnet == nil {
		t.Fatal("missing claude/sonnet metrics")
		return
	}
	if sonnet.SuccessRate != 1.0 {
		t.Errorf("sonnet success_rate=%f", sonnet.SuccessRate)
	}
}

func TestAnalyzeDomainMetrics(t *testing.T) {
	dir := t.TempDir()
	ts := now()
	path := writeEvents(t, dir, []rawEvent{
		{Timestamp: ts, TaskID: "t1", Runtime: "rt", Model: "m", EventType: "task_completed", Data: map[string]interface{}{"domain": "backend"}},
		{Timestamp: ts, TaskID: "t2", Runtime: "rt", Model: "m", EventType: "task_failed", Data: map[string]interface{}{"domain": "frontend"}},
		{Timestamp: ts, TaskID: "t3", Runtime: "rt", Model: "m", EventType: "task_completed", Data: map[string]interface{}{"domain": "backend"}},
	})

	m := NewMiner(path, 24*time.Hour, 1)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}

	rm, ok := a.RuntimePerformance["rt/m"]
	if !ok || rm == nil {
		t.Fatal("missing rt/m")
		return
	}
	backend := rm.ByDomain["backend"]
	if backend == nil || backend.Tasks != 2 || backend.Successes != 2 {
		t.Errorf("backend=%+v", backend)
	}
	frontend := rm.ByDomain["frontend"]
	if frontend == nil || frontend.Tasks != 1 || frontend.Successes != 0 {
		t.Errorf("frontend=%+v", frontend)
	}
}

func TestAnalyzePhaseMetrics(t *testing.T) {
	dir := t.TempDir()
	ts := now()
	path := writeEvents(t, dir, []rawEvent{
		{Timestamp: ts, TaskID: "t1", EventType: "phase_started", Data: map[string]interface{}{"phase_name": "implementation", "complexity": "LARGE"}},
		{Timestamp: ts, TaskID: "t1", EventType: "phase_retry", Runtime: "rt", Model: "m", Data: map[string]interface{}{"phase_name": "implementation", "complexity": "LARGE"}},
		{Timestamp: ts, TaskID: "t1", EventType: "phase_retry", Runtime: "rt", Model: "m", Data: map[string]interface{}{"phase_name": "implementation", "complexity": "LARGE"}},
		{Timestamp: ts, TaskID: "t2", EventType: "phase_started", Data: map[string]interface{}{"phase_name": "implementation", "complexity": "SMALL"}},
		{Timestamp: ts, TaskID: "t3", EventType: "phase_stuck", Data: map[string]interface{}{"phase_name": "implementation"}},
	})

	m := NewMiner(path, 24*time.Hour, 1)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}

	pm, ok := a.PhaseMetrics["implementation"]
	if !ok || pm == nil {
		t.Fatal("missing implementation phase metrics")
		return
	}
	if pm.TotalRuns != 2 {
		t.Errorf("runs=%d", pm.TotalRuns)
	}
	if pm.TotalRetries != 2 {
		t.Errorf("retries=%d", pm.TotalRetries)
	}
	if pm.LoopDetections != 1 {
		t.Errorf("loops=%d", pm.LoopDetections)
	}

	large := pm.ByComplexity["LARGE"]
	if large == nil || large.Runs != 1 || large.Retries != 2 {
		t.Errorf("LARGE=%+v", large)
	}
}

func TestDetectRuntimeDomainMismatch(t *testing.T) {
	dir := t.TempDir()
	ts := now()
	var events []rawEvent
	for i := 0; i < 4; i++ {
		events = append(events, rawEvent{
			Timestamp: ts, TaskID: "t" + string(rune('a'+i)), Runtime: "rt", Model: "m",
			EventType: "task_failed", Data: map[string]interface{}{"domain": "frontend"},
		})
	}
	events = append(events, rawEvent{
		Timestamp: ts, TaskID: "tx", Runtime: "rt", Model: "m",
		EventType: "task_completed", Data: map[string]interface{}{"domain": "frontend"},
	})
	path := writeEvents(t, dir, events)

	m := NewMiner(path, 24*time.Hour, 3)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, p := range a.Patterns {
		if p.Type == "runtime_domain_mismatch" {
			found = true
			if p.Occurrences != 4 {
				t.Errorf("occurrences=%d", p.Occurrences)
			}
		}
	}
	if !found {
		t.Error("expected runtime_domain_mismatch pattern")
	}
}

func TestDetectRetryBudgetInsufficient(t *testing.T) {
	dir := t.TempDir()
	ts := now()
	var events []rawEvent
	for i := 0; i < 4; i++ {
		events = append(events, rawEvent{
			Timestamp: ts, TaskID: "t" + string(rune('a'+i)),
			EventType: "phase_started", Data: map[string]interface{}{"phase_name": "implementation", "complexity": "LARGE"},
		})
	}
	for i := 0; i < 2; i++ {
		events = append(events, rawEvent{
			Timestamp: ts, TaskID: "t" + string(rune('a'+i)),
			EventType: "phase_failed", Data: map[string]interface{}{"phase_name": "implementation", "complexity": "LARGE"},
		})
	}
	path := writeEvents(t, dir, events)

	m := NewMiner(path, 24*time.Hour, 3)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, p := range a.Patterns {
		if p.Type == "retry_budget_insufficient" {
			found = true
		}
	}
	if !found {
		t.Error("expected retry_budget_insufficient pattern")
	}
}

func TestWindowFiltering(t *testing.T) {
	dir := t.TempDir()
	old := now().Add(-48 * time.Hour)
	recent := now()
	path := writeEvents(t, dir, []rawEvent{
		{Timestamp: old, TaskID: "old", Runtime: "rt", Model: "m", EventType: "task_completed"},
		{Timestamp: recent, TaskID: "new", Runtime: "rt", Model: "m", EventType: "task_completed"},
	})

	m := NewMiner(path, 24*time.Hour, 1)
	a, err := m.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if a.EventWindow.TotalEvents != 1 {
		t.Errorf("expected 1 event (filtered by window), got %d", a.EventWindow.TotalEvents)
	}
}

func TestDefaultMinerValues(t *testing.T) {
	m := NewMiner("test", 0, 0)
	if m.Window != 7*24*time.Hour {
		t.Errorf("default window=%v", m.Window)
	}
	if m.MinOccurrences != 3 {
		t.Errorf("default min=%d", m.MinOccurrences)
	}
}
