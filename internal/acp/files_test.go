package acp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/transport"
)

func toolEvent(paths ...string) proto.Event {
	return proto.Event{ToolCall: &proto.ToolCall{
		ID: "c1", Name: "write", Kind: "edit", Status: "completed", Locations: paths,
	}}
}

// Git is authoritative. A change made through a shell command names no files in
// any tool call, so a transport that trusted tool reports alone would tell role
// verification that nothing was touched.
func TestGitChangesSurviveWithoutAnyToolReport(t *testing.T) {
	got := mergeFilesChanged([]string{"internal/a.go", "internal/b.go"}, nil)
	if len(got) != 2 || !slices.Contains(got, "internal/a.go") {
		t.Fatalf("files=%v, want both git paths", got)
	}
}

// Tool locations add what git cannot report, such as a write to an ignored path.
func TestToolReportsAddPathsGitDidNotSee(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	events := []proto.Event{toolEvent(filepath.Join(cwd, "build", "artifact.bin"))}

	got := mergeFilesChanged([]string{"internal/a.go"}, events)
	want := filepath.Join("build", "artifact.bin")
	if !slices.Contains(got, want) {
		t.Errorf("files=%v, want it to include %q", got, want)
	}
}

// Agents report absolute paths and git reports repo-relative ones. Downstream
// matching on role file scope and declared locks compares plain strings, so one
// file reported both ways must not appear twice in two forms.
func TestOnePathReportedTwoWaysIsNotDuplicated(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	events := []proto.Event{toolEvent(filepath.Join(cwd, "internal", "a.go"))}

	got := mergeFilesChanged([]string{filepath.Join("internal", "a.go")}, events)
	if len(got) != 1 {
		t.Fatalf("files=%v, want one entry — the same file in two path forms", got)
	}
	if filepath.IsAbs(got[0]) {
		t.Errorf("files=%v, want the repo-relative form", got)
	}
}

// A path outside the working tree is left as it is. Rewriting it into something
// that looks like a repo path would misreport where the change happened.
func TestPathOutsideTheTreeKeepsItsAbsoluteForm(t *testing.T) {
	outside := filepath.Join(string(filepath.Separator), "etc", "somewhere", "x.conf")
	got := mergeFilesChanged(nil, []proto.Event{toolEvent(outside)})
	if len(got) != 1 || got[0] != outside {
		t.Errorf("files=%v, want the absolute path preserved", got)
	}
}

func TestEventsWithoutToolCallsContributeNothing(t *testing.T) {
	events := []proto.Event{
		proto.MarkerEvent(proto.Marker{Type: proto.MarkerNote, Msg: "hi"}, "BATON:N:hi"),
		{Usage: &proto.Usage{TotalTokens: 10}},
	}
	if got := mergeFilesChanged(nil, events); len(got) != 0 {
		t.Errorf("files=%v, want none", got)
	}
}

func TestDuplicateGitPathsAreCollapsed(t *testing.T) {
	got := mergeFilesChanged([]string{"a.go", "a.go", "", "b.go"}, nil)
	if len(got) != 2 {
		t.Errorf("files=%v, want a.go and b.go once each", got)
	}
}

// Ordering is git first, then anything only the agent reported, so the
// authoritative record leads the list.
func TestGitPathsComeFirst(t *testing.T) {
	cwd, _ := os.Getwd()
	events := []proto.Event{toolEvent(filepath.Join(cwd, "z-agent-only.txt"))}

	got := mergeFilesChanged([]string{"a-from-git.go"}, events)
	if len(got) != 2 || got[0] != "a-from-git.go" {
		t.Errorf("files=%v, want the git path first", got)
	}
}

func TestRelativiseLeavesRelativePathsAlone(t *testing.T) {
	if got := relativise("/repo", "internal/a.go"); got != "internal/a.go" {
		t.Errorf("relativise=%q, want it unchanged", got)
	}
}

func TestRelativiseWithoutARootIsANoOp(t *testing.T) {
	if got := relativise("", "/abs/a.go"); got != "/abs/a.go" {
		t.Errorf("relativise=%q, want it unchanged when the root is unknown", got)
	}
}

// A live turn reported the working directory itself as a tool location,
// alongside the file it actually edited. A directory in this list reads as a
// modified file to role verification and to dirty-bit tracking.
func TestDirectoryLocationsAreNotReportedAsChanges(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	events := []proto.Event{toolEvent(cwd, filepath.Join(cwd, "transport.go"))}

	got := mergeFilesChanged(nil, events)
	if slices.Contains(got, ".") {
		t.Errorf("files=%v, want the working directory dropped", got)
	}
	if !slices.Contains(got, "transport.go") {
		t.Errorf("files=%v, want the real file kept", got)
	}
}

// A deleted file cannot be stat'd, and dropping it would hide exactly the change
// worth noticing.
func TestDeletedFilesAreStillReported(t *testing.T) {
	cwd, _ := os.Getwd()
	gone := filepath.Join(cwd, "this-file-was-deleted.go")

	got := mergeFilesChanged(nil, []proto.Event{toolEvent(gone)})
	if !slices.Contains(got, "this-file-was-deleted.go") {
		t.Errorf("files=%v, want a path that no longer exists to be kept", got)
	}
}

// A turn that edits files and then fails still edited files. Reporting nothing
// hides the edit from role boundary verification, and tells dirty-bit tracking
// that nothing happened upstream — which skips the verification phases that
// would have caught it.
//
// The agent is a shell script speaking just enough ACP to reach the prompt and
// then die, so the failure path runs for real rather than being simulated.
func TestFailedTurnStillReportsChangedFiles(t *testing.T) {
	dir := acpGitRepo(t)

	agent := filepath.Join(dir, "fake-agent.sh")
	script := `#!/usr/bin/env bash
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentInfo":{"name":"fake","version":"0"}}}\n' "$id" ;;
    *'"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"s1"}}\n' "$id" ;;
    *'"session/prompt"'*)
      # Edit a file, then die without answering: a turn that fails mid-work.
      echo CHANGED >> target.txt
      exit 1 ;;
  esac
done
`
	if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"fake": {Command: agent, Protocol: config.ProtocolACP},
	}}
	tr := New(cfg, func(string, ...any) {})

	res, err := tr.Execute(context.Background(), transport.Request{
		TaskID: "t1", RuntimeName: "fake", Prompt: "edit it",
		Liveness: transport.LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "failed" {
		t.Fatalf("status=%q, want failed", res.Status)
	}
	if !slices.Contains(res.FilesChanged, "target.txt") {
		t.Errorf("FilesChanged=%v, want target.txt — the agent edited it before dying",
			res.FilesChanged)
	}
}

// A handshake that never succeeds means no work ran, so there is nothing to
// attribute. It must still not error out on the missing snapshot.
func TestFailedHandshakeReportsNoFiles(t *testing.T) {
	dir := acpGitRepo(t)

	agent := filepath.Join(dir, "broken-agent.sh")
	if err := os.WriteFile(agent, []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"fake": {Command: agent, Protocol: config.ProtocolACP},
	}}
	tr := New(cfg, func(string, ...any) {})

	res, err := tr.Execute(context.Background(), transport.Request{
		TaskID: "t1", RuntimeName: "fake", Prompt: "x",
		Liveness: transport.LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "failed" {
		t.Errorf("status=%q, want failed", res.Status)
	}
	if len(res.FilesChanged) != 0 {
		t.Errorf("FilesChanged=%v, want none: no work ran", res.FilesChanged)
	}
	if res.ErrorDetail == "" {
		t.Error("expected the handshake failure to be described")
	}
}

// acpGitRepo builds a throwaway repo with one committed file and makes it the
// process working directory, since the transport snapshots cwd.
func acpGitRepo(t *testing.T) string {
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

// Persistent sessions are declined, not unavailable. Agents advertise them —
// OpenCode answers initialize with loadSession, fork, list and resume — but
// baton holds one session per turn so that compaction gates and the librarian
// keep operating on the whole of a worker's context, and so an L3 retry can
// actually start fresh.
//
// This is a decision with reasons, recorded in docs/adr/027-per-turn-acp-sessions.md.
// If it is ever reversed, read that first: the objections are about correctness,
// not effort.
func TestSessionsAreDeliberatelyPerTurn(t *testing.T) {
	dir := acpGitRepo(t)

	agent := filepath.Join(dir, "capable-agent.sh")
	// An agent advertising every session capability there is.
	script := `#!/usr/bin/env bash
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"close":{},"fork":{},"list":{},"resume":{}}},"agentInfo":{"name":"capable","version":"0"}}}\n' "$id" ;;
    *'"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"s1"}}\n' "$id" ;;
  esac
done
`
	if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"capable": {Command: agent, Protocol: config.ProtocolACP},
	}}
	tr := New(cfg, func(string, ...any) {})

	caps, err := tr.Probe(context.Background(), "capable")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !caps.Probed {
		t.Fatal("Probed=false after a successful handshake")
	}
	if caps.Persistent {
		t.Error("Persistent=true: baton must not claim a cross-phase session it does not hold")
	}
}
