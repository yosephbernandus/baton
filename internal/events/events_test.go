package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitter_Emit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	emitter, err := NewEmitter(logPath)
	if err != nil {
		t.Fatal(err)
	}

	err = emitter.Emit(Event{
		TaskID:    "task-001",
		Runtime:   "opencode",
		Model:     "kimi",
		EventType: "task_created",
		Data:      map[string]interface{}{"summary": "test task"},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}

	var ev Event
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatal(err)
	}

	if ev.TaskID != "task-001" {
		t.Errorf("expected task-001, got %s", ev.TaskID)
	}
	if ev.EventType != "task_created" {
		t.Errorf("expected task_created, got %s", ev.EventType)
	}
	if ev.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestEmitter_MultipleEvents(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")
	emitter, _ := NewEmitter(logPath)

	for i := 0; i < 5; i++ {
		emitter.Emit(Event{
			TaskID:    "task-001",
			EventType: "output",
			Data:      map[string]interface{}{"line": "test"},
		})
	}

	data, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestTaskEvent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")
	emitter, _ := NewEmitter(logPath)

	err := emitter.TaskEvent("t1", "opencode", "kimi", "claude-code/sonnet", "task_started", map[string]interface{}{"attempt": 1})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(logPath)
	var ev Event
	json.Unmarshal([]byte(strings.TrimSpace(string(data))), &ev)

	if ev.DispatchedBy != "claude-code/sonnet" {
		t.Errorf("expected dispatched_by claude-code/sonnet, got %s", ev.DispatchedBy)
	}
}
