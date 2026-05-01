package task

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yosephbernandus/baton/internal/spec"
	"gopkg.in/yaml.v3"
)

type Task struct {
	ID           string      `yaml:"id"`
	Runtime      string      `yaml:"runtime"`
	Model        string      `yaml:"model"`
	Status       string      `yaml:"status"`
	DispatchedBy string      `yaml:"dispatched_by"`
	Spec         *spec.Spec  `yaml:"spec,omitempty"`
	Escalation   Escalation  `yaml:"escalation"`
	Attempts     []Attempt   `yaml:"attempts"`
	CreatedAt    time.Time   `yaml:"created_at"`
	StartedAt    *time.Time  `yaml:"started_at,omitempty"`
	CompletedAt  *time.Time  `yaml:"completed_at,omitempty"`
	Duration     string      `yaml:"duration,omitempty"`
	PID          int         `yaml:"pid,omitempty"`
	ExitCode     *int        `yaml:"exit_code,omitempty"`
	FilesChanged []string    `yaml:"files_changed,omitempty"`
	OutputTail   []string    `yaml:"output_tail,omitempty"`
	Error        string      `yaml:"error,omitempty"`
}

type Escalation struct {
	WorkerClarification  string `yaml:"worker_clarification,omitempty"`
	OrchestratorAnalysis string `yaml:"orchestrator_analysis,omitempty"`
	HumanDecision        string `yaml:"human_decision,omitempty"`
	HumanReason          string `yaml:"human_reason,omitempty"`
}

type Attempt struct {
	Attempt     int        `yaml:"attempt"`
	StartedAt   time.Time  `yaml:"started_at"`
	CompletedAt *time.Time `yaml:"completed_at,omitempty"`
	Status      string     `yaml:"status"`
}

type Store struct {
	dir string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating task directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Create(t *Task) error {
	return s.write(t)
}

func (s *Store) Update(t *Task) error {
	return s.write(t)
}

func (s *Store) Get(id string) (*Task, error) {
	path := filepath.Join(s.dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading task %s: %w", id, err)
	}

	var t Task
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing task %s: %w", id, err)
	}
	return &t, nil
}

func (s *Store) List(filter string) ([]*Task, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tasks: %w", err)
	}

	var tasks []*Task
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}

		id := e.Name()[:len(e.Name())-5]
		t, err := s.Get(id)
		if err != nil {
			continue
		}

		if filter == "" || t.Status == filter {
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

func (s *Store) AddAttempt(id string, a Attempt) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	t.Attempts = append(t.Attempts, a)
	return s.Update(t)
}

func (s *Store) KillTask(id string) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}

	if t.PID > 0 {
		killProcessGroup(t.PID)
	}

	t.Status = "killed"
	now := time.Now().UTC()
	t.CompletedAt = &now
	return s.Update(t)
}

func (s *Store) CleanStale(maxAge time.Duration) ([]string, error) {
	tasks, err := s.List("")
	if err != nil {
		return nil, err
	}

	var cleaned []string
	cutoff := time.Now().UTC().Add(-maxAge)

	for _, t := range tasks {
		if t.Status != "running" && t.Status != "pending" {
			continue
		}
		if t.CreatedAt.Before(cutoff) {
			t.Status = "failed"
			t.Error = "stale: cleaned up after " + maxAge.String()
			now := time.Now().UTC()
			t.CompletedAt = &now
			if err := s.Update(t); err != nil {
				continue
			}
			cleaned = append(cleaned, t.ID)
		}
	}
	return cleaned, nil
}

func (s *Store) write(t *Task) error {
	data, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshaling task: %w", err)
	}

	tmpPath := filepath.Join(s.dir, t.ID+".yaml.tmp")
	finalPath := filepath.Join(s.dir, t.ID+".yaml")

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing task: %w", err)
	}

	return os.Rename(tmpPath, finalPath)
}
