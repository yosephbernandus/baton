# Technical Engineering Design: Baton

**Version:** 1.0
**Date:** 2026-04-26
**Status:** Draft
**Authors:** Project author

---

## 1. Overview

Baton is a lightweight, runtime-agnostic multi-agent orchestrator. It enables a single orchestrator (Claude Code, OpenCode, pi-agent, or a human) to delegate coding tasks to multiple external agents running different models, with structured context preservation and real-time monitoring.

**One sentence:** Baton is the bridge between an AI orchestrator that plans and cheap AI workers that implement.

### 1.1 Problem Statement

Three pain points when working with multiple AI coding agents:

1. **Cognitive overload.** Running multiple agents in separate terminals forces the human to become the integration layer — tracking state, merging context, checking for conflicts.
2. **Cost inefficiency.** Expensive models (Opus) are used for tasks that cheap models (Kimi, DeepSeek) handle fine. Claude Code only supports Anthropic models; OpenCode only supports its own ecosystem. No way to mix and match.
3. **Context degradation.** When delegating across agent boundaries, critical context (why, constraints, related work) is lost at each hop. Workers make technically correct but contextually wrong decisions.

### 1.2 Goals

- Delegate coding tasks from any orchestrator to any agent runtime via a single CLI
- Preserve 75-85% of context at the worker level through structured task specs
- Detect worker confusion and escalate rather than let workers guess
- Prevent parallel file conflicts through advisory locks
- Provide real-time visibility via terminal UI
- Add new runtimes via config, not code changes
- Ship as a single Go binary with <5ms startup

### 1.3 Non-Goals

- **Not an orchestrator.** Baton does not plan, reason, or make decisions. It is a bridge.
- **Not a model router.** Routing rules are advisory, read by the orchestrator. Baton does not decide which model handles what.
- **Not distributed.** Single machine only. No remote worker execution.
- **Not a framework.** No SDK, no client libraries, no abstractions beyond CLI + YAML.
- **Not interactive mid-task.** Workers cannot negotiate with the orchestrator during execution. They exit and get re-spawned.

---

## 2. System Architecture

### 2.1 Component Diagram

```
Human
  |
  v
Orchestrator (Claude Code / OpenCode / human shell)
  |  bash commands
  v
baton CLI (single Go binary)
  |
  +-- Config Loader -----> agents.yaml
  +-- Spec Validator ----> .baton/specs/*.yaml
  +-- Prompt Builder ----> project-brief.md + spec -> prompt string
  +-- Task Manager ------> .baton/tasks/*.yaml (CRUD)
  +-- Lock Registry -----> .baton/locks.yaml
  +-- Runner ------------> subprocess lifecycle (spawn, capture, timeout, kill)
  +-- Acceptance Checker -> runs post-task commands from spec
  +-- Escalation Detector > pattern matching on worker stdout
  +-- Event Emitter -----> .baton/events.ndjson (NDJSON append)
  +-- TUI Renderer ------> bubbletea monitor (reads event log via fsnotify)
  |
  +-- spawns subprocesses
  |
  v
Workers: opencode, pi-agent, aider (external binaries)
```

### 2.2 Data Flow

```
1. Orchestrator calls: baton run --runtime opencode --model kimi --spec task.yaml
2. Baton loads config (agents.yaml), validates runtime+model
3. Baton loads and validates spec (required fields, context_files exist on disk)
4. Baton checks writes_to against lock registry
5. Baton acquires file locks
6. Baton creates task record (.baton/tasks/{id}.yaml)
7. Baton constructs prompt: project-brief + spec fields + clarification instruction
8. Baton takes git snapshot (if in git repo)
9. Baton spawns: opencode -m kimi -p "<prompt>" --files <context_files>
10. Baton streams stdout/stderr to event log, scans for clarification patterns
11. Worker exits
12. Baton runs acceptance_checks (if defined in spec)
13. Baton determines status: completed | failed | needs_clarification | timeout
14. Baton updates task record, detects file changes via git diff
15. Baton releases file locks
16. Baton emits final event, prints result to stdout
```

### 2.3 Directory Structure

```
baton/
|-- main.go                     # entrypoint, cobra root command
|-- go.mod
|-- go.sum
|
|-- cmd/
|   |-- run.go                  # baton run
|   |-- status.go               # baton status
|   |-- list.go                 # baton list
|   |-- result.go               # baton result
|   |-- monitor.go              # baton monitor
|   +-- config.go               # baton config
|
+-- internal/
    |-- config/config.go        # YAML config parsing, validation, precedence
    |-- spec/spec.go            # Task spec parsing, validation, prompt building
    |-- runner/runner.go        # Subprocess lifecycle, timeout, output capture
    |-- task/task.go            # Task record CRUD, attempt tracking, escalation
    |-- lock/lock.go            # File lock registry, prefix locks, stale cleanup
    |-- brief/brief.go          # Project brief loader
    |-- events/events.go        # NDJSON emitter + tailer with fsnotify
    +-- tui/
        |-- tui.go              # bubbletea model/update/view
        |-- taskview.go         # task table component
        |-- outputview.go       # output panel with ring buffer
        +-- escalation.go       # escalation detail view
```

### 2.4 Runtime Data Directory

```
.baton/
|-- agents.yaml                 # project-level config (overrides ~/.baton/agents.yaml)
|-- project-brief.md            # prepended to all worker prompts
|-- specs/                      # task spec YAML files (written by orchestrator/architect)
|   |-- task-001.yaml
|   +-- task-002.yaml
|-- tasks/                      # task records (managed by baton)
|   |-- task-001.yaml
|   +-- task-002.yaml
|-- results/                    # worker output captures
|-- locks.yaml                  # file lock registry
+-- events.ndjson               # append-only event log
```

---

## 3. Detailed Component Design

### 3.1 Config Loader (`internal/config/config.go`)

**Responsibility:** Parse and validate `agents.yaml` with precedence rules.

**Precedence:** `./.baton/agents.yaml` (project) > `~/.baton/agents.yaml` (user) > built-in defaults.

**Data structures:**

```go
type Config struct {
    Orchestrator   OrchestratorConfig          `yaml:"orchestrator"`
    Runtimes       map[string]RuntimeConfig    `yaml:"runtimes"`
    Defaults       DefaultsConfig              `yaml:"defaults"`
    Routing        RoutingConfig               `yaml:"routing"`
    ClarifyPatterns []string                   `yaml:"clarification_patterns"`
    ClarifyExit    int                         `yaml:"clarification_exit_code"`
    EventLog       string                      `yaml:"event_log"`
    TaskDir        string                      `yaml:"task_dir"`
    ResultDir      string                      `yaml:"result_dir"`
    SpecDir        string                      `yaml:"spec_dir"`
    LockFile       string                      `yaml:"lock_file"`
    ProjectBrief   string                      `yaml:"project_brief"`
    LogMaxSizeMB   int                         `yaml:"log_max_size_mb"`
    LogKeepCount   int                         `yaml:"log_keep_count"`
    DefaultTimeout string                      `yaml:"default_timeout"`
}

type RuntimeConfig struct {
    Command     string   `yaml:"command"`
    ModelFlag   string   `yaml:"model_flag"`
    PromptFlag  string   `yaml:"prompt_flag"`
    ContextFlag string   `yaml:"context_flag"`
    ExtraFlags  []string `yaml:"extra_flags"`
    Workdir     string   `yaml:"workdir"`
    Models      []string `yaml:"models"`
}
```

**Key functions:**
- `LoadConfig() (*Config, error)` — loads with precedence, merges, validates
- `ValidateRuntime(name, model string) error` — checks runtime exists and model is in its list
- `RuntimeAvailable(name string) bool` — checks if command is in PATH via `exec.LookPath`

### 3.2 Spec Validator (`internal/spec/spec.go`)

**Responsibility:** Parse task spec YAML, validate required fields, verify context_files exist on disk, build prompt string from spec + project brief.

**Data structures:**

```go
type Spec struct {
    What               string          `yaml:"what"`
    Why                string          `yaml:"why"`
    Constraints        []string        `yaml:"constraints"`
    ContextFiles       []string        `yaml:"context_files"`
    RelatedTasks       []RelatedTask   `yaml:"related_tasks"`
    AcceptanceCriteria []string        `yaml:"acceptance_criteria"`
    Decisions          []Decision      `yaml:"decisions"`
    WritesTo           []string        `yaml:"writes_to"`
    Examples           []Example       `yaml:"examples"`
    AcceptanceChecks   []Check         `yaml:"acceptance_checks"`
    Criticality        string          `yaml:"criticality"`
}

type RelatedTask struct {
    TaskID          string `yaml:"task_id"`
    Status          string `yaml:"status"`
    Summary         string `yaml:"summary"`
    RelevantOutput  string `yaml:"relevant_output"`
}

type Decision struct {
    Question  string `yaml:"question"`
    Answer    string `yaml:"answer"`
    Reason    string `yaml:"reason"`
    DecidedBy string `yaml:"decided_by"`
}

type Check struct {
    Command     string `yaml:"command"`
    ExpectExit  int    `yaml:"expect_exit"`
    Description string `yaml:"description"`
}
```

**Validation rules:**
- `what` non-empty
- `why` non-empty
- `constraints` must be present (can be empty slice)
- `context_files` each must exist on disk (`os.Stat`)
- `acceptance_criteria` at least one item
- `writes_to` validated against lock registry (delegated to lock package)
- `criticality` if present, must be `low | medium | high`
- `acceptance_checks` commands must be non-empty strings

**Key functions:**
- `LoadSpec(path string) (*Spec, error)` — parse YAML, return structured spec
- `Validate(spec *Spec) []ValidationError` — returns all errors, not just first
- `BuildPrompt(spec *Spec, brief string) string` — concatenates into worker prompt

**Prompt template:**

```
[PROJECT CONTEXT]
{brief}

[TASK]
{spec.What}

[WHY THIS MATTERS]
{spec.Why}

[CONSTRAINTS]
- {each constraint}

[RELATED TASKS — DO NOT CONFLICT]
- {task_id} ({status}): {summary}
  Output: {relevant_output}

[ACCEPTANCE CRITERIA]
- {each criterion}

[DECISIONS ALREADY MADE]
- Q: {question} -> A: {answer} (reason: {reason}, decided by: {decided_by})

[EXAMPLES]
## {description}
{code}

[IMPORTANT]
If you are uncertain about any aspect of this task, output the line:
CLARIFICATION_NEEDED: <your question here>
and exit. Do NOT guess.
```

### 3.3 Task Runner (`internal/runner/runner.go`)

**Responsibility:** Subprocess lifecycle — construct command, spawn, capture stdout/stderr, enforce timeout, detect clarification, run acceptance checks, detect file changes.

**Subprocess construction:**

```go
func buildCommand(cfg *config.RuntimeConfig, model, prompt string, contextFiles []string) *exec.Cmd {
    args := []string{}
    args = append(args, cfg.ModelFlag, model)
    args = append(args, cfg.PromptFlag, prompt)
    if cfg.ContextFlag != "" && len(contextFiles) > 0 {
        args = append(args, cfg.ContextFlag, strings.Join(contextFiles, ","))
    }
    args = append(args, cfg.ExtraFlags...)

    cmd := exec.CommandContext(ctx, cfg.Command, args...)
    cmd.Dir = projectRoot
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    return cmd
}
```

**Timeout handling:**
- `context.WithTimeout` wrapping the subprocess
- On timeout: `SIGTERM` to process group, wait 5 seconds, `SIGKILL`
- Task status set to `timeout`, exit code 124

**Output capture:**
- `cmd.StdoutPipe()` + `bufio.Scanner` for line-by-line streaming
- Each line emitted as an `output` event to the NDJSON log
- Lines scanned against clarification patterns concurrently

**Clarification detection:**
- Scan each stdout line against configured patterns
- If any pattern matches AND exit code is non-zero (or matches `clarification_exit_code`): `needs_clarification`
- Extract the `CLARIFICATION_NEEDED: <question>` line if present

**Acceptance checks:**
- After worker exits successfully (or `needs_clarification` detected), run each check command
- `exec.Command("sh", "-c", check.Command)` with short timeout (30s)
- If any check exits with code != `expect_exit`: task status = `failed`, record which check broke
- Emit `acceptance_check_passed` / `acceptance_check_failed` events

**Git integration:**
```go
// Before spawn
beforeFiles = gitDiffNameOnly() + gitUntrackedFiles()

// After completion
afterFiles = gitDiffNameOnly() + gitUntrackedFiles()
changedFiles = diff(afterFiles, beforeFiles)
```

### 3.4 Task Manager (`internal/task/task.go`)

**Responsibility:** CRUD for task records in `.baton/tasks/`. Atomic writes via temp file + rename.

**Data structures:**

```go
type Task struct {
    ID            string       `yaml:"id"`
    Runtime       string       `yaml:"runtime"`
    Model         string       `yaml:"model"`
    Status        string       `yaml:"status"`
    DispatchedBy  string       `yaml:"dispatched_by"`
    Spec          *spec.Spec   `yaml:"spec"`
    Escalation    Escalation   `yaml:"escalation"`
    Attempts      []Attempt    `yaml:"attempts"`
    CreatedAt     time.Time    `yaml:"created_at"`
    StartedAt     *time.Time   `yaml:"started_at"`
    CompletedAt   *time.Time   `yaml:"completed_at"`
    Duration      string       `yaml:"duration"`
    PID           int          `yaml:"pid"`
    ExitCode      *int         `yaml:"exit_code"`
    FilesChanged  []string     `yaml:"files_changed"`
    Error         string       `yaml:"error"`
}

type Escalation struct {
    WorkerClarification  string `yaml:"worker_clarification"`
    OrchestratorAnalysis string `yaml:"orchestrator_analysis"`
    HumanDecision        string `yaml:"human_decision"`
    HumanReason          string `yaml:"human_reason"`
}

type Attempt struct {
    Attempt     int        `yaml:"attempt"`
    StartedAt   time.Time  `yaml:"started_at"`
    CompletedAt *time.Time `yaml:"completed_at"`
    Status      string     `yaml:"status"`
}
```

**Task states:** `pending` -> `running` -> `completed | failed | timeout | needs_clarification` -> (optional) `re_delegated` -> `pending`

**Key functions:**
- `Create(t *Task) error` — write to `.baton/tasks/{id}.yaml`
- `Update(t *Task) error` — atomic temp+rename write
- `Get(id string) (*Task, error)` — read and parse
- `List(filter string) ([]*Task, error)` — scan directory, optional status filter
- `AddAttempt(id string, a Attempt) error` — append attempt to task record

**Atomic writes:** Write to `.baton/tasks/{id}.yaml.tmp`, then `os.Rename` to final path. Prevents partial reads.

### 3.5 Lock Registry (`internal/lock/lock.go`)

**Responsibility:** Manage advisory file locks in `.baton/locks.yaml`. Prevent conflicting parallel tasks.

**Data structures:**

```go
type LockRegistry struct {
    Locks map[string]Lock `yaml:"locks"`
}

type Lock struct {
    HeldBy string    `yaml:"held_by"`
    Type   string    `yaml:"type"` // "file" or "prefix"
    Since  time.Time `yaml:"since"`
}
```

**Key functions:**
- `Check(paths []string) ([]Conflict, error)` — returns conflicting locks without acquiring
- `Acquire(taskID string, paths []string) error` — acquires locks for all paths or none (atomic)
- `Release(taskID string) error` — releases all locks held by task
- `CleanStale() error` — on startup, check if holding PIDs are alive, release dead locks

**Prefix lock matching:** Path `migrations/` locks any path that starts with `migrations/`. Checked via `strings.HasPrefix`.

**Atomicity:** Acquire is all-or-nothing. If any path conflicts, none are acquired. Prevents partial lock states.

### 3.6 Event Emitter (`internal/events/events.go`)

**Responsibility:** Append NDJSON events to `.baton/events.ndjson`. Provide tailing for the TUI monitor.

**Data structures:**

```go
type Event struct {
    Timestamp    time.Time              `json:"ts"`
    TaskID       string                 `json:"task_id"`
    Runtime      string                 `json:"runtime"`
    Model        string                 `json:"model"`
    DispatchedBy string                 `json:"dispatched_by"`
    EventType    string                 `json:"event"`
    Data         map[string]interface{} `json:"data"`
}
```

**Key functions:**
- `Emit(e Event) error` — JSON marshal, append line with flock
- `Tail(ctx context.Context) <-chan Event` — returns channel of new events, uses fsnotify to watch file
- `Rotate() error` — if file > MaxSize, rename to `.1`, shift older files, create new

**Concurrency:** `Emit` uses `syscall.Flock` (or `sync.Mutex` fallback) to prevent interleaved writes from parallel tasks.

### 3.7 TUI Monitor (`internal/tui/`)

**Responsibility:** Full-screen terminal UI showing task status, live output, and escalation banners.

**Framework:** bubbletea + lipgloss

**Components:**
- `tui.go` — Main bubbletea model. Subscribes to event tailer. Dispatches to sub-components.
- `taskview.go` — Task table. Columns: Task ID, Runtime, Model, Status, Duration. Color-coded status. Spinner for running tasks.
- `outputview.go` — Output panel for selected task. Ring buffer of last N lines. Auto-scroll for running, manual scroll for completed.
- `escalation.go` — Escalation detail view (triggered by `[e]` key). Shows worker question, orchestrator analysis, human decision fields.

**Color scheme:**
- Running: yellow
- Completed: green
- Failed: red
- Timeout: orange
- Needs clarification: cyan
- Needs human: magenta

**Keyboard:**
- `up/down` — select task
- `enter` — view output
- `e` — view escalation details
- `q` — quit

**Data source:** Reads `.baton/events.ndjson` via the event tailer. Builds in-memory task state from events. No direct file reads of task records.

---

## 4. API Design (CLI Interface)

### 4.1 baton run

Spawns a task in an external runtime.

```
baton run --runtime <runtime> --model <model> [flags]

Flags:
  --spec <path>          Task spec YAML file
  --prompt <string>      Inline prompt (requires --skip-validation)
  --task-id <string>     Task ID (default: task-{timestamp})
  --context-files <csv>  Context files (only with --prompt)
  --skip-validation      Skip spec validation for inline prompts
  --background           Return immediately (default: false)
  --timeout <duration>   Max time before kill (default: 10m)

Exit codes:
  0   Completed successfully
  1   Failed (worker error)
  2   Configuration error
  3   Spec validation error
  4   Lock conflict
  5   Acceptance check failed
  10  Needs clarification
  124 Timed out
```

### 4.2 baton status

```
baton status [--json] [--filter <status>]

Output:
  TASK ID     RUNTIME     MODEL      STATUS              DURATION
  task-001    opencode    kimi       completed           2m15s
  task-002    opencode    kimi       needs human         -
  task-003    pi-agent    gemini     running             1m03s
```

### 4.3 baton list

```
baton list [--json]

Output:
  RUNTIME     MODELS                              STATUS
  opencode    kimi, deepseek, gemini-flash        available
  pi-agent    gemini, grok                        available
  aider       gpt-4o, deepseek, claude-sonnet     not found
```

### 4.4 baton result

```
baton result <task-id> [--json] [--files-only] [--clarification] [--escalation]
```

### 4.5 baton monitor

```
baton monitor
```

Launches interactive TUI. Reads event log, builds in-memory state, streams updates.

### 4.6 baton config

```
baton config get <key>
baton config set <key> <value>
```

---

## 5. Data Models

### 5.1 Task Spec (input, written by orchestrator/architect)

```yaml
spec:
  what: string          # required
  why: string           # required
  constraints: [string] # required (can be empty)
  context_files: [string] # required (validated on disk)
  acceptance_criteria: [string] # required (min 1)
  related_tasks:        # optional
    - task_id: string
      status: string
      summary: string
      relevant_output: string
  decisions:            # optional
    - question: string
      answer: string
      reason: string
      decided_by: string
  writes_to: [string]   # optional
  examples:             # optional
    - description: string
      code: string
  acceptance_checks:    # optional
    - command: string
      expect_exit: int
      description: string
  criticality: string   # optional (low|medium|high)
```

### 5.2 Task Record (managed by baton)

```yaml
id: string
runtime: string
model: string
status: string          # pending|running|completed|failed|timeout|needs_clarification|needs_human
dispatched_by: string
spec: <embedded spec>
escalation:
  worker_clarification: string
  orchestrator_analysis: string
  human_decision: string
  human_reason: string
attempts:
  - attempt: int
    started_at: timestamp
    completed_at: timestamp
    status: string
created_at: timestamp
started_at: timestamp
completed_at: timestamp
duration: string
pid: int
exit_code: int
files_changed: [string]
error: string
```

### 5.3 Event Log Entry

```json
{
  "ts": "RFC3339 timestamp",
  "task_id": "string",
  "runtime": "string",
  "model": "string",
  "dispatched_by": "string",
  "event": "event_type",
  "data": {}
}
```

### 5.4 Lock Registry

```yaml
locks:
  <path>:
    held_by: <task_id>
    type: file|prefix
    since: timestamp
```

---

## 6. Error Handling

| Scenario | Behavior | Exit Code |
|---|---|---|
| Runtime not in PATH | Error with install suggestion | 2 |
| Model not in runtime's list | Error with available models | 2 |
| Spec missing required fields | Error listing specific missing fields | 3 |
| context_files not on disk | Error with missing file paths | 3 |
| Lock conflict | Error with conflicting task ID and duration | 4 |
| Worker crashes | Check clarification patterns first, then mark failed | 1 |
| Worker timeout | SIGTERM, wait 5s, SIGKILL. Mark timeout. | 124 |
| Acceptance check fails | Mark failed, record which check broke | 5 |
| Clarification detected | Mark needs_clarification, capture question | 10 |
| Baton crash recovery | On startup: scan stale running tasks (dead PIDs), mark failed, release orphaned locks | — |

---

## 7. Concurrency Model

**Parallel task execution:** Baton itself runs one task per `baton run` invocation. The orchestrator can run multiple `baton run` commands simultaneously. Concurrency is at the process level, not within Baton.

**Event log writes:** Serialized via `syscall.Flock` on the event log file. Each write is one JSON line + newline, atomic at the OS level for small writes (<4KB, guaranteed by POSIX).

**Task record writes:** Each task has its own file. No cross-task contention. Writes are temp+rename for atomicity.

**Lock registry:** Read-modify-write cycle on `.baton/locks.yaml` protected by flock. Short critical section (read YAML, check/update, write YAML).

**TUI monitor:** Single goroutine reads event channel from tailer. No shared mutable state with the emitter. Monitor is read-only.

---

## 8. Testing Strategy

### 8.1 Unit Tests

| Package | What to Test |
|---|---|
| `config` | YAML parsing, precedence merging, runtime validation, model validation |
| `spec` | Required field validation, context_file existence check, prompt building |
| `task` | CRUD operations, atomic writes, status transitions, attempt tracking |
| `lock` | Acquire/release, prefix matching, conflict detection, stale cleanup |
| `events` | NDJSON marshaling, rotation trigger, event type coverage |
| `runner` | Command construction from runtime config, clarification pattern matching |

### 8.2 Integration Tests

- **Full delegation cycle:** `baton run` with a mock runtime (shell script that echoes and exits). Verify task record, event log, result capture.
- **Clarification detection:** Mock runtime that outputs clarification patterns. Verify `needs_clarification` status.
- **Acceptance checks:** Mock runtime that produces output. Verify checks run and status reflects pass/fail.
- **Lock conflicts:** Two `baton run` calls with overlapping `writes_to`. Verify second gets exit code 4.
- **Timeout:** Mock runtime that sleeps forever. Verify SIGTERM, SIGKILL, timeout status.

### 8.3 Mock Runtime

A shell script that simulates an agent runtime for testing:

```bash
#!/bin/bash
# mock-runtime.sh
# Accepts same flags as real runtimes
while getopts "m:p:" opt; do
  case $opt in
    m) MODEL=$OPTARG ;;
    p) PROMPT=$OPTARG ;;
  esac
done

echo "Mock runtime processing: $PROMPT"
echo "Model: $MODEL"

# Simulate behavior based on prompt content
if echo "$PROMPT" | grep -q "FORCE_CLARIFY"; then
  echo "CLARIFICATION_NEEDED: Which schema version?"
  exit 10
fi

if echo "$PROMPT" | grep -q "FORCE_FAIL"; then
  exit 1
fi

echo "Done."
exit 0
```

---

## 9. Build Phases

### Phase 1: Core CLI + Spec System (MVP)

**Scope:**
- `internal/config` — config loading, runtime validation
- `internal/spec` — spec parsing, validation, prompt building
- `internal/brief` — project brief loader
- `internal/runner` — subprocess spawn, output capture, clarification detection
- `internal/task` — task record CRUD
- `internal/events` — NDJSON event emitter (no tailer yet)
- `cmd/run` — `baton run` foreground execution
- `cmd/status` — `baton status` table output
- `cmd/list` — `baton list` with runtime availability
- `cmd/result` — `baton result` with `--clarification`

**Target runtime:** OpenCode only.

**Deliverable:** Orchestrator can delegate via structured spec, baton validates, spawns worker, prepends project brief, detects confusion, captures results.

**Estimated size:** ~2,000 lines Go

### Phase 2: Quality Gates

**Scope:**
- `internal/lock` — file lock registry
- `writes_to` validation in spec
- `acceptance_checks` execution after worker exit
- `examples` field in prompt builder
- `criticality` field in task records

**Deliverable:** Parallel tasks cannot conflict on files. Obvious failures caught automatically.

**Estimated size:** ~800 lines Go

### Phase 3: TUI Monitor

**Scope:**
- `internal/events` — add tailer with fsnotify
- `internal/tui` — bubbletea monitor (task table, output panel, escalation banner)
- `cmd/monitor` — `baton monitor` command

**Deliverable:** Real-time visibility from a tmux pane.

**Estimated size:** ~1,200 lines Go

### Phase 4: Multi-Runtime + Orchestrator Agnostic

**Scope:**
- Additional runtime adapters (pi-agent, aider) in config
- `cmd/config` — `baton config get/set`
- `dispatched_by` tracking in events and tasks
- Routing rules in config

**Deliverable:** Any runtime as orchestrator, any runtime as worker.

**Estimated size:** ~500 lines Go

### Phase 5: Advanced Features

**Scope:**
- Background execution (detached processes)
- Git integration for file change detection
- Timeout with graceful shutdown (SIGTERM -> wait -> SIGKILL)
- Log rotation
- `--json` output for all commands
- Stale task cleanup on startup
- Attempt tracking and retry history

**Estimated size:** ~1,000 lines Go

### Phase 6: Smart Features (Optional)

**Scope:**
- Automatic runtime+model selection from routing rules
- Cost tracking per task
- Decision persistence to project docs
- Coherence checkpoint prompt generation

**Estimated size:** ~800 lines Go

---

## 10. Security Considerations

| Concern | Mitigation |
|---|---|
| **API keys in environment** | Baton inherits current env and passes to workers. Does not log, store, or transmit API keys. Keys visible in process table (`/proc/{pid}/environ`) — same risk as running agents directly. |
| **Command injection via spec fields** | Spec fields are passed as CLI flag values, not interpolated into shell commands. Acceptance checks use `sh -c` — commands come from the spec author (trusted orchestrator), not external input. |
| **Malicious worker output** | Workers run in the project directory with user permissions. A compromised model could write malicious code. Mitigated by acceptance checks and orchestrator review, not by sandboxing. |
| **Lock file tampering** | `.baton/locks.yaml` is advisory. A malicious process could delete it. Not a security boundary — it prevents accidents, not attacks. |
| **Event log injection** | Workers write to stdout, which Baton captures as events. A worker could emit misleading event data. The event log is append-only and timestamped — forensically recoverable. |

---

## 11. Performance Considerations

| Metric | Target | Rationale |
|---|---|---|
| Baton startup time | <5ms | Invoked per-task. Must not add perceptible delay. |
| Spec validation | <10ms | File stat for context_files is the bottleneck. |
| Lock check/acquire | <5ms | Single YAML file read/write. |
| Event emit | <1ms | One JSON line append with flock. |
| TUI refresh | <16ms (60fps) | bubbletea handles rendering. fsnotify delivers events at filesystem latency. |
| Max concurrent tasks | ~50 | Limited by filesystem watching and process table. Target use case is 5-15. |
| Event log rotation | 10MB | Prevents unbounded growth. Configurable. |

---

## 12. Dependencies

| Dependency | Version | Purpose | License |
|---|---|---|---|
| `github.com/spf13/cobra` | latest | CLI command framework (see ADR-010 for future removal) | Apache 2.0 |
| `github.com/charmbracelet/bubbletea` | latest | TUI framework | MIT |
| `github.com/charmbracelet/lipgloss` | latest | TUI styling | MIT |
| `github.com/fsnotify/fsnotify` | latest | File watching for event tailing | BSD-3 |
| `gopkg.in/yaml.v3` | latest | YAML parsing | Apache 2.0 / MIT |

No other runtime dependencies. Total transitive dependency count expected: <20.

---

## 13. Open Questions

| # | Question | Impact | Default if Unresolved |
|---|---|---|---|
| 1 | Should `context_flag` support per-runtime formatting (comma-separated vs. repeated flags vs. @file)? | Phase 1 — affects runtime adapter | Comma-separated for v1. Add `context_format` field if needed. |
| 2 | Should acceptance checks run on `needs_clarification` tasks or only on exit code 0? | Phase 2 — affects check runner | Only on exit code 0. Clarification means the task didn't complete. |
| 3 | How should the TUI handle >20 concurrent tasks? | Phase 3 — affects TUI layout | Scrollable table. Unlikely to hit this in practice. |
| 4 | Should Baton support piping spec content via stdin? | Phase 1 — affects CLI interface | No. File-based specs are debuggable. Stdin is a v2 consideration. |
| 5 | Should lock acquisition retry with backoff or fail immediately? | Phase 2 — affects UX | Fail immediately. The orchestrator decides retry strategy. |

---

## 14. Glossary

| Term | Definition |
|---|---|
| **Orchestrator** | The agent (or human) that plans tasks and calls `baton run`. Does not write code. |
| **Worker** | The agent runtime (OpenCode, aider, pi-agent) that receives a task and writes code. |
| **Architect** | The high-tier model (Opus) that produces task specs during planning sessions. |
| **Runtime** | An external CLI tool that hosts an AI model (e.g., opencode, aider, pi-agent). |
| **Spec** | A structured YAML file describing a task with what, why, constraints, and acceptance criteria. |
| **Escalation** | The process of passing an unresolved question up the chain: worker -> orchestrator -> human. |
| **Coherence checkpoint** | A periodic review by the orchestrator to verify completed tasks work together without conflicts. |
| **Advisory lock** | A lock that prevents Baton from spawning conflicting tasks, but does not enforce filesystem-level restrictions on workers. |
