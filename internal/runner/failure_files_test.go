package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
	"github.com/yosephbernandus/baton/internal/transport"
)

// gitRepo builds a throwaway repo with one committed file and makes it the
// process working directory, since the transport snapshots cwd.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "target.txt")
	run("commit", "-qm", "seed")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

func execRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	dir := gitRepo(t)

	store, err := task.NewStore(filepath.Join(dir, ".baton", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	emitter, err := events.NewEmitter(filepath.Join(dir, ".baton", "events.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {Command: "bash", PromptFlag: "-c", Models: []string{"default"}},
		},
		Orchestrator: config.OrchestratorConfig{Runtime: "test", Model: "test"},
		TaskDir:      filepath.Join(dir, ".baton", "tasks"),
		EventLog:     filepath.Join(dir, ".baton", "events.ndjson"),
		ClarifyExit:  10,
	}
	r := New(cfg, emitter, store)

	taskID := "failure-files"
	now := time.Now().UTC()
	if err := store.Create(&task.Task{
		ID: taskID, Runtime: "mock", Model: "default", Status: "running", CreatedAt: now,
		Attempts: []task.Attempt{{Attempt: 1, StartedAt: now, Status: "running"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(".baton", "tasks", taskID), 0o755); err != nil {
		t.Fatal(err)
	}
	return r, taskID
}

// A worker that edits files and then fails its acceptance checks still edited
// files. Reporting nothing hides the edit from role boundary verification, and
// tells dirty-bit tracking that nothing happened upstream — which skips the very
// verification phases that would have caught it.
func TestFailedAcceptanceStillReportsChangedFiles(t *testing.T) {
	r, taskID := execRunner(t)

	result, err := r.Execute(context.Background(), transport.Request{
		TaskID:      taskID,
		RuntimeName: "mock",
		Model:       "default",
		Prompt:      `echo "CHANGED" >> target.txt; echo "BATON:C:setup:done"`,
		Spec: &spec.Spec{
			AcceptanceChecks: []spec.Check{{Command: "false"}},
		},
		Liveness: LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if result.Status != "failed" {
		t.Fatalf("status=%q, want failed", result.Status)
	}
	if len(result.ChecksFailed) == 0 {
		t.Fatal("expected the acceptance check to be recorded as failed")
	}
	if !slices.Contains(result.FilesChanged, "target.txt") {
		t.Errorf("FilesChanged=%v, want target.txt — the worker edited it before failing",
			result.FilesChanged)
	}
}

func TestSuccessfulAcceptanceStillReportsChangedFiles(t *testing.T) {
	r, taskID := execRunner(t)

	result, err := r.Execute(context.Background(), transport.Request{
		TaskID:      taskID,
		RuntimeName: "mock",
		Model:       "default",
		Prompt:      `echo "CHANGED" >> target.txt; echo "BATON:C:setup:done"`,
		Spec: &spec.Spec{
			AcceptanceChecks: []spec.Check{{Command: "true"}},
		},
		Liveness: LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !slices.Contains(result.FilesChanged, "target.txt") {
		t.Errorf("FilesChanged=%v, want target.txt", result.FilesChanged)
	}
}

// A worker that exits non-zero after editing must report the edit too.
func TestFailedWorkerStillReportsChangedFiles(t *testing.T) {
	r, taskID := execRunner(t)

	result, err := r.Execute(context.Background(), transport.Request{
		TaskID:      taskID,
		RuntimeName: "mock",
		Model:       "default",
		Prompt:      `echo "CHANGED" >> target.txt; exit 1`,
		Liveness:    LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status=%q, want failed", result.Status)
	}
	if !slices.Contains(result.FilesChanged, "target.txt") {
		t.Errorf("FilesChanged=%v, want target.txt", result.FilesChanged)
	}
}
