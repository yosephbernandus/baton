package sync

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	"github.com/yosephbernandus/baton/internal/task"
)

type terminalEvent struct {
	Status    string
	Timestamp time.Time
}

type rawEvent struct {
	Timestamp time.Time `json:"ts"`
	TaskID    string    `json:"task_id"`
	EventType string   `json:"event"`
}

func readTerminalEvents(path string) (map[string]terminalEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	terminals := make(map[string]terminalEvent)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		var ev rawEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		if ev.TaskID == "" {
			continue
		}
		var status string
		switch ev.EventType {
		case "task_completed", "phase_completed":
			status = "completed"
		case "task_failed", "phase_failed":
			status = "failed"
		case "task_killed":
			status = "killed"
		default:
			continue
		}
		terminals[ev.TaskID] = terminalEvent{Status: status, Timestamp: ev.Timestamp}
	}
	return terminals, scanner.Err()
}

func Reconcile(eventsPath string, store *task.Store) (int, error) {
	terminals, err := readTerminalEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	tasks, err := store.List("")
	if err != nil {
		return 0, err
	}

	count := 0
	for _, t := range tasks {
		if t.Status != "running" && t.Status != "pending" {
			continue
		}
		te, ok := terminals[t.ID]
		if !ok {
			continue
		}
		t.Status = te.Status
		now := time.Now().UTC()
		t.CompletedAt = &now
		if err := store.Update(t); err != nil {
			continue
		}
		count++
	}
	return count, nil
}
