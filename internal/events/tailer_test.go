package events

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTailer_ReadAll(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	emitter, _ := NewEmitter(logPath)
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "task_created"})
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "task_started"})
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "task_completed"})

	tailer := NewTailer(logPath)
	events, err := tailer.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
	if events[0].EventType != "task_created" {
		t.Errorf("expected task_created, got %s", events[0].EventType)
	}
	if events[2].EventType != "task_completed" {
		t.Errorf("expected task_completed, got %s", events[2].EventType)
	}
}

func TestTailer_ReadAllEmpty(t *testing.T) {
	tailer := NewTailer("/nonexistent/path.ndjson")
	events, err := tailer.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestTailer_Tail(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")
	_ = os.WriteFile(logPath, []byte{}, 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tailer := NewTailer(logPath)
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	emitter, _ := NewEmitter(logPath)
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "task_created"})
	_ = emitter.Emit(Event{TaskID: "t1", EventType: "task_completed"})

	var received []Event
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			received = append(received, ev)
			if len(received) >= 2 {
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:

	if len(received) != 2 {
		t.Errorf("expected 2 tailed events, got %d", len(received))
	}
}
