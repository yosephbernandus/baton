package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type WatchEvent struct {
	Type    string
	TaskID  string
	Payload interface{}
}

type Watcher struct {
	taskDir string
	watcher *fsnotify.Watcher
}

func NewWatcher(taskDir string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating watcher: %w", err)
	}
	return &Watcher{taskDir: taskDir, watcher: w}, nil
}

// Watch monitors a task directory for state changes.
func (w *Watcher) Watch(ctx context.Context, taskID string) <-chan WatchEvent {
	ch := make(chan WatchEvent, 16)
	dir := filepath.Join(w.taskDir, taskID)

	_ = os.MkdirAll(dir, 0o755)
	_ = w.watcher.Add(dir)

	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				we := classifyEvent(event.Name, taskID)
				if we != nil {
					select {
					case ch <- *we:
					case <-ctx.Done():
						return
					}
				}
			case _, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return ch
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}

func classifyEvent(path, taskID string) *WatchEvent {
	base := filepath.Base(path)
	switch base {
	case "stuck.yaml":
		var s StuckSignal
		if data, err := os.ReadFile(path); err == nil {
			_ = yaml.Unmarshal(data, &s)
		}
		return &WatchEvent{Type: "stuck", TaskID: taskID, Payload: s}

	case "result.yaml":
		var r ResultSignal
		if data, err := os.ReadFile(path); err == nil {
			_ = yaml.Unmarshal(data, &r)
		}
		return &WatchEvent{Type: r.Status, TaskID: taskID, Payload: r}

	case "worker-state.yaml":
		var ts TaskState
		if data, err := os.ReadFile(path); err == nil {
			_ = yaml.Unmarshal(data, &ts)
		}
		return &WatchEvent{Type: "state_change", TaskID: taskID, Payload: ts}

	case "progress.ndjson":
		return &WatchEvent{Type: "progress", TaskID: taskID}

	case "manifest.yaml":
		return &WatchEvent{Type: "manifest_update", TaskID: taskID}
	}
	return nil
}
