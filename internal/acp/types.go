package acp

import "encoding/json"

// Wire types for the subset of ACP baton speaks, hand-derived from the vendored
// schema/v1/schema.json (see its SOURCE file).
//
// The asymmetry is deliberate. Outbound payloads are real structs, because we
// must produce exactly what the protocol expects. Inbound payloads are decoded
// leniently: fields we ignore stay absent, and an update variant we do not
// recognise is skipped rather than failing the turn. That keeps this file small
// — of the schema's 170 definitions, baton produces about fifteen — and keeps a
// spec addition from breaking a running pipeline.

// ProtocolVersion is the wire version baton implements and tests against. It is
// pinned: an agent negotiating something else is reported, not accommodated.
const ProtocolVersion = 1

// Methods baton sends.
const (
	methodInitialize      = "initialize"
	methodSessionNew      = "session/new"
	methodSessionPrompt   = "session/prompt"
	methodSessionCancel   = "session/cancel"
	methodSetConfigOption = "session/set_config_option"
	methodSetMode         = "session/set_mode"
)

// Methods the agent sends to us.
const (
	methodSessionUpdate     = "session/update"
	methodRequestPermission = "session/request_permission"
)

// --- initialize ---

type initializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
	ClientInfo         implementation     `json:"clientInfo"`
}

type clientCapabilities struct {
	FS       fsCapabilities `json:"fs"`
	Terminal bool           `json:"terminal"`
}

// baton's workers run in the same working tree baton does, so the agent reads
// and writes files itself. Advertising these as false keeps the agent from
// routing file IO through us for no benefit.
type fsCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
	AgentInfo         implementation    `json:"agentInfo"`
}

type agentCapabilities struct {
	LoadSession bool `json:"loadSession"`
}

// --- session/new ---

type newSessionRequest struct {
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

// newSessionResponse covers both mechanisms protocol v1 defines for selection.
// configOptions is the general one, carrying a category that says whether an
// option selects a model, a mode, or something else; modes is the dedicated
// mode selector. Which an agent populates is discovered per agent rather than
// assumed — OpenCode answers with configOptions.
type newSessionResponse struct {
	SessionID     string         `json:"sessionId"`
	ConfigOptions []configOption `json:"configOptions"`
	Modes         *modeState     `json:"modes"`
}

type configOption struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Category     string               `json:"category"`
	Type         string               `json:"type"`
	CurrentValue string               `json:"currentValue"`
	Options      []configOptionChoice `json:"options"`
}

type configOptionChoice struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type modeState struct {
	CurrentModeID  string     `json:"currentModeId"`
	AvailableModes []modeInfo `json:"availableModes"`
}

type modeInfo struct {
	ModeID string `json:"modeId"`
	Name   string `json:"name"`
}

// --- selection ---

// setConfigOptionRequest sets one session config option.
//
// The field is configId, not optionId. Getting that wrong is silent: the agent
// answers -32602 on stderr, the option keeps its previous value, and baton
// carries on believing it applied a restriction it did not. A read-only role ran
// with edit tools intact because of exactly this.
//
// A bare string Value is the wire default when no type discriminator is present,
// which is what a select option takes.
type setConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type setModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// --- session/prompt ---

type promptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func textBlock(s string) contentBlock { return contentBlock{Type: "text", Text: s} }

type promptResponse struct {
	StopReason string     `json:"stopReason"`
	Usage      *usageInfo `json:"usage"`
}

type usageInfo struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CachedReadTokens int `json:"cachedReadTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// Stop reasons that end a turn. Anything else is treated as a failure, so a
// reason added upstream fails loudly rather than passing as success.
const (
	stopEndTurn   = "end_turn"
	stopMaxTokens = "max_tokens"
	stopMaxTurns  = "max_turn_requests"
	stopRefusal   = "refusal"
	stopCancelled = "cancelled"
)

type cancelNotification struct {
	SessionID string `json:"sessionId"`
}

// --- session/update ---

type sessionNotification struct {
	SessionID string        `json:"sessionId"`
	Update    sessionUpdate `json:"update"`
}

// sessionUpdate is the lenient inbound union. Every field is optional; which
// ones are set depends on SessionUpdate. Variants baton does not handle decode
// into an otherwise-empty struct and are skipped.
type sessionUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`

	// content is polymorphic: an object on message and thought chunks, an
	// array of tool outputs on tool_call_update. Decoding it eagerly into one
	// shape makes the other shape fail the whole notification, so it is held
	// raw and interpreted per variant.
	Content json.RawMessage `json:"content"`

	// tool_call, tool_call_update
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Locations  []toolLocation  `json:"locations"`
	RawInput   json.RawMessage `json:"rawInput"`

	// plan
	Entries []planEntry `json:"entries"`
}

type toolLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line"`
}

type planEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

// session/update variants baton acts on.
const (
	updateAgentMessage = "agent_message_chunk"
	updateAgentThought = "agent_thought_chunk"
	updateToolCall     = "tool_call"
	updateToolCallEnd  = "tool_call_update"
	updatePlan         = "plan"
)

// --- session/request_permission ---

type requestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  sessionUpdate      `json:"toolCall"`
	Options   []permissionOption `json:"options"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

type requestPermissionResponse struct {
	Outcome permissionOutcome `json:"outcome"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// Permission outcomes. "cancelled" is distinct from a denial: it means the turn
// was aborted, not that the user refused.
const (
	outcomeSelected  = "selected"
	outcomeCancelled = "cancelled"
)
