package session

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/task"
)

type Watchdog struct {
	eventsPath  string
	sessionsDir string
	timeout     time.Duration
	emitter     *events.Emitter
}

func NewWatchdog(eventsPath, sessionsDir string, timeout time.Duration, emitter *events.Emitter) *Watchdog {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &Watchdog{
		eventsPath:  eventsPath,
		sessionsDir: sessionsDir,
		timeout:     timeout,
		emitter:     emitter,
	}
}

func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *Watchdog) check() {
	lastEvents := w.scanLastEvents()

	entries, err := os.ReadDir(w.sessionsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(w.sessionsDir, entry.Name())
		m, err := Load(path)
		if err != nil {
			continue
		}
		if !m.IsResumable() {
			continue
		}

		lastCmd := m.LastCommandAt
		if lastCmd.IsZero() {
			lastCmd = m.UpdatedAt
		}

		if le, ok := lastEvents[m.SessionID]; ok && le.After(lastCmd) {
			lastCmd = le
		}

		if time.Since(lastCmd) < w.timeout {
			continue
		}

		if m.CoordinatorPID > 0 && task.ProcessAlive(m.CoordinatorPID) {
			continue
		}

		m.MarkCrashed()
		if err := m.Save(path); err != nil {
			continue
		}

		if w.emitter != nil {
			_ = w.emitter.Emit(events.Event{
				TaskID:    m.SessionID,
				EventType: "session_timeout",
				Data: map[string]interface{}{
					"session_id":  m.SessionID,
					"last_active": lastCmd.Format(time.RFC3339),
					"timeout":     w.timeout.String(),
				},
			})
		}
	}
}

func (w *Watchdog) scanLastEvents() map[string]time.Time {
	f, err := os.Open(w.eventsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string]time.Time)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		var ev struct {
			Timestamp time.Time              `json:"ts"`
			TaskID    string                 `json:"task_id"`
			Data      map[string]interface{} `json:"data"`
		}
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		if sid, ok := ev.Data["session_id"].(string); ok && sid != "" {
			if ev.Timestamp.After(result[sid]) {
				result[sid] = ev.Timestamp
			}
		}
	}
	return result
}
