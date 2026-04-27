package decisions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Record struct {
	TaskID    string    `yaml:"task_id"`
	Question  string    `yaml:"question"`
	Answer    string    `yaml:"answer"`
	Reason    string    `yaml:"reason"`
	DecidedBy string    `yaml:"decided_by"`
	Timestamp time.Time `yaml:"timestamp"`
}

type Store struct {
	path string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating decisions directory: %w", err)
	}
	return &Store{path: filepath.Join(dir, "decisions.yaml")}, nil
}

func (s *Store) Append(records ...Record) error {
	existing, _ := s.ReadAll()
	existing = append(existing, records...)
	return s.write(existing)
}

func (s *Store) ReadAll() ([]Record, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading decisions: %w", err)
	}

	var records []Record
	if err := yaml.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parsing decisions: %w", err)
	}
	return records, nil
}

func (s *Store) Search(query string) []Record {
	all, err := s.ReadAll()
	if err != nil {
		return nil
	}

	query = strings.ToLower(query)
	var matches []Record
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.Question), query) ||
			strings.Contains(strings.ToLower(r.Answer), query) ||
			strings.Contains(strings.ToLower(r.Reason), query) {
			matches = append(matches, r)
		}
	}
	return matches
}

func (s *Store) write(records []Record) error {
	data, err := yaml.Marshal(records)
	if err != nil {
		return fmt.Errorf("marshaling decisions: %w", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing decisions: %w", err)
	}
	return os.Rename(tmpPath, s.path)
}
