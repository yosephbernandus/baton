// Package transport defines how baton talks to a worker, independent of the
// mechanism. A subprocess whose stdout carries BATON: markers and an agent
// speaking a structured protocol are both transports; the phase machine knows
// only this vocabulary.
//
// The rule that keeps it honest: a Request states intent, never mechanism. The
// pipeline says "this role may use these tools" and the transport decides how —
// CLI flags, a session mode, or a refusal reported through Caps.
package transport

import (
	"context"
	"time"

	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/spec"
)

// ToolRestrictionKind describes how precisely a transport can constrain the
// tools a worker may use.
type ToolRestrictionKind string

const (
	// RestrictNone means the transport cannot constrain tools at all.
	RestrictNone ToolRestrictionKind = "none"
	// RestrictCoarse means the transport can only toggle broad classes, such
	// as "no edits", without naming individual tools.
	RestrictCoarse ToolRestrictionKind = "coarse"
	// RestrictPerTool means the transport can allow an explicit tool list.
	RestrictPerTool ToolRestrictionKind = "per-tool"
)

// Caps reports what a transport can actually do for a given runtime.
//
// Capabilities are discovered, not configured. Two agents speaking the same
// protocol expose different mechanisms — one advertises a model list, another a
// generic config option — so a transport reports Caps from what the runtime
// tells it, and config only says how to reach the runtime.
//
// The pipeline never assumes a capability. Where one is missing, the gateway
// reports the gap before execution rather than letting enforcement fail
// silently at runtime.
type Caps struct {
	ToolRestriction ToolRestrictionKind
	ModelSelect     bool
	Permission      bool
	Usage           bool
	FileLocations   bool
	Persistent      bool
}

// LivenessConfig controls how a transport detects and handles an unresponsive
// worker.
type LivenessConfig struct {
	SilenceTimeout     time.Duration
	AbsoluteTimeout    time.Duration
	SilenceWarning     time.Duration
	StartupTimeout     time.Duration
	NetworkIdleTimeout time.Duration
	AttemptTimeout     time.Duration
	TickInterval       time.Duration // how often watchdog checks; 0 defaults to 30s
}

// Request is one unit of work handed to a transport.
type Request struct {
	TaskID      string
	RuntimeName string
	Model       string
	Prompt      string
	Spec        *spec.Spec
	Liveness    LivenessConfig

	// AllowedTools names the tools the worker's role permits. It is intent:
	// the transport translates it into whatever its mechanism supports, and
	// reports through Caps.ToolRestriction how faithfully it can. Empty means
	// no restriction was asked for.
	AllowedTools []string

	// ExtraArgs is an escape hatch for transports built on a command line. A
	// transport without one ignores it.
	ExtraArgs []string
}

// Result is what a transport reports back.
type Result struct {
	Status        string
	ExitCode      int
	Crashed       bool
	Clarification string
	Output        []string

	// Events is the transport-neutral view of what the worker did. The
	// pipeline reads this and never re-parses Output; transports without a
	// text stream still fill it.
	Events []proto.Event

	// Usage is nil unless the transport reports token accounting.
	Usage *proto.Usage

	ChecksFailed []string
	FilesChanged []string
	Duration     time.Duration
	SocketPath   string
	ErrorDetail  string
}

// Transport executes work for the pipeline.
type Transport interface {
	// Capabilities reports what this transport can do for the named runtime.
	Capabilities(runtimeName string) Caps
	// Execute runs one request to completion.
	Execute(ctx context.Context, req Request) (*Result, error)
}
