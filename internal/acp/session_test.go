package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/proto"
)

// replayGolden feeds a recorded agent→client transcript through the session the
// same way the connection would. The transcript in testdata is a real OpenCode
// 1.17.16 turn, captured over stdio; replaying it pins the mapping from ACP
// updates onto baton's event vocabulary against a live agent's actual output
// rather than against the schema's description of it.
func replayGolden(t *testing.T, allowedTools []string) *session {
	t.Helper()

	f, err := os.Open("testdata/opencode-turn.ndjson")
	if err != nil {
		t.Fatalf("opening golden transcript: %v", err)
	}
	defer f.Close()

	s := newSession(nil, allowedTools, func(string, ...any) {})

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var rec struct {
			Dir string `json:"dir"`
			Raw string `json:"raw"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil || rec.Dir != "in" {
			continue
		}
		var m message
		if err := json.Unmarshal([]byte(rec.Raw), &m); err != nil {
			continue
		}
		if m.Method != "" && m.ID == nil {
			s.Notify(m.Method, m.Params)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading golden transcript: %v", err)
	}
	// The transport flushes at turn end; the replay must too, or text left
	// without a trailing newline never lands.
	s.flush()
	return s
}

func TestGoldenTranscriptYieldsToolCalls(t *testing.T) {
	s := replayGolden(t, nil)
	events, _ := s.snapshot()

	var calls []*proto.ToolCall
	for _, ev := range events {
		if ev.ToolCall != nil {
			calls = append(calls, ev.ToolCall)
		}
	}
	if len(calls) == 0 {
		t.Fatal("no tool calls recovered from the golden transcript")
	}

	// The turn asked the agent to write one file, so every tool call shares an
	// id and the last one reports completion.
	last := calls[len(calls)-1]
	if last.Status != "completed" {
		t.Errorf("final tool call status=%q, want completed", last.Status)
	}
	if last.ID == "" {
		t.Error("tool call carries no id")
	}
}

// OpenCode announces a call with kind "edit" and a bare title, then rewrites the
// title to the file path by completion. Carrying the kind forward from the
// announcement is what keeps the final event classifiable.
func TestGoldenTranscriptCarriesToolKindForward(t *testing.T) {
	s := replayGolden(t, nil)
	events, _ := s.snapshot()

	for _, ev := range events {
		if ev.ToolCall == nil {
			continue
		}
		if ev.ToolCall.Kind != "edit" {
			t.Errorf("tool call %q kind=%q, want edit on every update", ev.ToolCall.Name, ev.ToolCall.Kind)
		}
	}
}

func TestGoldenTranscriptReportsTouchedFile(t *testing.T) {
	s := replayGolden(t, nil)
	events, _ := s.snapshot()

	files := filesTouched(events)
	if len(files) != 1 {
		t.Fatalf("files=%v, want exactly one touched path", files)
	}
	if !strings.HasSuffix(files[0], "hello.txt") {
		t.Errorf("touched file=%q, want the file the turn wrote", files[0])
	}
}

// Thoughts are recorded for visibility but never parsed for markers: an agent
// reasoning about finishing has not claimed to have finished.
func TestGoldenTranscriptKeepsThoughtsOutOfEvents(t *testing.T) {
	s := replayGolden(t, nil)
	events, output := s.snapshot()

	var thoughts int
	for _, line := range output {
		if strings.HasPrefix(line, "[thinking] ") {
			thoughts++
		}
	}
	if thoughts == 0 {
		t.Fatal("golden transcript has agent thoughts; none were recorded")
	}
	for _, ev := range events {
		if ev.Marker != nil && strings.Contains(ev.Raw, "[thinking]") {
			t.Errorf("marker parsed out of a thought: %q", ev.Raw)
		}
	}
}

func TestMarkersParsedFromAgentMessages(t *testing.T) {
	s := newSession(nil, nil, nil)
	s.handleUpdate(sessionUpdate{
		SessionUpdate: updateAgentMessage,
		Content:       mustRaw(contentBlock{Type: "text", Text: "working on it\nBATON:C:setup:done:all good"}),
	})

	s.flush()

	events, _ := s.snapshot()
	cp, ok := proto.LastCompletion(events, "setup")
	if !ok {
		t.Fatal("completion marker not recovered from an agent message")
	}
	if cp.Status != "done" || cp.Detail != "all good" {
		t.Errorf("completion=%+v", cp)
	}
}

func TestMarkersNotParsedFromThoughts(t *testing.T) {
	s := newSession(nil, nil, nil)
	s.handleUpdate(sessionUpdate{
		SessionUpdate: updateAgentThought,
		Content:       mustRaw(contentBlock{Type: "text", Text: "I could just emit BATON:C:setup:done here"}),
	})

	s.flush()

	events, _ := s.snapshot()
	if _, ok := proto.LastCompletion(events, "setup"); ok {
		t.Fatal("a completion claimed inside reasoning must not count")
	}
}

// An update variant baton does not handle must be skipped, not fatal: the
// protocol grows, and a pipeline mid-run should survive an addition.
func TestUnknownUpdateVariantIsIgnored(t *testing.T) {
	s := newSession(nil, nil, nil)
	s.Notify(methodSessionUpdate, json.RawMessage(
		`{"sessionId":"s1","update":{"sessionUpdate":"something_new","payload":{"a":1}}}`))

	events, output := s.snapshot()
	if len(events) != 0 || len(output) != 0 {
		t.Errorf("unknown variant produced events=%v output=%v, want both empty", events, output)
	}
}

func permissionFor(tool string, allowed []string, opts []permissionOption) requestPermissionResponse {
	return permissionForKind(tool, "edit", allowed, opts)
}

func permissionForKind(title, kind string, allowed []string, opts []permissionOption) requestPermissionResponse {
	s := newSession(nil, allowed, nil)
	return s.decidePermission(requestPermissionParams{
		SessionID: "s1",
		ToolCall:  sessionUpdate{Title: title, Kind: kind},
		Options:   opts,
	})
}

var standardOptions = []permissionOption{
	{OptionID: "allow_once", Kind: "allow_once", Name: "Allow once"},
	{OptionID: "allow_always", Kind: "allow_always", Name: "Allow always"},
	{OptionID: "reject_once", Kind: "reject_once", Name: "Deny"},
}

func TestPermissionAllowsToolInsideBoundary(t *testing.T) {
	got := permissionForKind("Read", "read", []string{"Read", "Grep"}, standardOptions)
	if got.Outcome.Outcome != outcomeSelected || got.Outcome.OptionID != "allow_once" {
		t.Errorf("outcome=%+v, want allow_once selected", got.Outcome)
	}
}

func TestPermissionDeniesToolOutsideBoundary(t *testing.T) {
	got := permissionFor("Write", []string{"Read", "Grep"}, standardOptions)
	if got.Outcome.Outcome != outcomeSelected || got.Outcome.OptionID != "reject_once" {
		t.Errorf("outcome=%+v, want reject_once selected", got.Outcome)
	}
}

// An unrestricted role must not be blocked: developer declares no boundary, and
// treating "no boundary" as "deny everything" would stall every write phase.
func TestPermissionAllowsEverythingWhenUnrestricted(t *testing.T) {
	got := permissionFor("Write", nil, standardOptions)
	if got.Outcome.Outcome != outcomeSelected || got.Outcome.OptionID != "allow_once" {
		t.Errorf("outcome=%+v, want allow_once selected", got.Outcome)
	}
}

// Options are chosen by kind, never by position. An agent that lists denial
// first must not have that read as approval.
func TestPermissionPicksByKindNotPosition(t *testing.T) {
	reversed := []permissionOption{
		{OptionID: "no", Kind: "reject_once", Name: "Deny"},
		{OptionID: "yes", Kind: "allow_once", Name: "Allow"},
	}
	if got := permissionForKind("Read", "read", []string{"Read"}, reversed); got.Outcome.OptionID != "yes" {
		t.Errorf("optionId=%q, want the allow option regardless of order", got.Outcome.OptionID)
	}
	if got := permissionFor("Write", []string{"Read"}, reversed); got.Outcome.OptionID != "no" {
		t.Errorf("optionId=%q, want the reject option regardless of order", got.Outcome.OptionID)
	}
}

// If an agent offers no way to refuse, baton cancels rather than approving a
// call it is supposed to block.
func TestPermissionCancelsWhenNoRefusalOffered(t *testing.T) {
	allowOnly := []permissionOption{{OptionID: "allow_once", Kind: "allow_once", Name: "Allow"}}
	got := permissionFor("Write", []string{"Read"}, allowOnly)
	if got.Outcome.Outcome != outcomeCancelled {
		t.Errorf("outcome=%+v, want cancelled", got.Outcome)
	}
}

func TestPermissionDenialIsRecordedAsANote(t *testing.T) {
	s := newSession(nil, []string{"Read"}, nil)
	s.decidePermission(requestPermissionParams{
		ToolCall: sessionUpdate{Title: "Write", Kind: "edit"},
		Options:  standardOptions,
	})
	events, _ := s.snapshot()
	notes := proto.Notes(events)
	if len(notes) != 1 || !strings.Contains(notes[0], "Write") {
		t.Errorf("notes=%v, want one note naming the denied tool", notes)
	}
}

func TestUnhandledInboundMethodAnswersMethodNotFound(t *testing.T) {
	s := newSession(nil, nil, nil)
	_, err := s.Request(context.Background(), "ping", nil)
	re, ok := err.(*rpcError)
	if !ok || re.Code != codeMethodNotFound {
		t.Fatalf("err=%v, want a -32601 rpcError; an off-spec probe is not a failure", err)
	}
}

func TestStatusForOnlyEndTurnIsSuccess(t *testing.T) {
	if statusFor(stopEndTurn) != "completed" {
		t.Error("end_turn must be completed")
	}
	for _, reason := range []string{stopMaxTokens, stopMaxTurns, stopRefusal, "invented_upstream"} {
		if got := statusFor(reason); got != "failed" {
			t.Errorf("statusFor(%q)=%q, want failed — a short turn must not pass a phase gate", reason, got)
		}
	}
	if statusFor(stopCancelled) != "cancelled" {
		t.Error("cancelled must be distinct from failed")
	}
}

// Message chunks are streaming deltas: a live OpenCode turn split
// "BATON:C:setup:done:ok" across three chunks as "BAT", "ON:C:setup",
// ":done:ok". Parsing each chunk on arrival loses the marker and with it the
// phase's completion signal, so text is buffered to line boundaries.
func TestMarkerSplitAcrossStreamingChunks(t *testing.T) {
	s := newSession(nil, nil, nil)
	for _, chunk := range []string{"BAT", "ON:C:setup", ":done:ok"} {
		s.handleUpdate(sessionUpdate{
			SessionUpdate: updateAgentMessage,
			Content:       mustRaw(contentBlock{Type: "text", Text: chunk}),
		})
	}

	if events, _ := s.snapshot(); len(events) != 0 {
		t.Fatalf("events=%v before flush, want none — the line is still partial", events)
	}

	s.flush()

	events, _ := s.snapshot()
	cp, ok := proto.LastCompletion(events, "setup")
	if !ok {
		t.Fatal("completion lost when the marker spanned chunks")
	}
	if cp.Status != "done" || cp.Detail != "ok" {
		t.Errorf("completion=%+v", cp)
	}
}

// A chunk boundary must not split one logical line into two output lines.
func TestChunkBoundaryDoesNotSplitOutputLines(t *testing.T) {
	s := newSession(nil, nil, nil)
	for _, chunk := range []string{"first ", "line\nsecond ", "line"} {
		s.handleUpdate(sessionUpdate{
			SessionUpdate: updateAgentMessage,
			Content:       mustRaw(contentBlock{Type: "text", Text: chunk}),
		})
	}
	s.flush()

	_, output := s.snapshot()
	want := []string{"first line", "second line"}
	if len(output) != len(want) {
		t.Fatalf("output=%q, want %q", output, want)
	}
	for i := range want {
		if output[i] != want[i] {
			t.Errorf("output[%d]=%q, want %q", i, output[i], want[i])
		}
	}
}

// flush must be safe to call when nothing is buffered, and must not re-record
// what it already emitted.
func TestFlushIsIdempotent(t *testing.T) {
	s := newSession(nil, nil, nil)
	s.handleUpdate(sessionUpdate{
		SessionUpdate: updateAgentMessage,
		Content:       mustRaw(contentBlock{Type: "text", Text: "BATON:N:hello"}),
	})
	s.flush()
	s.flush()

	events, output := s.snapshot()
	if n := len(proto.Notes(events)); n != 1 {
		t.Errorf("notes=%d, want 1", n)
	}
	if len(output) != 1 {
		t.Errorf("output=%q, want one line", output)
	}
}

// Titles are free text and agents fill them differently. omp titles a shell call
// with the command it is about to run, so matching titles against the role's
// tool names denied "go vet ./..." to a role that explicitly permits Bash — and
// a read-only phase failed three times for want of the tools it was entitled to.
func TestPermissionJudgesTheKindNotTheTitle(t *testing.T) {
	// lead: Read, Grep, Glob, Bash — a shell command titled with the command.
	for _, title := range []string{
		"go vet ./...",
		"go run .",
		"git diff --name-only && git diff -- greet.go",
	} {
		got := permissionForKind(title, "execute", []string{"Read", "Grep", "Glob", "Bash"}, standardOptions)
		if got.Outcome.OptionID != "allow_once" {
			t.Errorf("%q denied; the role permits Bash and the kind is execute", title)
		}
	}
}

// test_lead permits Read, Grep, Glob and no Bash, so a command is still refused.
func TestPermissionDeniesExecuteWithoutBash(t *testing.T) {
	got := permissionForKind("go test ./...", "execute", []string{"Read", "Grep", "Glob"}, standardOptions)
	if got.Outcome.OptionID != "reject_once" {
		t.Errorf("outcome=%+v, want a refusal: the role has no Bash", got.Outcome)
	}
}

func TestPermissionMapsToolKindsToRoleTools(t *testing.T) {
	readOnly := []string{"Read", "Grep", "Glob"}
	writer := []string{"Read", "Edit", "Write", "Bash"}

	cases := []struct {
		kind    string
		allowed []string
		want    bool
	}{
		{"read", readOnly, true},
		{"search", readOnly, true},
		{"edit", readOnly, false},
		{"delete", readOnly, false},
		{"move", readOnly, false},
		{"execute", readOnly, false},
		{"edit", writer, true},
		{"execute", writer, true},
	}
	for _, c := range cases {
		got := permissionForKind("t", c.kind, c.allowed, standardOptions)
		allowed := got.Outcome.OptionID == "allow_once"
		if allowed != c.want {
			t.Errorf("kind %q with %v: allowed=%v, want %v", c.kind, c.allowed, allowed, c.want)
		}
	}
}

// A kind baton does not model is allowed. Denying what it cannot classify is how
// the title-matching bug did its damage, and the gateway already reports this
// restriction as partial.
func TestPermissionAllowsKindsItDoesNotModel(t *testing.T) {
	for _, kind := range []string{"think", "fetch", "switch_mode", "other", "", "invented_later"} {
		got := permissionForKind("t", kind, []string{"Read"}, standardOptions)
		if got.Outcome.OptionID != "allow_once" {
			t.Errorf("kind %q denied; baton models no boundary for it", kind)
		}
	}
}
