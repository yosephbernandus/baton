package acp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/transport"
)

// Transport runs a worker over ACP instead of scraping its stdout.
//
// One Execute is one process, one session, one prompt turn. A session that
// spans phases is possible — agents advertise it — but baton's compaction gates
// and librarian assume baton owns the context between phases, so keeping the
// session per-turn preserves that. Caps reports Persistent as false to say so
// rather than implying a capability baton declines to use.
type Transport struct {
	cfg *config.Config
	log func(string, ...any)

	// caps memoises what each runtime reported at handshake.
	caps map[string]transport.Caps
}

// New builds an ACP transport over the given config.
func New(cfg *config.Config, log func(string, ...any)) *Transport {
	if log == nil {
		log = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}
	return &Transport{cfg: cfg, log: log, caps: make(map[string]transport.Caps)}
}

// Capabilities reports what the named runtime told us it can do.
//
// Capabilities are a property of the agent, not of the config: two agents
// speaking ACP expose model selection through different mechanisms, and neither
// is discoverable from YAML. Before a handshake has happened this reports the
// floor — everything absent — so a caller can never mistake "not yet asked" for
// "supported".
func (t *Transport) Capabilities(runtimeName string) transport.Caps {
	if c, ok := t.caps[runtimeName]; ok {
		return c
	}
	return transport.Caps{ToolRestriction: transport.RestrictNone}
}

// Probe establishes what a runtime can do without running any work. The
// handshake it performs — initialize then session/new — is what reveals the
// agent's mechanisms, and neither call invokes a model, so a preflight check
// can report an enforcement gap before a run rather than after.
func (t *Transport) Probe(ctx context.Context, runtimeName string) (transport.Caps, error) {
	if c, ok := t.caps[runtimeName]; ok && c.Probed {
		return c, nil
	}

	proc, err := t.spawn(ctx, runtimeName)
	if err != nil {
		return transport.Caps{ToolRestriction: transport.RestrictNone}, err
	}
	defer proc.close()

	// A probe asks no tool boundary and no model: it is only establishing what
	// the agent offers, and requesting either would change session state.
	caps, err := t.handshake(proc.ctx, proc.conn, proc.sess, transport.Request{RuntimeName: runtimeName})
	if err != nil {
		return transport.Caps{ToolRestriction: transport.RestrictNone}, err
	}
	t.caps[runtimeName] = caps
	return caps, nil
}

// Execute runs one prompt turn.
func (t *Transport) Execute(ctx context.Context, req transport.Request) (*transport.Result, error) {
	start := time.Now()

	proc, err := t.spawn(ctx, req.RuntimeName)
	if err != nil {
		return nil, err
	}
	defer proc.close()
	proc.sess.setBoundary(req.AllowedTools)

	sess, conn := proc.sess, proc.conn

	caps, err := t.handshake(proc.ctx, conn, sess, req)
	if err != nil {
		return t.failure(sess, start, err)
	}
	t.caps[req.RuntimeName] = caps

	// Apply liveness as a ceiling on the turn. ACP has no heartbeat of its
	// own: the prompt call simply blocks until the agent is done, so the
	// absolute timeout is the only bound that applies.
	turnCtx := proc.ctx
	if d := req.Liveness.AbsoluteTimeout; d > 0 {
		var stop context.CancelFunc
		turnCtx, stop = context.WithTimeout(proc.ctx, d)
		defer stop()
	}

	var resp promptResponse
	promptErr := conn.Call(turnCtx, methodSessionPrompt, promptRequest{
		SessionID: sess.sessionID,
		Prompt:    []contentBlock{textBlock(req.Prompt)},
	}, &resp)

	if promptErr != nil {
		// Tell the agent to stop before the process is killed, so a session it
		// persists is not left mid-turn.
		_ = conn.Notify(methodSessionCancel, cancelNotification{SessionID: sess.sessionID})
		return t.failure(sess, start, promptErr)
	}

	// The final marker usually arrives without a trailing newline, so flush
	// before reading the turn's events.
	sess.flush()

	events, output := sess.snapshot()
	result := &transport.Result{
		Status:   statusFor(resp.StopReason),
		Events:   events,
		Output:   output,
		Usage:    convertUsage(resp.Usage),
		Duration: time.Since(start),
	}
	if result.Status != "completed" {
		result.ExitCode = 1
		result.ErrorDetail = fmt.Sprintf("agent stopped: %s", resp.StopReason)
	}
	result.FilesChanged = filesTouched(events)
	return result, nil
}

// process is one spawned agent and the connection to it.
type process struct {
	ctx    context.Context
	conn   *Conn
	sess   *session
	cancel context.CancelFunc
	kill   func()
}

func (p *process) close() {
	p.cancel()
	p.kill()
}

// spawn starts the agent and serves its connection. Both Execute and Probe use
// it, so a probe and a real turn reach the agent exactly the same way.
func (t *Transport) spawn(ctx context.Context, runtimeName string) (*process, error) {
	rt, ok := t.cfg.Runtimes[runtimeName]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found", runtimeName)
	}

	runCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(runCtx, rt.Command, rt.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp stdout pipe: %w", err)
	}
	// stdout is the protocol stream, so the agent's logs must not land there.
	// Everything it writes to stderr is diagnostics.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting %s: %w", rt.Command, err)
	}

	go drainStderr(stderr, t.log)

	sess := newSession(nil, nil, t.log)
	conn := NewConn(stdout, stdin, sess, t.log)
	sess.conn = conn
	go func() { _ = conn.Serve(runCtx) }()

	return &process{
		ctx:    runCtx,
		conn:   conn,
		sess:   sess,
		cancel: cancel,
		kill: func() {
			_ = stdin.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		},
	}, nil
}

// handshake performs initialize and session/new, then negotiates model and tool
// boundary against whatever mechanism the agent turned out to expose.
func (t *Transport) handshake(
	ctx context.Context, conn *Conn, sess *session,
	req transport.Request,
) (transport.Caps, error) {
	caps := transport.Caps{Probed: true, ToolRestriction: transport.RestrictNone}

	var initResp initializeResponse
	err := conn.Call(ctx, methodInitialize, initializeRequest{
		ProtocolVersion: ProtocolVersion,
		// The agent shares baton's working tree, so it does its own file IO.
		ClientCapabilities: clientCapabilities{FS: fsCapabilities{}, Terminal: false},
		ClientInfo:         implementation{Name: "baton", Version: "1"},
	}, &initResp)
	if err != nil {
		return caps, fmt.Errorf("acp initialize: %w", err)
	}
	if initResp.ProtocolVersion != ProtocolVersion {
		return caps, fmt.Errorf(
			"acp protocol version %d from %s, baton implements %d",
			initResp.ProtocolVersion, initResp.AgentInfo.Name, ProtocolVersion)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return caps, fmt.Errorf("acp cwd: %w", err)
	}

	var newResp newSessionResponse
	if err := conn.Call(ctx, methodSessionNew, newSessionRequest{
		Cwd:        cwd,
		MCPServers: []any{},
	}, &newResp); err != nil {
		return caps, fmt.Errorf("acp session/new: %w", err)
	}
	if newResp.SessionID == "" {
		return caps, fmt.Errorf("acp session/new returned no session id")
	}
	sess.sessionID = newResp.SessionID

	// Everything below is discovery: what the agent actually offered.
	caps.Usage = true
	caps.FileLocations = true
	caps.Permission = true

	modelOpt, hasModelOpt := findOption(newResp.ConfigOptions, "model")
	caps.ModelSelect = hasModelOpt || newResp.Models != nil

	modeOpt, hasModeOpt := findOption(newResp.ConfigOptions, "mode")
	if hasModeOpt || newResp.Modes != nil {
		// A mode toggle can withhold edit tools but cannot name individual
		// ones. Permission requests are exact, but only for agents that ask.
		caps.ToolRestriction = transport.RestrictCoarse
	}

	if req.Model != "" && caps.ModelSelect {
		if err := t.selectModel(ctx, conn, sess.sessionID, req.Model, modelOpt, hasModelOpt, newResp.Models); err != nil {
			// A model baton could not select is worth reporting, not worth
			// failing the turn over: the agent still has a working default.
			t.log("acp: %v", err)
		}
	}

	if len(req.AllowedTools) > 0 && !allowsEditing(req.AllowedTools) {
		if err := t.selectReadOnlyMode(ctx, conn, sess.sessionID, modeOpt, hasModeOpt, newResp.Modes); err != nil {
			t.log("acp: %v", err)
		}
	}

	return caps, nil
}

func (t *Transport) selectModel(
	ctx context.Context, conn *Conn, sessionID, model string,
	opt configOption, hasOpt bool, models *modelState,
) error {
	if hasOpt {
		value, ok := matchChoice(opt.Options, model)
		if !ok {
			return fmt.Errorf("model %q not offered by the agent, keeping %q", model, opt.CurrentValue)
		}
		return conn.Call(ctx, methodSetConfigOption, setConfigOptionRequest{
			SessionID: sessionID, OptionID: "model", Value: value,
		}, nil)
	}
	if models != nil {
		for _, m := range models.AvailableModels {
			if strings.EqualFold(m.ModelID, model) || strings.Contains(strings.ToLower(m.ModelID), strings.ToLower(model)) {
				return conn.Call(ctx, methodSetModel, setModelRequest{
					SessionID: sessionID, ModelID: m.ModelID,
				}, nil)
			}
		}
		return fmt.Errorf("model %q not offered by the agent", model)
	}
	return nil
}

// selectReadOnlyMode asks the agent for a mode that withholds edit tools. This
// is the coarse half of role enforcement: it cannot name tools, but it is what
// keeps a reviewer from writing when the agent never asks permission.
func (t *Transport) selectReadOnlyMode(
	ctx context.Context, conn *Conn, sessionID string,
	opt configOption, hasOpt bool, modes *modeState,
) error {
	const wanted = "plan"

	if hasOpt {
		value, ok := matchChoice(opt.Options, wanted)
		if !ok {
			return fmt.Errorf("agent offers no read-only mode; role tool boundary is unenforced")
		}
		return conn.Call(ctx, methodSetConfigOption, setConfigOptionRequest{
			SessionID: sessionID, OptionID: "mode", Value: value,
		}, nil)
	}
	if modes != nil {
		for _, m := range modes.AvailableModes {
			if strings.EqualFold(m.ModeID, wanted) {
				return conn.Call(ctx, methodSetMode, setModeRequest{
					SessionID: sessionID, ModeID: m.ModeID,
				}, nil)
			}
		}
	}
	return fmt.Errorf("agent offers no read-only mode; role tool boundary is unenforced")
}

func (t *Transport) failure(sess *session, start time.Time, err error) (*transport.Result, error) {
	sess.flush()
	events, output := sess.snapshot()
	return &transport.Result{
		Status:      "failed",
		ExitCode:    1,
		Events:      events,
		Output:      output,
		Duration:    time.Since(start),
		ErrorDetail: err.Error(),
	}, nil
}

// allowsEditing reports whether a role's tool list includes any tool that can
// change files.
func allowsEditing(tools []string) bool {
	for _, t := range tools {
		switch t {
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			return true
		}
	}
	return false
}

func findOption(opts []configOption, id string) (configOption, bool) {
	for _, o := range opts {
		if o.ID == id {
			return o, true
		}
	}
	return configOption{}, false
}

// matchChoice resolves a configured name against what the agent offers. Exact
// match wins; otherwise a substring match, since agents qualify model ids with
// a provider prefix that baton's config does not carry.
func matchChoice(choices []configOptionChoice, want string) (string, bool) {
	for _, c := range choices {
		if strings.EqualFold(c.Value, want) || strings.EqualFold(c.Name, want) {
			return c.Value, true
		}
	}
	lower := strings.ToLower(want)
	for _, c := range choices {
		if strings.Contains(strings.ToLower(c.Value), lower) {
			return c.Value, true
		}
	}
	return "", false
}

// statusFor maps an ACP stop reason onto baton's task status vocabulary. Only
// end_turn is success: every other reason means the agent stopped short, and
// treating one as completion would let a truncated turn pass a phase gate.
func statusFor(reason string) string {
	switch reason {
	case stopEndTurn:
		return "completed"
	case stopCancelled:
		return "cancelled"
	case stopMaxTokens, stopMaxTurns, stopRefusal:
		return "failed"
	default:
		return "failed"
	}
}

// filesTouched collects the paths tool calls reported. The agent names them, so
// baton does not need a git diff to know what a phase changed.
func filesTouched(events []proto.Event) []string {
	seen := make(map[string]bool)
	var out []string
	for _, ev := range events {
		if ev.ToolCall == nil {
			continue
		}
		for _, p := range ev.ToolCall.Locations {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func convertUsage(u *usageInfo) *proto.Usage {
	if u == nil {
		return nil
	}
	return &proto.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CachedReadTokens: u.CachedReadTokens,
		TotalTokens:      u.TotalTokens,
	}
}

func drainStderr(r io.Reader, log func(string, ...any)) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			for _, line := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
				if line != "" {
					log("acp[stderr]: %s", line)
				}
			}
		}
		if err != nil {
			return
		}
	}
}
