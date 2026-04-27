package cost

import (
	"path/filepath"
	"testing"
	"time"
)

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		model    string
		duration time.Duration
		wantMin  float64
		wantMax  float64
	}{
		{"kimi", 5 * time.Minute, 0.009, 0.011},
		{"opus", 2 * time.Minute, 0.14, 0.16},
		{"test", 1 * time.Minute, 0.0, 0.001},
		{"unknown-model", 1 * time.Minute, 0.009, 0.011},
	}

	for _, tt := range tests {
		got := EstimateCost(tt.model, tt.duration)
		if got < tt.wantMin || got > tt.wantMax {
			t.Errorf("EstimateCost(%q, %v) = %f, want [%f, %f]", tt.model, tt.duration, got, tt.wantMin, tt.wantMax)
		}
	}
}

func TestEstimateCost_MinOneMinute(t *testing.T) {
	got := EstimateCost("kimi", 10*time.Second)
	if got != 0.002 {
		t.Errorf("expected minimum 1-minute charge (0.002), got %f", got)
	}
}

func TestTracker_RecordAndSummarize(t *testing.T) {
	dir := t.TempDir()
	tracker, err := NewTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	tracker.Record(Entry{
		TaskID:   "t1",
		Runtime:  "opencode",
		Model:    "kimi",
		Duration: 3 * time.Minute,
		Status:   "completed",
	})
	tracker.Record(Entry{
		TaskID:   "t2",
		Runtime:  "opencode",
		Model:    "deepseek",
		Duration: 5 * time.Minute,
		Status:   "completed",
	})
	tracker.Record(Entry{
		TaskID:   "t3",
		Runtime:  "aider",
		Model:    "gpt-4o",
		Duration: 2 * time.Minute,
		Status:   "failed",
	})

	summary, err := tracker.Summarize()
	if err != nil {
		t.Fatal(err)
	}

	if summary.TotalTasks != 3 {
		t.Errorf("expected 3 tasks, got %d", summary.TotalTasks)
	}
	if summary.TotalEstimate <= 0 {
		t.Error("expected positive total estimate")
	}
	if summary.ByStatus["completed"] != 2 {
		t.Errorf("expected 2 completed, got %d", summary.ByStatus["completed"])
	}
	if summary.ByStatus["failed"] != 1 {
		t.Errorf("expected 1 failed, got %d", summary.ByStatus["failed"])
	}
	if _, ok := summary.ByModel["kimi"]; !ok {
		t.Error("expected kimi in by_model")
	}
	if _, ok := summary.ByRuntime["aider"]; !ok {
		t.Error("expected aider in by_runtime")
	}
}

func TestTracker_ReadAll_Empty(t *testing.T) {
	dir := t.TempDir()
	tracker := &Tracker{path: filepath.Join(dir, "nonexistent.ndjson")}
	entries, err := tracker.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
