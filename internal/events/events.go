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
	path string
	mu   sync.Mutex
}

func NewEmitter(path string) (*Emitter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating event log directory: %w", err)
	}
	return &Emitter{path: path}, nil
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
	defer f.Close()

	_, err = f.Write(line)
	return err
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
