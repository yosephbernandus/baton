package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Timestamp    time.Time              `json:"ts"`
	TaskID       string                 `json:"task_id"`
	Runtime      string                 `json:"runtime"`
	Model        string                 `json:"model"`
	DispatchedBy string                 `json:"dispatched_by"`
	EventType    string                 `json:"event"`
	Data         map[string]interface{} `json:"data"`
}

type Emitter struct {
	path         string
	mu           sync.Mutex
	maxSizeBytes int64
	keepCount    int
}

func NewEmitter(path string) (*Emitter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating event log directory: %w", err)
	}
	return &Emitter{path: path, maxSizeBytes: 10 * 1024 * 1024, keepCount: 3}, nil
}

func NewEmitterWithRotation(path string, maxSizeMB, keepCount int) (*Emitter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating event log directory: %w", err)
	}
	return &Emitter{path: path, maxSizeBytes: int64(maxSizeMB) * 1024 * 1024, keepCount: keepCount}, nil
}

func (e *Emitter) Emit(ev Event) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}
	line = append(line, '\n')

	e.mu.Lock()
	defer e.mu.Unlock()

	f, err := os.OpenFile(e.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening event log: %w", err)
	}
	defer f.Close() //nolint:errcheck

	_, err = f.Write(line)
	if err != nil {
		return err
	}

	info, err := f.Stat()
	if err == nil && e.maxSizeBytes > 0 && info.Size() > e.maxSizeBytes {
		_ = f.Close()
		e.rotate()
		return nil
	}

	return nil
}

func (e *Emitter) rotate() {
	for i := e.keepCount - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", e.path, i)
		new := fmt.Sprintf("%s.%d", e.path, i+1)
		_ = os.Rename(old, new)
	}
	_ = os.Rename(e.path, e.path+".1")

	if e.keepCount > 0 {
		remove := fmt.Sprintf("%s.%d", e.path, e.keepCount+1)
		_ = os.Remove(remove)
	}
}

func (e *Emitter) TaskEvent(taskID, runtime, model, dispatchedBy, eventType string, data map[string]interface{}) error {
	return e.Emit(Event{
		TaskID:       taskID,
		Runtime:      runtime,
		Model:        model,
		DispatchedBy: dispatchedBy,
		EventType:    eventType,
		Data:         data,
	})
}
