package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/yosephbernandus/baton/internal/proto"
)

// session drives one ACP conversation and collects what the pipeline needs.
//
// Baton's prompt carries the BATON: marker protocol, so an ACP agent still
// reports completion the way a subprocess worker does — as text. The difference
// is where that text arrives: in agent_message_chunk updates rather than on
// stdout. Markers are parsed from message chunks only, never from thoughts:
// reasoning aloud about finishing is not a claim to have finished.
type session struct {
	conn      *Conn
	sessionID string

	// effectiveModel is what the agent reported it is running, which is the
	// only way to know when the request asked for "auto".
	effectiveModel string

	// allowedTools is the role boundary. Empty means unrestricted.
	allowedTools map[string]bool
	restricted   bool

	mu     sync.Mutex
	events []proto.Event
	output []string

	// toolKind remembers what a tool_call announced, so a later
	// tool_call_update that omits the kind still reports it.
	toolKind map[string]string

	// Message and thought chunks are streaming deltas, split wherever the
	// model emitted a token — a live turn splits "BATON:C:setup:done" across
	// three chunks. Text is buffered until a newline so markers are only ever
	// parsed from complete lines.
	msgBuf     string
	thoughtBuf string

	// stderrTail keeps the agent's last diagnostic lines. Agents put their real
	// explanations there — a JSON-RPC error carries "Internal error" while
	// stderr says which model the adapter is too old for — so a failure that
	// has nothing better to report quotes these.
	stderrTail []string

	log func(string, ...any)
}

func newSession(conn *Conn, allowedTools []string, log func(string, ...any)) *session {
	s := &session{
		conn:     conn,
		toolKind: make(map[string]string),
		log:      log,
	}
	s.setBoundary(allowedTools)
	if s.log == nil {
		s.log = func(string, ...any) {}
	}
	return s
}

// setBoundary installs the role's tool list. Empty means unrestricted, which is
// what a probe uses: it establishes what the agent offers without asking for a
// restriction that would change session state.
func (s *session) setBoundary(allowedTools []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(allowedTools) == 0 {
		s.restricted = false
		s.allowedTools = nil
		return
	}
	s.restricted = true
	s.allowedTools = make(map[string]bool, len(allowedTools))
	for _, t := range allowedTools {
		s.allowedTools[t] = true
	}
}

// Notify handles inbound notifications. Unknown methods are ignored: an agent
// is free to send updates baton has no use for.
func (s *session) Notify(method string, params json.RawMessage) {
	if method != methodSessionUpdate {
		return
	}
	var n sessionNotification
	if err := json.Unmarshal(params, &n); err != nil {
		s.log("acp: undecodable session/update: %v", err)
		return
	}
	s.handleUpdate(n.Update)
}

// Request handles inbound calls. Anything baton does not implement answers
// -32601, which is the correct reply to an off-spec liveness probe and not
// something to treat as an error.
func (s *session) Request(_ context.Context, method string, params json.RawMessage) (any, error) {
	if method != methodRequestPermission {
		return nil, errMethodNotFound
	}
	var p requestPermissionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decoding permission request: %w", err)
	}
	return s.decidePermission(p), nil
}

// decidePermission enforces the role's tool boundary at the moment the agent
// asks. This is the one place restriction can be exact rather than coarse, so
// when an agent does ask, baton answers from the boundary rather than waving it
// through.
func (s *session) decidePermission(p requestPermissionParams) requestPermissionResponse {
	name := toolName(p.ToolCall)

	s.mu.Lock()
	restricted, allowed := s.restricted, s.allowedTools[name]
	s.mu.Unlock()

	if restricted && !allowed {
		s.recordEvent(proto.Event{
			Marker: markerPtr(proto.Marker{
				Type: proto.MarkerNote,
				Msg:  fmt.Sprintf("denied %s: outside the role's tool boundary", name),
			}),
			Raw: fmt.Sprintf("[permission] denied %s", name),
		})
		if opt, ok := pickOption(p.Options, "reject_once", "reject"); ok {
			return requestPermissionResponse{Outcome: permissionOutcome{Outcome: outcomeSelected, OptionID: opt}}
		}
		// No refusal option offered. Cancelling is the honest answer: baton
		// will not approve a call it is supposed to block.
		return requestPermissionResponse{Outcome: permissionOutcome{Outcome: outcomeCancelled}}
	}

	if opt, ok := pickOption(p.Options, "allow_once", "allow"); ok {
		return requestPermissionResponse{Outcome: permissionOutcome{Outcome: outcomeSelected, OptionID: opt}}
	}
	return requestPermissionResponse{Outcome: permissionOutcome{Outcome: outcomeCancelled}}
}

// pickOption finds the first option whose kind matches exactly, then the first
// whose kind carries the prefix. Agents word these differently, and picking by
// position would be a coin flip between allowing and denying.
func pickOption(opts []permissionOption, exact, prefix string) (string, bool) {
	for _, o := range opts {
		if o.Kind == exact {
			return o.OptionID, true
		}
	}
	for _, o := range opts {
		if strings.HasPrefix(o.Kind, prefix) {
			return o.OptionID, true
		}
	}
	return "", false
}

func (s *session) handleUpdate(u sessionUpdate) {
	switch u.SessionUpdate {
	case updateAgentMessage:
		s.msgBuf = s.consumeLines(s.msgBuf+contentText(u.Content), "", true)

	case updateAgentThought:
		// Visible for debugging, but never parsed for markers: reasoning about
		// finishing is not a claim to have finished.
		s.thoughtBuf = s.consumeLines(s.thoughtBuf+contentText(u.Content), "[thinking] ", false)

	case updateToolCall, updateToolCallEnd:
		s.recordToolCall(u)

	case updatePlan:
		for _, e := range u.Entries {
			s.recordOutput(fmt.Sprintf("[plan] %s %s", e.Status, e.Content))
		}
	}
}

func (s *session) recordToolCall(u sessionUpdate) {
	name := toolName(u)

	s.mu.Lock()
	kind := u.Kind
	if kind == "" {
		kind = s.toolKind[u.ToolCallID]
	} else if u.ToolCallID != "" {
		s.toolKind[u.ToolCallID] = kind
	}
	s.mu.Unlock()

	var locations []string
	for _, l := range u.Locations {
		if l.Path != "" {
			locations = append(locations, l.Path)
		}
	}

	s.recordEvent(proto.Event{
		ToolCall: &proto.ToolCall{
			ID:        u.ToolCallID,
			Name:      name,
			Kind:      kind,
			Status:    u.Status,
			Locations: locations,
		},
		Raw: fmt.Sprintf("[tool] %s %s", name, u.Status),
	})
	s.recordOutput(fmt.Sprintf("[tool] %s %s", name, u.Status))
}

// consumeLines records every complete line in buf and returns the unterminated
// remainder, to be prepended to the next chunk.
func (s *session) consumeLines(buf, prefix string, parseMarkers bool) string {
	for {
		i := strings.IndexByte(buf, '\n')
		if i < 0 {
			return buf
		}
		s.recordLine(buf[:i], prefix, parseMarkers)
		buf = buf[i+1:]
	}
}

func (s *session) recordLine(line, prefix string, parseMarkers bool) {
	s.recordOutput(prefix + line)
	if !parseMarkers {
		return
	}
	if mk, ok := proto.ParseMarker(line); ok {
		s.recordEvent(proto.MarkerEvent(mk, line))
	}
}

// flush records whatever the agent left without a trailing newline. A turn that
// ends on its completion marker — which is the normal case — has that marker
// sitting in the buffer, so skipping this would drop it.
func (s *session) flush() {
	if s.msgBuf != "" {
		s.recordLine(s.msgBuf, "", true)
		s.msgBuf = ""
	}
	if s.thoughtBuf != "" {
		s.recordLine(s.thoughtBuf, "[thinking] ", false)
		s.thoughtBuf = ""
	}
}

const stderrTailLines = 5

func (s *session) recordStderr(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stderrTail = append(s.stderrTail, line)
	if len(s.stderrTail) > stderrTailLines {
		s.stderrTail = s.stderrTail[len(s.stderrTail)-stderrTailLines:]
	}
}

func (s *session) lastStderr() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.stderrTail))
	copy(out, s.stderrTail)
	return out
}

func (s *session) recordEvent(ev proto.Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *session) recordOutput(line string) {
	s.mu.Lock()
	s.output = append(s.output, line)
	s.mu.Unlock()
}

func (s *session) snapshot() ([]proto.Event, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]proto.Event, len(s.events))
	copy(events, s.events)
	output := make([]string, len(s.output))
	copy(output, s.output)
	return events, output
}

// toolName prefers the title an agent gives a call. Titles carry the tool name
// on announcement; by completion OpenCode rewrites the title to the file it
// touched, which is why the kind is remembered from the first update instead.
func toolName(u sessionUpdate) string {
	if u.Title != "" {
		return u.Title
	}
	return u.ToolCallID
}

// contentText reads the text of a message or thought chunk. Anything that is
// not a single text-bearing object — notably the array form tool_call_update
// uses — yields nothing rather than an error.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var block contentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return ""
	}
	return block.Text
}

func markerPtr(mk proto.Marker) *proto.Marker { return &mk }
