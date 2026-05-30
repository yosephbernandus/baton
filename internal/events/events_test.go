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
		_ = emitter.Emit(Event{
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
	_ = json.Unmarshal([]byte(strings.TrimSpace(string(data))), &ev)

	if ev.DispatchedBy != "claude-code/sonnet" {
		t.Errorf("expected dispatched_by claude-code/sonnet, got %s", ev.DispatchedBy)
	}
}

func TestReadTaskOutput(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")
	emitter, _ := NewEmitter(logPath)

	_ = emitter.Emit(Event{TaskID: "t1", EventType: "output", Data: map[string]interface{}{"line": "hello"}})
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "output", Data: map[string]interface{}{"line": "world"}})
	_ = emitter.Emit(Event{TaskID: "t2", EventType: "output", Data: map[string]interface{}{"line": "other"}})
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "task_created", Data: map[string]interface{}{"summary": "ignored"}})

	lines, err := ReadTaskOutput(logPath, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 output lines for t1, got %d", len(lines))
	}
	if lines[0] != "hello" || lines[1] != "world" {
		t.Errorf("unexpected output: %v", lines)
	}
}

func TestReadTaskOutputNoMatch(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")
	emitter, _ := NewEmitter(logPath)

	_ = emitter.Emit(Event{TaskID: "t1", EventType: "output", Data: map[string]interface{}{"line": "hello"}})

	lines, err := ReadTaskOutput(logPath, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestReadTaskOutputMissingFile(t *testing.T) {
	lines, err := ReadTaskOutput("/nonexistent/events.ndjson", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for missing file, got %d", len(lines))
	}
}

func TestReadTaskOutputRotatedLogs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	// Write to rotated file (.1)
	ev1 := Event{TaskID: "t1", EventType: "output", Data: map[string]interface{}{"line": "from-rotated"}}
	line1, _ := json.Marshal(ev1)
	if err := os.WriteFile(logPath+".1", append(line1, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write to current file
	emitter, _ := NewEmitter(logPath)
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "output", Data: map[string]interface{}{"line": "from-current"}})

	lines, err := ReadTaskOutput(logPath, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines across rotated+current, got %d", len(lines))
	}
	if lines[0] != "from-rotated" {
		t.Errorf("expected rotated line first, got %s", lines[0])
	}
	if lines[1] != "from-current" {
		t.Errorf("expected current line second, got %s", lines[1])
	}
}

func TestLogPaths(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "events.ndjson")

	// No rotated files — just base
	paths := logPaths(base)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if paths[0] != base {
		t.Errorf("expected base path, got %s", paths[0])
	}

	// Create .1 and .2
	_ = os.WriteFile(base+".1", []byte(""), 0o644)
	_ = os.WriteFile(base+".2", []byte(""), 0o644)

	paths = logPaths(base)
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}
	// Order: .2, .1, base (oldest first)
	if paths[0] != base+".2" {
		t.Errorf("expected .2 first, got %s", paths[0])
	}
	if paths[1] != base+".1" {
		t.Errorf("expected .1 second, got %s", paths[1])
	}
	if paths[2] != base {
		t.Errorf("expected base last, got %s", paths[2])
	}
}

func TestEmitter_Rotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	emitter, _ := NewEmitter(logPath)
	emitter.maxSizeBytes = 100
	emitter.keepCount = 2

	for i := 0; i < 20; i++ {
		_ = emitter.Emit(Event{
			TaskID:    "t1",
			EventType: "output",
			Data:      map[string]interface{}{"line": "this is a line of output padding"},
		})
	}

	// Rotated file should exist
	if _, err := os.Stat(logPath + ".1"); os.IsNotExist(err) {
		t.Error("expected rotated file .1 to exist")
	}
}

func TestNewEmitterCreatesDir(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sub", "dir", "events.ndjson")

	emitter, err := NewEmitter(logPath)
	if err != nil {
		t.Fatal(err)
	}

	_ = emitter.Emit(Event{TaskID: "t1", EventType: "test"})

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("expected log file to be created")
	}
}

