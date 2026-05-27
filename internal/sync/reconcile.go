package sync

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yosephbernandus/baton/internal/task"
	"gopkg.in/yaml.v3"
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

type subdirManifest struct {
	SessionID  string    `yaml:"session_id"`
	Status     string    `yaml:"status"`
	UpdatedAt  time.Time `yaml:"updated_at"`
	WorkerPID  int       `yaml:"worker_pid,omitempty"`
}

type subdirWorkerState struct {
	TaskID    string `yaml:"task_id"`
	State     string `yaml:"state"`
	WorkerPID int    `yaml:"worker_pid"`
}

func ReconcileSubdirs(taskDir string) (int, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(taskDir, entry.Name())

		if reconcileManifest(dir) {
			count++
		}
		if reconcileWorkerState(dir) {
			count++
		}
	}
	return count, nil
}

func reconcileManifest(dir string) bool {
	path := filepath.Join(dir, "manifest.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var m subdirManifest
	if yaml.Unmarshal(data, &m) != nil {
		return false
	}

	if m.Status != "running" {
		return false
	}
	if m.WorkerPID > 0 && task.ProcessAlive(m.WorkerPID) {
		return false
	}

	patched := replaceYAMLField(data, "status", "failed")
	if patched == nil {
		return false
	}
	return atomicWrite(path, patched)
}

func reconcileWorkerState(dir string) bool {
	path := filepath.Join(dir, "worker-state.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var ws subdirWorkerState
	if yaml.Unmarshal(data, &ws) != nil {
		return false
	}

	if ws.State == "completed" || ws.State == "failed" || ws.State == "idle" {
		return false
	}
	if ws.WorkerPID > 0 && task.ProcessAlive(ws.WorkerPID) {
		return false
	}

	patched := replaceYAMLField(data, "state", "failed")
	if patched == nil {
		return false
	}
	return atomicWrite(path, patched)
}

func replaceYAMLField(data []byte, field, newValue string) []byte {
	var raw map[string]interface{}
	if yaml.Unmarshal(data, &raw) != nil {
		return nil
	}
	raw[field] = newValue
	raw["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	out, err := yaml.Marshal(raw)
	if err != nil {
		return nil
	}
	return out
}

func atomicWrite(path string, data []byte) bool {
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return false
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
		return false
	}
	return true
}

func ReconcileAll(eventsPath string, store *task.Store, taskDir string) (int, int, error) {
	taskCount, err := Reconcile(eventsPath, store)
	if err != nil {
		return 0, 0, fmt.Errorf("task reconciliation: %w", err)
	}
	subdirCount, err := ReconcileSubdirs(taskDir)
	if err != nil {
		return taskCount, 0, fmt.Errorf("subdir reconciliation: %w", err)
	}
	return taskCount, subdirCount, nil
}
