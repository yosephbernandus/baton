package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	TaskID    string        `json:"task_id"`
	Runtime   string        `json:"runtime"`
	Model     string        `json:"model"`
	Duration  time.Duration `json:"duration_ns"`
	Status    string        `json:"status"`
	Estimate  float64       `json:"estimate_usd"`
	Timestamp time.Time     `json:"ts"`
}

type Summary struct {
	TotalTasks    int                `json:"total_tasks"`
	TotalEstimate float64           `json:"total_estimate_usd"`
	ByModel       map[string]float64 `json:"by_model"`
	ByRuntime     map[string]float64 `json:"by_runtime"`
	ByStatus      map[string]int     `json:"by_status"`
}

var modelRates = map[string]float64{
	"opus":          0.075,
	"sonnet":        0.015,
	"kimi":          0.002,
	"deepseek":      0.001,
	"deepseek-r1":   0.003,
	"gemini-flash":  0.001,
	"gpt-4o":        0.025,
	"claude-sonnet": 0.015,
	"gemini":        0.005,
	"grok":          0.010,
	"test":          0.000,
}

func EstimateCost(model string, duration time.Duration) float64 {
	rate, ok := modelRates[model]
	if !ok {
		rate = 0.010
	}
	minutes := duration.Minutes()
	if minutes < 1 {
		minutes = 1
	}
	return rate * minutes
}

type Tracker struct {
	path string
}

func NewTracker(dir string) (*Tracker, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cost directory: %w", err)
	}
	return &Tracker{path: filepath.Join(dir, "costs.ndjson")}, nil
}

func (t *Tracker) Record(entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Estimate == 0 {
		entry.Estimate = EstimateCost(entry.Model, entry.Duration)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cost entry: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(t.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening cost log: %w", err)
	}
	defer f.Close() //nolint:errcheck

	_, err = f.Write(line)
	return err
}

func (t *Tracker) Summarize() (*Summary, error) {
	entries, err := t.ReadAll()
	if err != nil {
		return nil, err
	}

	s := &Summary{
		ByModel:   make(map[string]float64),
		ByRuntime: make(map[string]float64),
		ByStatus:  make(map[string]int),
	}

	for _, e := range entries {
		s.TotalTasks++
		s.TotalEstimate += e.Estimate
		s.ByModel[e.Model] += e.Estimate
		s.ByRuntime[e.Runtime] += e.Estimate
		s.ByStatus[e.Status]++
	}

	return s, nil
}

func (t *Tracker) ReadAll() ([]Entry, error) {
	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cost log: %w", err)
	}

	var entries []Entry
	for _, line := range splitLines(string(data)) {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
