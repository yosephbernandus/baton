package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchdogMarksCrashedSession(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessDir, 0o755)

	m := New("test-session", "spec.yaml", "MEDIUM")
	m.LastCommandAt = time.Now().Add(-20 * time.Minute)
	m.CoordinatorPID = 99999
	sessPath := filepath.Join(sessDir, "test.yaml")
	_ = m.Save(sessPath)

	eventsPath := filepath.Join(dir, "events.ndjson")
	_ = os.WriteFile(eventsPath, []byte{}, 0o644)

	w := NewWatchdog(eventsPath, sessDir, 5*time.Minute, nil)
	w.check()

	loaded, err := Load(sessPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "crashed" {
		t.Errorf("want crashed, got %q", loaded.Status)
	}
}

func TestWatchdogSkipsActiveSession(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessDir, 0o755)

	m := New("active-session", "spec.yaml", "MEDIUM")
	m.LastCommandAt = time.Now()
	sessPath := filepath.Join(sessDir, "active.yaml")
	_ = m.Save(sessPath)

	eventsPath := filepath.Join(dir, "events.ndjson")
	_ = os.WriteFile(eventsPath, []byte{}, 0o644)

	w := NewWatchdog(eventsPath, sessDir, 5*time.Minute, nil)
	w.check()

	loaded, _ := Load(sessPath)
	if loaded.Status != "running" {
		t.Errorf("active session should stay running, got %q", loaded.Status)
	}
}

func TestWatchdogSkipsCompletedSession(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessDir, 0o755)

	m := New("done-session", "spec.yaml", "MEDIUM")
	m.MarkCompleted()
	m.LastCommandAt = time.Now().Add(-20 * time.Minute)
	sessPath := filepath.Join(sessDir, "done.yaml")
	_ = m.Save(sessPath)

	eventsPath := filepath.Join(dir, "events.ndjson")
	_ = os.WriteFile(eventsPath, []byte{}, 0o644)

	w := NewWatchdog(eventsPath, sessDir, 5*time.Minute, nil)
	w.check()

	loaded, _ := Load(sessPath)
	if loaded.Status != "completed" {
		t.Errorf("completed session should stay completed, got %q", loaded.Status)
	}
}

func TestWatchdogUsesEventTimestamp(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessDir, 0o755)

	m := New("evt-session", "spec.yaml", "MEDIUM")
	m.LastCommandAt = time.Now().Add(-20 * time.Minute)
	m.CoordinatorPID = 99999
	sessPath := filepath.Join(sessDir, "evt.yaml")
	_ = m.Save(sessPath)

	eventsPath := filepath.Join(dir, "events.ndjson")
	f, _ := os.Create(eventsPath)
	ev := map[string]interface{}{
		"ts":      time.Now().Format(time.RFC3339Nano),
		"task_id": "some-task",
		"data":    map[string]interface{}{"session_id": "evt-session"},
	}
	line, _ := json.Marshal(ev)
	_, _ = f.Write(append(line, '\n'))
	f.Close()

	w := NewWatchdog(eventsPath, sessDir, 5*time.Minute, nil)
	w.check()

	loaded, _ := Load(sessPath)
	if loaded.Status != "running" {
		t.Errorf("session with recent event should stay running, got %q", loaded.Status)
	}
}
