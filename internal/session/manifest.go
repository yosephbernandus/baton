package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	SessionID           string        `yaml:"session_id"`
	StartedAt           time.Time     `yaml:"started_at"`
	UpdatedAt           time.Time     `yaml:"updated_at"`
	Status              string        `yaml:"status"`
	SpecPath            string        `yaml:"spec_path"`
	Complexity          string        `yaml:"complexity"`
	GitHead             string        `yaml:"git_head,omitempty"`
	SpecCoreHash        string        `yaml:"spec_core_hash,omitempty"`
	WorkerPID           int           `yaml:"worker_pid,omitempty"`
	CoordinatorPID      int           `yaml:"coordinator_pid,omitempty"`
	LastCommandAt       time.Time     `yaml:"last_command_at,omitempty"`
	ResumeCount         int           `yaml:"resume_count"`
	PhaseResumeAttempts map[int]int   `yaml:"phase_resume_attempts,omitempty"`
	Pipeline            PipelineState `yaml:"pipeline"`
	Budget              BudgetState   `yaml:"budget"`
	PhaseRecords        []PhaseRecord `yaml:"phase_records,omitempty"`
	PipelineFiles       []string      `yaml:"pipeline_files,omitempty"`
}

type PhaseRecord struct {
	ID           int       `yaml:"id"`
	Name         string    `yaml:"name"`
	Status       string    `yaml:"status"`
	Notes        []string  `yaml:"notes,omitempty"`
	Errors       []string  `yaml:"errors,omitempty"`
	FilesChanged []string  `yaml:"files_changed,omitempty"`
	Attempts     int       `yaml:"attempts"`
	Duration     string    `yaml:"duration,omitempty"`
	FailReason   string    `yaml:"fail_reason,omitempty"`
	CompletedAt  time.Time `yaml:"completed_at,omitempty"`
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
	return m.Status == "running" || m.Status == "crashed" || m.Status == "rate_limited"
}

func (m *Manifest) MarkRateLimited(reason string) {
	m.Status = "rate_limited"
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

func (m *Manifest) RemainingL1Retries(max int) int { return max - m.Budget.L1RetriesTotal }
func (m *Manifest) RemainingL2Cycles(max int) int  { return max - m.Budget.L2CyclesTotal }

func SessionPath(specID string) string {
	return filepath.Join(".baton", "sessions", specID+".yaml")
}

func (m *Manifest) AddPhaseRecord(r PhaseRecord) {
	for i, existing := range m.PhaseRecords {
		if existing.ID == r.ID {
			m.PhaseRecords[i] = r
			return
		}
	}
	m.PhaseRecords = append(m.PhaseRecords, r)
}

func (m *Manifest) AddPipelineFiles(files []string) {
	seen := make(map[string]bool)
	for _, f := range m.PipelineFiles {
		seen[f] = true
	}
	for _, f := range files {
		if !seen[f] {
			m.PipelineFiles = append(m.PipelineFiles, f)
			seen[f] = true
		}
	}
}

func appendUnique(slice []int, val int) []int {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}
