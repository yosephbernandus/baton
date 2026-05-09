package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	SessionID  string        `yaml:"session_id"`
	StartedAt  time.Time     `yaml:"started_at"`
	UpdatedAt  time.Time     `yaml:"updated_at"`
	Status     string        `yaml:"status"`
	SpecPath   string        `yaml:"spec_path"`
	Complexity string        `yaml:"complexity"`
	Pipeline   PipelineState `yaml:"pipeline"`
	Budget     BudgetState   `yaml:"budget"`
}

type PipelineState struct {
	CurrentPhase    int   `yaml:"current_phase"`
	PhasesCompleted []int `yaml:"phases_completed"`
	PhasesSkipped   []int `yaml:"phases_skipped"`
	L2Cycles        int   `yaml:"l2_cycles"`
}

type BudgetState struct {
	L1RetriesTotal int `yaml:"l1_retries_total"`
	L2CyclesTotal  int `yaml:"l2_cycles_total"`
}

func New(sessionID, specPath, complexity string) *Manifest {
	now := time.Now()
	return &Manifest{
		SessionID:  sessionID,
		StartedAt:  now,
		UpdatedAt:  now,
		Status:     "running",
		SpecPath:   specPath,
		Complexity: complexity,
	}
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}

func (m *Manifest) Save(path string) error {
	m.UpdatedAt = time.Now()
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (m *Manifest) AdvancePhase(phaseID int) {
	m.Pipeline.CurrentPhase = phaseID
	m.Pipeline.PhasesCompleted = appendUnique(m.Pipeline.PhasesCompleted, phaseID)
}

func (m *Manifest) SetSkipped(skipped []int) {
	m.Pipeline.PhasesSkipped = skipped
}

func (m *Manifest) RecordL1Retry() {
	m.Budget.L1RetriesTotal++
}

func (m *Manifest) RecordL2Cycle() {
	m.Pipeline.L2Cycles++
	m.Budget.L2CyclesTotal++
}

func (m *Manifest) LoopBackTo(phaseID int) {
	m.Pipeline.CurrentPhase = phaseID
}

func (m *Manifest) MarkCompleted() {
	m.Status = "completed"
}

func (m *Manifest) MarkFailed(reason string) {
	m.Status = "failed"
}

func (m *Manifest) MarkCrashed() {
	m.Status = "crashed"
}

func (m *Manifest) IsResumable() bool {
	return m.Status == "running" || m.Status == "crashed"
}

func (m *Manifest) LastCompletedPhase() int {
	if len(m.Pipeline.PhasesCompleted) == 0 {
		return 0
	}
	max := 0
	for _, id := range m.Pipeline.PhasesCompleted {
		if id > max {
			max = id
		}
	}
	return max
}

func appendUnique(slice []int, val int) []int {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}
