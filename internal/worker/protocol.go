package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/role"
	"github.com/yosephbernandus/baton/internal/session"
	"github.com/yosephbernandus/baton/internal/spec"
)

type State string

const (
	StateIdle      State = "idle"
	StateStarted   State = "started"
	StateWorking   State = "working"
	StateStuck     State = "stuck"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

type TaskState struct {
	TaskID      string    `yaml:"task_id"`
	Phase       int       `yaml:"phase"`
	PhaseName   string    `yaml:"phase_name"`
	Role        string    `yaml:"role"`
	State       State     `yaml:"state"`
	Complexity  string    `yaml:"complexity"`
	StartedAt   time.Time `yaml:"started_at"`
	UpdatedAt   time.Time `yaml:"updated_at"`
	WorkerPID   int       `yaml:"worker_pid"`
	Reflections []string  `yaml:"reflections,omitempty"`
}

type StuckSignal struct {
	Question  string    `yaml:"question"`
	AskedAt   time.Time `yaml:"asked_at"`
	WorkerPID int       `yaml:"worker_pid"`
}

type GuidanceResponse struct {
	Answer    string    `yaml:"answer"`
	From      string    `yaml:"from"`
	SentAt    time.Time `yaml:"sent_at"`
}

type ResultSignal struct {
	Status    string    `yaml:"status"`
	Reason    string    `yaml:"reason,omitempty"`
	Phase     int       `yaml:"phase"`
	PhaseName string    `yaml:"phase_name"`
	DoneAt    time.Time `yaml:"done_at"`
}

type ProgressEntry struct {
	Type    string `json:"type"`
	Time    string `json:"ts"`
	Msg     string `json:"msg"`
	Pct     int    `json:"pct,omitempty"`
	TaskID  string `json:"task_id"`
	Phase   int    `json:"phase,omitempty"`
}

func taskDir(cfg *config.Config, taskID string) string {
	return filepath.Join(cfg.TaskDir, taskID)
}

func loadTaskState(dir string) (*TaskState, error) {
	data, err := os.ReadFile(filepath.Join(dir, "worker-state.yaml"))
	if err != nil {
		return nil, err
	}
	var ts TaskState
	if err := yaml.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("parsing worker state: %w", err)
	}
	return &ts, nil
}

func saveTaskState(dir string, ts *TaskState) error {
	ts.UpdatedAt = time.Now()
	data, err := yaml.Marshal(ts)
	if err != nil {
		return fmt.Errorf("marshaling worker state: %w", err)
	}
	tmpPath := filepath.Join(dir, "worker-state.yaml.tmp")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(dir, "worker-state.yaml"))
}

// Init creates a task directory, loads spec, resolves first phase, and generates instructions.
// Returns the task ID and the path to generated instructions.
func Init(cfg *config.Config, specPath, complexity, runtimeName string) (string, string, error) {
	s, err := spec.Load(specPath)
	if err != nil {
		return "", "", fmt.Errorf("loading spec: %w", err)
	}

	if complexity == "" {
		complexity = s.EstimatedComplexity
	}
	if complexity == "" {
		complexity = cfg.PhaseMachine.ComplexityDefault
	}
	if complexity == "" {
		complexity = phase.ComplexityMedium
	}

	specBase := filepath.Base(specPath)
	ext := filepath.Ext(specBase)
	taskID := specBase[:len(specBase)-len(ext)]

	dir := taskDir(cfg, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating task dir: %w", err)
	}

	phases := phase.DefaultPhases()
	active := phase.ActivePhases(phases, complexity)
	if len(active) == 0 {
		return "", "", fmt.Errorf("no active phases for complexity %s", complexity)
	}

	firstPhase := active[0]
	ts := &TaskState{
		TaskID:     taskID,
		Phase:      firstPhase.ID,
		PhaseName:  firstPhase.Name,
		Role:       firstPhase.Role,
		State:      StateIdle,
		Complexity: complexity,
		StartedAt:  time.Now(),
		WorkerPID:  os.Getpid(),
	}
	if err := saveTaskState(dir, ts); err != nil {
		return "", "", fmt.Errorf("saving initial state: %w", err)
	}

	// Save session manifest
	manifest := session.New(taskID, specPath, complexity)
	skipped := phase.SkippedPhaseIDs(phases, complexity)
	manifest.SetSkipped(skipped)
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := manifest.Save(manifestPath); err != nil {
		return "", "", fmt.Errorf("saving manifest: %w", err)
	}

	// Generate instructions
	batonBin := cfg.WorkerProtocol.BatonBinary
	if batonBin == "" {
		batonBin = "baton"
	}

	icfg := InstructionConfig{
		Runtime:    Runtime(runtimeName),
		TaskID:     taskID,
		Spec:       s,
		Phase:      firstPhase,
		Complexity: complexity,
		BatonBin:   batonBin,
	}

	instructions := GenerateInstructions(icfg)
	instrPath := filepath.Join(dir, "instructions.md")
	if err := os.WriteFile(instrPath, []byte(instructions), 0o644); err != nil {
		return "", "", fmt.Errorf("writing instructions: %w", err)
	}

	return taskID, instrPath, nil
}

// Start transitions from idle to started, returns phase prompt.
func Start(cfg *config.Config, taskID string) (*TaskState, string, error) {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return nil, "", fmt.Errorf("loading task state: %w", err)
	}

	if ts.State != StateIdle && ts.State != StateCompleted {
		return nil, "", fmt.Errorf("cannot start: task in state %s (expected idle or completed)", ts.State)
	}

	ts.State = StateStarted
	ts.WorkerPID = os.Getpid()
	if err := saveTaskState(dir, ts); err != nil {
		return nil, "", err
	}

	prompt := buildCurrentPhaseInfo(ts)
	return ts, prompt, nil
}

// Next advances to the next phase. Returns new TaskState and prompt, or empty prompt if pipeline done.
func Next(cfg *config.Config, taskID string) (*TaskState, string, error) {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return nil, "", fmt.Errorf("loading task state: %w", err)
	}

	if ts.State != StateCompleted {
		return nil, "", fmt.Errorf("cannot advance: current phase not completed (state: %s)", ts.State)
	}

	// Update manifest
	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest, err := session.Load(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("loading manifest: %w", err)
	}
	manifest.AdvancePhase(ts.Phase)
	if err := manifest.Save(manifestPath); err != nil {
		return nil, "", fmt.Errorf("saving manifest: %w", err)
	}

	// Find next active phase
	phases := phase.DefaultPhases()
	active := phase.ActivePhases(phases, ts.Complexity)

	nextIdx := -1
	for i, p := range active {
		if p.ID > ts.Phase {
			nextIdx = i
			break
		}
	}

	if nextIdx < 0 {
		manifest.MarkCompleted()
		_ = manifest.Save(manifestPath)

		ts.State = StateCompleted
		_ = saveTaskState(dir, ts)
		return ts, "", nil
	}

	nextPhase := active[nextIdx]
	ts.Phase = nextPhase.ID
	ts.PhaseName = nextPhase.Name
	ts.Role = nextPhase.Role
	ts.State = StateStarted
	ts.Reflections = nil
	if err := saveTaskState(dir, ts); err != nil {
		return nil, "", err
	}

	prompt := buildCurrentPhaseInfo(ts)
	return ts, prompt, nil
}

// Heartbeat records a liveness signal.
func Heartbeat(cfg *config.Config, taskID, msg string) error {
	dir := taskDir(cfg, taskID)
	return appendProgress(dir, ProgressEntry{
		Type:   "heartbeat",
		Time:   time.Now().Format(time.RFC3339),
		Msg:    msg,
		TaskID: taskID,
	})
}

// Progress records a percent + message update.
func Progress(cfg *config.Config, taskID string, pct int, msg string) error {
	dir := taskDir(cfg, taskID)

	ts, err := loadTaskState(dir)
	if err == nil && ts.State == StateStarted {
		ts.State = StateWorking
		_ = saveTaskState(dir, ts)
	}

	return appendProgress(dir, ProgressEntry{
		Type:   "progress",
		Time:   time.Now().Format(time.RFC3339),
		Msg:    msg,
		Pct:    pct,
		TaskID: taskID,
	})
}

// Stuck signals the worker is blocked. Writes stuck.yaml and polls for guidance.
func Stuck(cfg *config.Config, taskID, question string, timeout time.Duration) (string, error) {
	dir := taskDir(cfg, taskID)

	ts, err := loadTaskState(dir)
	if err != nil {
		return "", fmt.Errorf("loading task state: %w", err)
	}
	ts.State = StateStuck
	if err := saveTaskState(dir, ts); err != nil {
		return "", err
	}

	signal := StuckSignal{
		Question:  question,
		AskedAt:   time.Now(),
		WorkerPID: os.Getpid(),
	}
	data, err := yaml.Marshal(&signal)
	if err != nil {
		return "", err
	}
	stuckPath := filepath.Join(dir, "stuck.yaml")
	if err := os.WriteFile(stuckPath, data, 0o644); err != nil {
		return "", err
	}

	_ = appendProgress(dir, ProgressEntry{
		Type:   "stuck",
		Time:   time.Now().Format(time.RFC3339),
		Msg:    question,
		TaskID: taskID,
	})

	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	guidancePath := filepath.Join(dir, "guidance.yaml")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(guidancePath); err == nil && len(data) > 0 {
			var g GuidanceResponse
			if err := yaml.Unmarshal(data, &g); err == nil && g.Answer != "" {
				// Clean up
				_ = os.Remove(stuckPath)
				_ = os.Remove(guidancePath)

				ts.State = StateWorking
				_ = saveTaskState(dir, ts)

				return g.Answer, nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	// Timeout — no guidance received
	_ = os.Remove(stuckPath)
	return "", fmt.Errorf("no guidance received within %s", timeout)
}

// Complete signals the current phase is done.
func Complete(cfg *config.Config, taskID string) error {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return fmt.Errorf("loading task state: %w", err)
	}

	// Verify role boundaries
	result := ResultSignal{
		Status:    "completed",
		Phase:     ts.Phase,
		PhaseName: ts.PhaseName,
		DoneAt:    time.Now(),
	}
	data, err := yaml.Marshal(&result)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "result.yaml"), data, 0o644); err != nil {
		return err
	}

	ts.State = StateCompleted
	if err := saveTaskState(dir, ts); err != nil {
		return err
	}

	_ = appendProgress(dir, ProgressEntry{
		Type:   "completed",
		Time:   time.Now().Format(time.RFC3339),
		Msg:    fmt.Sprintf("phase %d (%s) completed", ts.Phase, ts.PhaseName),
		TaskID: taskID,
		Phase:  ts.Phase,
	})

	return nil
}

// Fail signals the current phase failed.
func Fail(cfg *config.Config, taskID, reason string) error {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return fmt.Errorf("loading task state: %w", err)
	}

	result := ResultSignal{
		Status:    "failed",
		Reason:    reason,
		Phase:     ts.Phase,
		PhaseName: ts.PhaseName,
		DoneAt:    time.Now(),
	}
	data, err := yaml.Marshal(&result)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "result.yaml"), data, 0o644); err != nil {
		return err
	}

	ts.State = StateFailed
	if err := saveTaskState(dir, ts); err != nil {
		return err
	}

	_ = appendProgress(dir, ProgressEntry{
		Type:   "failed",
		Time:   time.Now().Format(time.RFC3339),
		Msg:    fmt.Sprintf("phase %d (%s) failed: %s", ts.Phase, ts.PhaseName, reason),
		TaskID: taskID,
		Phase:  ts.Phase,
	})

	return nil
}

// Observe records a meta-cognitive reflection.
func Observe(cfg *config.Config, taskID, note string) error {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return fmt.Errorf("loading task state: %w", err)
	}

	ts.Reflections = append(ts.Reflections, note)
	if err := saveTaskState(dir, ts); err != nil {
		return err
	}

	_ = appendProgress(dir, ProgressEntry{
		Type:   "observation",
		Time:   time.Now().Format(time.RFC3339),
		Msg:    note,
		TaskID: taskID,
		Phase:  ts.Phase,
	})

	return nil
}

// Context returns current task context for the worker.
func Context(cfg *config.Config, taskID string) (string, error) {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return "", fmt.Errorf("loading task state: %w", err)
	}

	var out string
	out += fmt.Sprintf("Task: %s\n", ts.TaskID)
	out += fmt.Sprintf("Phase: %d (%s)\n", ts.Phase, ts.PhaseName)
	out += fmt.Sprintf("Role: %s\n", ts.Role)
	out += fmt.Sprintf("State: %s\n", ts.State)
	out += fmt.Sprintf("Complexity: %s\n", ts.Complexity)

	if desc := phase.PhaseDescription(ts.PhaseName); desc != "" {
		out += fmt.Sprintf("\nObjective: %s\n", desc)
	}
	if boundary := role.BoundaryText(ts.Role); boundary != "" {
		out += fmt.Sprintf("\nBoundary: %s\n", boundary)
	}

	if len(ts.Reflections) > 0 {
		out += "\nReflections:\n"
		for _, r := range ts.Reflections {
			out += fmt.Sprintf("  - %s\n", r)
		}
	}

	// Include scratchpad if exists
	scratchPath := filepath.Join(dir, "scratchpad.md")
	if data, err := os.ReadFile(scratchPath); err == nil && len(data) > 0 {
		out += "\n[SCRATCHPAD]\n" + string(data) + "\n"
	}

	return out, nil
}

// Retry resets current phase for another attempt. Increments L1 counter in manifest.
func Retry(cfg *config.Config, taskID string) (*TaskState, string, error) {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return nil, "", fmt.Errorf("loading task state: %w", err)
	}

	if ts.State != StateFailed {
		return nil, "", fmt.Errorf("cannot retry: task in state %s (expected failed)", ts.State)
	}

	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest, err := session.Load(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("loading manifest: %w", err)
	}

	manifest.RecordL1Retry()
	if err := manifest.Save(manifestPath); err != nil {
		return nil, "", fmt.Errorf("saving manifest: %w", err)
	}

	// Clean up previous result
	_ = os.Remove(filepath.Join(dir, "result.yaml"))

	ts.State = StateStarted
	ts.Reflections = nil
	if err := saveTaskState(dir, ts); err != nil {
		return nil, "", err
	}

	prompt := buildCurrentPhaseInfo(ts)

	// Include scratchpad from previous attempts
	scratchpad := phase.NewScratchpad(cfg.TaskDir, taskID)
	if content := scratchpad.ForPrompt(); content != "" {
		prompt += "\n" + content + "\n"
	}

	return ts, prompt, nil
}

// Loopback jumps back to a target phase (L2 cycle). Increments L2 counter in manifest.
func Loopback(cfg *config.Config, taskID string, targetPhase int) (*TaskState, string, error) {
	if targetPhase != phase.L2StartPhase {
		return nil, "", fmt.Errorf("invalid loopback target: phase %d (only phase %d allowed)", targetPhase, phase.L2StartPhase)
	}

	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return nil, "", fmt.Errorf("loading task state: %w", err)
	}

	if !phase.IsVerificationPhase(ts.Phase) {
		return nil, "", fmt.Errorf("loopback only allowed from verification phases (current: phase %d %s)", ts.Phase, ts.PhaseName)
	}

	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest, err := session.Load(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("loading manifest: %w", err)
	}

	manifest.RecordL2Cycle()
	manifest.LoopBackTo(targetPhase)
	if err := manifest.Save(manifestPath); err != nil {
		return nil, "", fmt.Errorf("saving manifest: %w", err)
	}

	// Clean up previous result
	_ = os.Remove(filepath.Join(dir, "result.yaml"))

	// Find target phase info
	phases := phase.DefaultPhases()
	var targetPh phase.Phase
	for _, p := range phases {
		if p.ID == targetPhase {
			targetPh = p
			break
		}
	}

	ts.Phase = targetPh.ID
	ts.PhaseName = targetPh.Name
	ts.Role = targetPh.Role
	ts.State = StateStarted
	ts.Reflections = nil
	if err := saveTaskState(dir, ts); err != nil {
		return nil, "", err
	}

	prompt := buildCurrentPhaseInfo(ts)
	return ts, prompt, nil
}

// Status returns formatted summary of task state and budget.
func Status(cfg *config.Config, taskID string) (string, error) {
	dir := taskDir(cfg, taskID)
	ts, err := loadTaskState(dir)
	if err != nil {
		return "", fmt.Errorf("loading task state: %w", err)
	}

	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest, err := session.Load(manifestPath)
	if err != nil {
		return "", fmt.Errorf("loading manifest: %w", err)
	}

	maxL1 := cfg.PhaseMachine.MaxL1Retries
	if maxL1 <= 0 {
		maxL1 = 2
	}
	maxL2 := cfg.PhaseMachine.MaxL2Cycles
	if maxL2 <= 0 {
		maxL2 = 3
	}

	var out string
	out += fmt.Sprintf("Task: %s\n", ts.TaskID)
	out += fmt.Sprintf("Phase: %d (%s)\n", ts.Phase, ts.PhaseName)
	out += fmt.Sprintf("Role: %s\n", ts.Role)
	out += fmt.Sprintf("State: %s\n", ts.State)
	out += fmt.Sprintf("Complexity: %s\n", ts.Complexity)
	out += fmt.Sprintf("Pipeline: %s\n", manifest.Status)
	out += fmt.Sprintf("\nBudget:\n")
	out += fmt.Sprintf("  L1 retries: %d/%d used (%d remaining)\n",
		manifest.Budget.L1RetriesTotal, maxL1, manifest.RemainingL1Retries(maxL1))
	out += fmt.Sprintf("  L2 cycles:  %d/%d used (%d remaining)\n",
		manifest.Budget.L2CyclesTotal, maxL2, manifest.RemainingL2Cycles(maxL2))
	out += fmt.Sprintf("  Phases completed: %v\n", manifest.Pipeline.PhasesCompleted)
	out += fmt.Sprintf("  Phases skipped: %v\n", manifest.Pipeline.PhasesSkipped)

	return out, nil
}

// EmitEvent sends a worker event to the event log.
func EmitEvent(cfg *config.Config, taskID, eventType string, data map[string]interface{}) {
	emitter, err := events.NewEmitter(cfg.EventLog)
	if err != nil {
		return
	}
	_ = emitter.TaskEvent(taskID, "", "", "baton-worker", eventType, data)
}

func buildCurrentPhaseInfo(ts *TaskState) string {
	var out string
	out += fmt.Sprintf("=== PHASE: %s (#%d) ===\n", ts.PhaseName, ts.Phase)
	out += fmt.Sprintf("Role: %s\n", ts.Role)

	if desc := phase.PhaseDescription(ts.PhaseName); desc != "" {
		out += fmt.Sprintf("Objective: %s\n", desc)
	}
	if rdesc := phase.RoleDescription(ts.Role); rdesc != "" {
		out += fmt.Sprintf("\n%s\n", rdesc)
	}
	if boundary := role.BoundaryText(ts.Role); boundary != "" {
		out += fmt.Sprintf("\n%s\n", boundary)
	}

	tools := role.AllowedTools(ts.Role)
	if len(tools) > 0 {
		out += fmt.Sprintf("\nAllowed tools: %v\n", tools)
	}

	reflections := PhaseReflections(ts.Phase, ts.PhaseName)
	if len(reflections) > 0 {
		out += "\n[MANDATORY EXIT QUESTIONS]\n"
		out += "Before completing this phase, record observations for each:\n"
		for i, q := range reflections {
			out += fmt.Sprintf("  %d. %s\n", i+1, q)
		}
	}

	return out
}

func appendProgress(dir string, entry ProgressEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "progress.ndjson"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
