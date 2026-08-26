package acp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yosephbernandus/baton/internal/proto"
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
