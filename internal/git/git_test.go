package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	run("init")
	os.WriteFile(filepath.Join(dir, "initial.txt"), []byte("hello"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	return dir
}

func TestDetectChanges(t *testing.T) {
	before := &Snapshot{
		Modified:  []string{"a.go"},
		Untracked: []string{"tmp.log"},
	}
	after := &Snapshot{
		Modified:  []string{"a.go", "b.go"},
		Untracked: []string{"tmp.log", "new.txt"},
	}

	changed := DetectChanges(before, after)

	if len(changed) != 2 {
		t.Fatalf("expected 2 changes, got %d: %v", len(changed), changed)
	}

	found := map[string]bool{}
	for _, f := range changed {
		found[f] = true
	}
	if !found["b.go"] {
		t.Error("expected b.go in changes")
	}
	if !found["new.txt"] {
		t.Error("expected new.txt in changes")
	}
}

func TestDetectChanges_NilSnapshots(t *testing.T) {
	if got := DetectChanges(nil, &Snapshot{}); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	if got := DetectChanges(&Snapshot{}, nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestDetectChanges_NoChange(t *testing.T) {
	snap := &Snapshot{Modified: []string{"a.go"}, Untracked: []string{"b.txt"}}
	changed := DetectChanges(snap, snap)
	if len(changed) != 0 {
		t.Errorf("expected 0 changes, got %d", len(changed))
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a\nb\nc\n", 3},
		{"\n\n", 0},
		{"single", 1},
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitLines(%q) = %d items, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestTakeSnapshot_InGitRepo(t *testing.T) {
	dir := setupGitRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	snap, err := TakeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if len(snap.Modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(snap.Modified))
	}

	os.WriteFile(filepath.Join(dir, "initial.txt"), []byte("modified"), 0o644)
	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("new"), 0o644)

	snap2, err := TakeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Modified) != 1 {
		t.Errorf("expected 1 modified, got %d", len(snap2.Modified))
	}
	if len(snap2.Untracked) != 1 {
		t.Errorf("expected 1 untracked, got %d", len(snap2.Untracked))
	}

	changed := DetectChanges(snap, snap2)
	if len(changed) != 2 {
		t.Errorf("expected 2 changed files, got %d: %v", len(changed), changed)
	}
}
