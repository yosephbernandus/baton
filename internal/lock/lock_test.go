package lock

import (
	"path/filepath"
	"testing"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	return NewRegistry(filepath.Join(dir, "locks.yaml"))
}

func TestAcquireAndRelease(t *testing.T) {
	reg := newTestRegistry(t)

	err := reg.Acquire("task-001", []string{"models/session.go", "migrations/"})
	if err != nil {
		t.Fatal(err)
	}

	locks, _ := reg.List()
	if len(locks) != 2 {
		t.Errorf("expected 2 locks, got %d", len(locks))
	}
	if locks["migrations/"].Type != "prefix" {
		t.Error("expected prefix lock for migrations/")
	}
	if locks["models/session.go"].Type != "file" {
		t.Error("expected file lock for models/session.go")
	}

	err = reg.Release("task-001")
	if err != nil {
		t.Fatal(err)
	}

	locks, _ = reg.List()
	if len(locks) != 0 {
		t.Errorf("expected 0 locks after release, got %d", len(locks))
	}
}

func TestConflictDetection(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Acquire("task-001", []string{"models/session.go"})

	err := reg.Acquire("task-002", []string{"models/session.go"})
	if err == nil {
		t.Error("expected conflict error")
	}

	conflicts, _ := reg.Check([]string{"models/session.go"})
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].HeldBy != "task-001" {
		t.Errorf("expected held by task-001, got %s", conflicts[0].HeldBy)
	}
}

func TestPrefixLockConflict(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Acquire("task-001", []string{"migrations/"})

	err := reg.Acquire("task-002", []string{"migrations/003_users.go"})
	if err == nil {
		t.Error("expected prefix conflict")
	}

	conflicts, _ := reg.Check([]string{"migrations/003_users.go"})
	if len(conflicts) != 1 {
		t.Errorf("expected 1 prefix conflict, got %d", len(conflicts))
	}
}

func TestReversePrefixConflict(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Acquire("task-001", []string{"migrations/003_users.go"})

	err := reg.Acquire("task-002", []string{"migrations/"})
	if err == nil {
		t.Error("expected reverse prefix conflict")
	}
}

func TestNoConflict(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Acquire("task-001", []string{"models/session.go"})

	err := reg.Acquire("task-002", []string{"models/user.go"})
	if err != nil {
		t.Errorf("expected no conflict, got: %v", err)
	}

	locks, _ := reg.List()
	if len(locks) != 2 {
		t.Errorf("expected 2 locks, got %d", len(locks))
	}
}

func TestAtomicAcquire(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Acquire("task-001", []string{"file-a.go"})

	err := reg.Acquire("task-002", []string{"file-b.go", "file-a.go"})
	if err == nil {
		t.Error("expected conflict on file-a.go")
	}

	locks, _ := reg.List()
	if len(locks) != 1 {
		t.Errorf("expected 1 lock (atomic — no partial acquire), got %d", len(locks))
	}
}

func TestCleanStale(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Acquire("task-alive", []string{"file-a.go"})
	reg.Acquire("task-dead", []string{"file-b.go"})

	reg.CleanStale(func(taskID string) bool {
		return taskID == "task-alive"
	})

	locks, _ := reg.List()
	if len(locks) != 1 {
		t.Errorf("expected 1 lock after stale cleanup, got %d", len(locks))
	}
	if _, ok := locks["file-a.go"]; !ok {
		t.Error("expected file-a.go lock to survive")
	}
}

func TestEmptyRegistryOperations(t *testing.T) {
	reg := newTestRegistry(t)

	conflicts, err := reg.Check([]string{"anything.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Error("expected no conflicts on empty registry")
	}

	err = reg.Release("nonexistent")
	if err != nil {
		t.Errorf("release on empty registry should not error: %v", err)
	}
}

func TestPathConflicts(t *testing.T) {
	tests := []struct {
		requested string
		held      string
		want      bool
	}{
		{"file.go", "file.go", true},
		{"migrations/003.go", "migrations/", true},
		{"migrations/", "migrations/003.go", true},
		{"models/user.go", "models/session.go", false},
		{"src/main.go", "migrations/", false},
		{"migrations/sub/deep.go", "migrations/", true},
	}

	for _, tt := range tests {
		got := pathConflicts(tt.requested, tt.held)
		if got != tt.want {
			t.Errorf("pathConflicts(%q, %q) = %v, want %v", tt.requested, tt.held, got, tt.want)
		}
	}
}
