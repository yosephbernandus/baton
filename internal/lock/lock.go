package lock

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Lock struct {
	HeldBy string    `yaml:"held_by"`
	Type   string    `yaml:"type"`
	Since  time.Time `yaml:"since"`
}

type LockRegistry struct {
	Locks map[string]Lock `yaml:"locks"`
}

type Conflict struct {
	Path   string
	HeldBy string
	Since  time.Time
}

func (c Conflict) String() string {
	elapsed := time.Since(c.Since).Round(time.Second)
	return fmt.Sprintf("%s locked by %s (held %s)", c.Path, c.HeldBy, elapsed)
}

type Registry struct {
	path string
}

func NewRegistry(path string) *Registry {
	return &Registry{path: path}
}

func (r *Registry) Check(paths []string) ([]Conflict, error) {
	reg, err := r.load()
	if err != nil {
		return nil, err
	}

	var conflicts []Conflict
	for _, p := range paths {
		for lockPath, lock := range reg.Locks {
			if pathConflicts(p, lockPath) {
				conflicts = append(conflicts, Conflict{
					Path:   p,
					HeldBy: lock.HeldBy,
					Since:  lock.Since,
				})
			}
		}
	}
	return conflicts, nil
}

func (r *Registry) Acquire(taskID string, paths []string) error {
	reg, err := r.load()
	if err != nil {
		return err
	}

	for _, p := range paths {
		for lockPath, lock := range reg.Locks {
			if pathConflicts(p, lockPath) {
				return fmt.Errorf("cannot acquire lock on %s: held by %s since %s",
					p, lock.HeldBy, lock.Since.Format(time.RFC3339))
			}
		}
	}

	now := time.Now().UTC()
	for _, p := range paths {
		lockType := "file"
		if strings.HasSuffix(p, "/") {
			lockType = "prefix"
		}
		reg.Locks[p] = Lock{
			HeldBy: taskID,
			Type:   lockType,
			Since:  now,
		}
	}

	return r.save(reg)
}

func (r *Registry) Release(taskID string) error {
	reg, err := r.load()
	if err != nil {
		return err
	}

	for path, lock := range reg.Locks {
		if lock.HeldBy == taskID {
			delete(reg.Locks, path)
		}
	}

	return r.save(reg)
}

func (r *Registry) CleanStale(isAlive func(taskID string) bool) error {
	reg, err := r.load()
	if err != nil {
		return err
	}

	for path, lock := range reg.Locks {
		if !isAlive(lock.HeldBy) {
			delete(reg.Locks, path)
		}
	}

	return r.save(reg)
}

func (r *Registry) List() (map[string]Lock, error) {
	reg, err := r.load()
	if err != nil {
		return nil, err
	}
	return reg.Locks, nil
}

func (r *Registry) load() (*LockRegistry, error) {
	reg := &LockRegistry{Locks: make(map[string]Lock)}

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, fmt.Errorf("reading lock file: %w", err)
	}

	if err := yaml.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parsing lock file: %w", err)
	}

	if reg.Locks == nil {
		reg.Locks = make(map[string]Lock)
	}
	return reg, nil
}

func (r *Registry) save(reg *LockRegistry) error {
	data, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshaling lock file: %w", err)
	}

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}
	return os.Rename(tmpPath, r.path)
}

func pathConflicts(requested, held string) bool {
	if requested == held {
		return true
	}
	if strings.HasSuffix(held, "/") && strings.HasPrefix(requested, held) {
		return true
	}
	if strings.HasSuffix(requested, "/") && strings.HasPrefix(held, requested) {
		return true
	}
	return false
}
