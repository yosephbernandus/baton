# Baton

A lightweight, runtime-agnostic multi-agent orchestrator that enables a single orchestrator (any coding agent: Claude Code, OpenCode, pi-agent, or even a human directly) to delegate work to multiple external coding agents running different models, with real-time monitoring via a terminal UI.

## Problem Statement

When working with AI coding agents, there are three concrete pain points this project solves:

**1. Cognitive overload from multi-terminal agent management.**

Running two or more coding agents in separate terminals means the human becomes the integration layer. You mentally track what each agent is doing, merge context between them, remember what instructions you gave each one, and check whether their outputs conflict. This is exhausting and error-prone, especially on longer sessions.

**2. Cost inefficiency from using expensive models for everything.**

Opus is the most capable model for reasoning, architecture, and code review, but it's expensive per token. Most coding tasks (implementing a migration, writing boilerplate, scaffolding tests) don't need Opus-level intelligence. A cheaper model (Kimi, DeepSeek, Gemini Flash) handles them fine. But Claude Code only supports Anthropic models, and tools like OpenCode only support their own provider ecosystem. There's no way to mix and match efficiently.

**3. Context loss across agent boundaries.**

When delegating work from one agent to another, context degrades at each hop. The human knows everything. The orchestrator only knows what's written down. The worker only knows what's in its prompt. Without a structured system for context transfer, workers make technically correct but contextually wrong decisions because they lack the "why" behind the task.

**The solution:** A single orchestrator that plans, delegates, and reviews, while cheap models in external runtimes do the actual code writing. A structured context chain ensures no critical information is lost between hops. An escalation model handles uncertainty at every level. One terminal for the human. The orchestrator handles all coordination.

## Design Principles

- **Filesystem is the protocol.** No custom wire protocols, no RPC frameworks. Tasks, context, and results flow through files that every agent can read. The repo is the shared state. See "Why No Wire Protocols" for the rationale.
- **Orchestrator-agnostic.** The orchestrator is whoever calls `baton`. Could be Claude Code, OpenCode, pi-agent, or the human directly from their shell. Baton doesn't care. See "Orchestrator Model" for details.
- **Runtime-agnostic.** Adding a new agent runtime (aider, Cursor CLI, whatever ships next month) is a config entry and a thin adapter, not a code change.
- **Model-agnostic.** The bridge doesn't care what model runs where. Kimi, DeepSeek, GPT, Gemini, local Llama -- if the runtime supports it, Baton can target it.
- **Context is explicit.** Every task carries structured context -- what to do, why, constraints, related tasks, acceptance criteria. No implicit assumptions. Workers get self-contained task specs.
- **Escalation over guessing.** When a worker is confused, it signals `needs_clarification` instead of guessing. When the orchestrator is also confused, it escalates to the human (`needs_human`). Nobody produces garbage because they were afraid to ask.
- **No frameworks, no heavy dependencies.** Baton is a single Go binary with minimal dependencies (bubbletea for TUI, fsnotify for file watching). No LangChain, no CrewAI, no abstractions beyond what the problem requires.

## Why No Wire Protocols

Wire protocols (gRPC, JSON-RPC, MCP, WebSocket) solve three problems we don't have:

**Service discovery.** We already know where the workers are. They are CLI binaries in PATH. No need to register, advertise, or resolve addresses.

**Persistent connections.** Agents are not long-running servers. We spawn a process, it runs, it exits. There is no connection to keep alive. Each task is a fresh subprocess with a clear start and end. Context management is the orchestrator's job, not the transport layer's. The orchestrator writes self-contained task specs so each worker invocation has everything it needs without relying on session state.

**Bidirectional messaging.** The orchestrator sends a task (via CLI args), the worker produces output (via stdout). That is unidirectional per invocation. When a worker cannot proceed, it does not need to call back mid-task -- it exits with a clarification status, and the orchestrator handles the retry with better context.

The event log provides real-time streaming without a wire protocol. Baton captures stdout line-by-line and appends to NDJSON. The monitor tails it with fsnotify. That is "real-time" at the latency of a filesystem write -- microseconds.

**When wire protocols would be needed (not in scope):** If workers needed interactive mid-task negotiation with the orchestrator ("I found two approaches, which one?"), that would require bidirectional communication. The current design handles this via the escalation model: the worker exits with `needs_clarification`, the orchestrator re-delegates with better instructions. This is simpler and sufficient for the target use cases.

## Core Tradeoff

The system deliberately trades slightly lower per-task accuracy for massively higher throughput within the same cost budget. This works because accuracy is recoverable (reviews catch issues, re-delegation is cheap) but quota is not (once Opus messages are gone, the day is blocked).

```
Old way:  Opus writes code (95% accurate, 3-5 tasks/day, then throttled)
Baton:    Workers write code (80% accurate, 15+ tasks/day)
          + automated acceptance checks (catches to 88%)
          + Sonnet reviews (catches to 93%)
          + Opus audits critical tasks (catches to 97%)
          = more output, nearly same quality, 5x throughput
```

The ~2% gap between 97% and Opus-direct (95%) is the price paid. In practice, the gap is negligible because Opus doing everything directly is not 100% either.

## Three-Tier Model

The system uses three tiers with distinct roles, optimized for the cost-accuracy balance on a Claude Pro plan.

### The Tiers

```
Tier 1: ARCHITECT (Opus, Claude Code)
  Role:      High-level planning, write task specs directly, architecture
             decisions, final audit of critical tasks
  Frequency: 2-3 interactions per day
  Cost:      Pro plan Opus quota (scarce, ~5-10 messages/day)
  When:      Morning planning, hard decisions, end-of-day review

Tier 2: ORCHESTRATOR (Sonnet, Claude Code)
  Role:      Daily coordination, delegation, worker review, routine
             escalation, coherence checkpoints
  Frequency: 20-30 interactions per day
  Cost:      Sonnet on Pro plan (generous quota)
  When:      Throughout the day, continuous

Tier 3: WORKERS (Kimi/DeepSeek/Gemini via OpenCode/pi-agent/aider)
  Role:      Code writing, implementation, tests, boilerplate
  Frequency: As many as needed
  Cost:      API pricing ($0.50-2/day heavy use, free tiers available)
  When:      Whenever orchestrator delegates
```

### Why Three Tiers

Using Opus as the always-on orchestrator burns Pro plan quota on spec-writing and routine reviews. Sonnet handles that fine. Opus is reserved for two things only it does well: (1) breaking down complex problems into the right tasks with the right constraints, and (2) catching subtle issues during final review.

The critical design choice: **Opus writes the actual spec YAML files directly in the morning session**, not a casual breakdown that Sonnet reinterprets. This minimizes the context leak at the Opus-to-Sonnet boundary. Sonnet's job becomes mechanical -- verify context_files exist, update related_tasks status, run `baton run`. Sonnet does not reinterpret Opus's intent.

### Daily Workflow

```
Morning (Opus, 1 message):
  You: "Here's what we need to build today."
  Opus: Produces detailed task breakdown. Writes actual spec YAML files
        to .baton/specs/. Makes architecture decisions. Records decisions
        in ARCHITECTURE.md for persistence.

During the day (Sonnet, many messages):
  You: "Execute the plan."
  Sonnet reads Opus's specs. For each task:
    1. Verifies context_files exist on disk
    2. Updates related_tasks with current status of other tasks
    3. Runs: baton run --runtime X --model Y --spec .baton/specs/task-N.yaml
    4. Reviews worker output against acceptance_criteria
    5. If acceptance_checks pass and output looks correct: done
    6. If needs_clarification: handles if routine, escalates if not

  Every 3-5 completed tasks, Sonnet does a coherence checkpoint:
    "Do all changes work together? Any conflicts? Any bad assumptions?"

When Sonnet is stuck (you decide):
  Option A: You clarify directly (free)
  Option B: Spend 1 Opus message for hard architecture decision

Critical tasks (tagged criticality: high):
  Sonnet flags these for Opus review before building on top.
  You spend 1 Opus message, but only for tasks where mistakes cascade.

End of day (Opus, 1 message):
  You: "Review everything built today."
  Opus: Final quality pass. Catches architecture violations, security
        issues, cross-cutting concerns. Updates project docs.
```

### Cost Analysis (Pro Plan)

```
WITHOUT Baton (Opus does everything):
  Complex task A: 3 Opus messages (plan, implement, iterate)
  Complex task B: 3 Opus messages
  Medium task C:  2 Opus messages
  Small fixes:    2 Opus messages
  Total: 10 Opus messages, throttled by afternoon

WITH Baton (three-tier):
  Opus:    2-3 messages (morning specs + evening review + 1 critical)
  Sonnet:  15-20 messages (delegation, reviews, checkpoints)
  Workers: 8-12 tasks on Kimi/DeepSeek ($1-2 total API cost)
  Total: full day productivity, Opus quota barely touched
```

Break-even for delegation: if a task would use more than ~5,000 output tokens on the orchestrator, delegate it. Below that threshold, orchestrator does it directly.

### Orchestrator Agnostic

Baton does not know or care who calls it. Any agent that can run bash commands is a valid orchestrator.

```
Mode 1: Claude Code (Sonnet orchestrates, Opus architects)
  Human <-> Opus (morning) <-> writes specs
  Human <-> Sonnet (day) <-> baton <-> workers

Mode 2: OpenCode orchestrates (2M context for large codebase)
  Human <-> OpenCode (Kimi) <-> baton <-> workers

Mode 3: Human orchestrates directly
  Human <-> baton <-> workers
```

```yaml
orchestrator:
  runtime: claude-code
  model: sonnet  # daily orchestrator is Sonnet, not Opus
```

## Task Routing

Routing rules tell the orchestrator where to send each task. Baton does not enforce routing -- the orchestrator reads these rules and applies them during planning.

```yaml
routing:
  rules:
    - match:
        estimated_complexity: trivial
        estimated_tokens: "<2000"
      action: keep
      reason: "Orchestration tax exceeds task cost"

    - match:
        domain: [architecture, design, security, auth]
        estimated_complexity: high
      action: escalate
      target: opus
      reason: "Requires deep reasoning, save for Opus budget"

    - match:
        domain: [frontend, css, ui, components]
      action: delegate
      runtime: opencode
      model: kimi

    - match:
        domain: [infra, k8s, terraform, ci-cd]
      action: delegate
      runtime: opencode
      model: deepseek

    - match:
        domain: [tests, boilerplate, scaffolding, i18n]
      action: delegate
      runtime: opencode
      model: gemini-flash

    - match:
        default: true
      action: delegate
      runtime: opencode
      model: kimi

  checkpoint_interval: 3
  critical_review: opus
```

## Architecture Overview

```
+------------------------------------------------------------+
|  Human                                                      |
|  - Makes final decisions on ambiguous questions             |
|  - Approves or rejects orchestrator proposals               |
|  - Can use baton directly (Mode 3)                          |
+--------------------+---------------------------------------+
                     | conversation / direct CLI
+--------------------v---------------------------------------+
|  Orchestrator (any runtime: Claude Code, OpenCode, etc.)   |
|                                                             |
|  Responsibilities:                                          |
|  - Read project context (ARCHITECTURE.md, manifests)        |
|  - Plan tasks, build structured task specs                  |
|  - Delegate via: baton run|status|list|result               |
|  - Review worker output, approve or re-delegate             |
|  - Escalate to human when uncertain                         |
|  - Do its own high-complexity work directly                 |
|                                                             |
|  Primary value:                                             |
|  Writing perfect self-contained task specs.                 |
|  The orchestrator's #1 job is context curation.             |
+--------------------+---------------------------------------+
                     | bash commands
+--------------------v---------------------------------------+
|  baton (single Go binary)                                   |
|                                                             |
|  Subcommands:                                               |
|  - run       spawn a task in an external runtime            |
|  - status    show all active/completed tasks                |
|  - list      show available runtimes and models             |
|  - result    read the output of a completed task            |
|  - monitor   launch interactive TUI (full screen)           |
|  - config    get/set runtime configuration                  |
|                                                             |
|  Internal components:                                       |
|  - Config loader (reads agents.yaml + routing rules)        |
|  - Runtime registry (validates runtime+model combos)        |
|  - Spec validator (enforces structured task specs)          |
|  - Task runner (subprocess lifecycle management)            |
|  - Context chain (project brief + prompt from specs)        |
|  - File lock registry (prevents parallel write conflicts)   |
|  - Acceptance checker (runs automated checks post-task)     |
|  - Escalation detector (clarification pattern matching)     |
|  - Event emitter (writes NDJSON to event log)               |
|  - TUI renderer (bubbletea-based monitor)                   |
+--------------------+---------------------------------------+
                     | spawns subprocesses
          +----------+----------+
          v          v          v
       opencode   pi-agent   aider
       (kimi)     (gemini)   (gpt-4o)
          |          |          |
          +----------+----------+
                     v
             .baton/events.ndjson
                     |
                     v
             baton monitor (TUI)
```

## Context Chain

The context chain ensures information flows from human to orchestrator to worker without degradation. This is the most critical part of the design.

### The Problem

Context degrades at each hop:

```
Human (100% context)
  |  LOSS: unwritten decisions, implicit assumptions
  v
Orchestrator (70-80% context)
  |  reads project docs, but only knows what is written down
  |  LOSS: summarizes and compresses, might drop relevant details,
  |         might misunderstand and propagate errors
  v
Worker (40-60% context, with unstructured prompt)
  |  receives only a prompt string and some files
  |  no conversation history, no phase awareness, no "why"
```

### The Solution: Structured Task Specs

Every delegated task must include a structured spec that forces the orchestrator to be explicit about context. This prevents the common failure mode where the orchestrator writes a casual prompt and drops critical information.

```yaml
spec:
  what: |
    Implement user_sessions migration creating the sessions table.

  why: |
    Needed for the auto-expiry feature in Phase 2. Sessions must
    self-clean via TTL. The cleanup job (task-005) depends on the
    expires_at column existing.

  constraints:
    - "Use session_v2 schema (has TTL/expires_at field), NOT session_v1"
    - "Follow the migration pattern in migrations/002_users.go"
    - "Do NOT modify schema.go -- only create a new migration file"
    - "Table name must be user_sessions (plural)"

  context_files:
    - src/schema.go
    - migrations/002_users.go
    - docs/phase2-spec.md

  related_tasks:
    - task_id: task-001
      status: running
      summary: "Modifying session API handler -- do NOT touch routes/"
    - task_id: task-003
      status: completed
      summary: "Added session model struct"
      relevant_output: "Session struct is in models/session.go"

  acceptance_criteria:
    - "Migration creates user_sessions table"
    - "Columns: id (ULID PK), user_id (FK to users), token (unique), created_at, expires_at"
    - "Indexes on user_id and token"
    - "go build passes after migration"

  decisions:
    - question: "session_v1 or session_v2?"
      answer: "v2"
      reason: "TTL field required for auto-expiry feature"
      decided_by: human
    - question: "ULID or UUID for primary key?"
      answer: "ULID"
      reason: "Project standard, see ARCHITECTURE.md section on ID generation"
      decided_by: orchestrator

  # Files this task will create or modify (used for file locking)
  writes_to:
    - migrations/003_user_sessions.go   # will create
    - models/session.go                 # might modify

  # Concrete code examples for the worker to mimic
  # Workers are better at mimicry than interpretation
  examples:
    - description: "Migration function pattern to follow"
      code: |
        func Migrate003(db *sql.DB) error {
            _, err := db.Exec(`
                CREATE TABLE user_sessions (
                    -- worker fills in columns here
                );
            `)
            return err
        }

  # Automated checks baton runs BEFORE marking task complete
  # Catches obvious failures without orchestrator review
  acceptance_checks:
    - command: "go build ./..."
      expect_exit: 0
      description: "Code must compile"
    - command: "test -f migrations/003_user_sessions.go"
      expect_exit: 0
      description: "Migration file must exist"
    - command: "grep -q 'expires_at' migrations/003_user_sessions.go"
      expect_exit: 0
      description: "Must include expires_at column"

  # Task criticality -- high-criticality tasks get flagged for Opus review
  criticality: medium  # low | medium | high
```

### Why Each Field Matters

**`what`** -- The task itself. What the worker must produce.

**`why`** -- The most important field. Without it, workers make technically correct but contextually wrong decisions. "Implement migration" is a what. "Because the cleanup job depends on expires_at" is a why that prevents the worker from designing a schema without TTL support.

**`constraints`** -- The negative space. What NOT to do is often more important than what to do. Workers tend to be eager and touch files they should not, use patterns they should not, or make assumptions they should not. Constraints prevent this.

**`context_files`** -- Explicit list of files the worker needs to read. Not "the whole repo" but the specific files that contain relevant information. This keeps the worker's context window focused.

**`related_tasks`** -- What other workers are doing right now. This prevents conflicts (two workers editing the same file) and provides cross-task context. The `relevant_output` field lets the orchestrator include specific results from completed tasks that matter for this one.

**`acceptance_criteria`** -- How the orchestrator will verify the work. The worker knows upfront what "done" means. This reduces back-and-forth.

**`decisions`** -- An audit trail of choices made during planning. When a worker sees "session_v2, decided by human, reason: TTL required," it will not question the choice or explore alternatives. Critical for decisions from the human escalation path -- they propagate all the way to the worker.

**`writes_to`** -- Files this task will create or modify. Used by baton's file lock registry to prevent parallel tasks from conflicting. If task A holds a lock on `models/session.go` and task B's `writes_to` includes the same file, baton queues task B until task A completes. This is the primary mechanism for preventing cross-worker conflicts during parallel execution.

**`examples`** -- Concrete code snippets the worker should mimic. Workers produce significantly better first attempts when given a pattern to follow versus a textual description of the pattern. The orchestrator extracts relevant snippets from the referenced files and includes them inline. This raises first-attempt accuracy from ~70% to ~85% for implementation tasks.

**`acceptance_checks`** -- Automated commands baton runs after the worker exits, before marking the task complete. If any check fails, baton marks the task as `failed` with the specific check that broke, before the orchestrator even reviews it. This catches compilation errors, missing files, and constraint violations automatically, saving Sonnet a review cycle.

**`criticality`** -- Tags the risk level of a task. Low and medium tasks are reviewed by Sonnet only. High-criticality tasks (auth, payments, data migrations, core models) are flagged for Opus review before any other task builds on top of the output. This prevents the "built all day on bad code, Opus catches it at 6pm" failure mode.

### Context Flow With Specs

```
Human (100% context)
  |  Decisions captured in project docs (ARCHITECTURE.md,
  |  phase specs, .claude/memories/) and in the decisions
  |  field of task specs.
  v
Orchestrator (85-90% context)
  |  Reads all project docs. Builds structured task spec.
  |  The spec template FORCES completeness -- baton validates
  |  required fields. Unresolved ambiguity triggers escalation
  |  to human before delegation.
  v
Worker (75-85% context)
  |  Receives structured spec with what, why, constraints,
  |  related tasks, acceptance criteria, and decision history.
  |  Everything needed for self-contained execution.
  |  If still confused, signals needs_clarification.
```

### Spec Validation

Baton validates task specs before spawning workers:

- `what` must be non-empty.
- `why` must be non-empty. Most commonly skipped, most important.
- `constraints` must exist (can be empty array, but must be explicitly present).
- `context_files` must reference files that actually exist on disk.
- `acceptance_criteria` must have at least one item.
- `writes_to` if present, checked against file lock registry for conflicts.
- `acceptance_checks` if present, commands are validated for syntax (non-empty command string).
- `criticality` if present, must be one of: `low`, `medium`, `high`.

If validation fails, baton returns an error with the specific missing fields, forcing the orchestrator to fill gaps.

### Spec File vs Inline Prompt

```bash
# Spec file (recommended for non-trivial tasks)
baton run --runtime opencode --model kimi --spec .baton/specs/task-002.yaml

# Inline prompt (for quick, simple tasks)
baton run --runtime opencode --model kimi \
  --prompt "Fix the typo in README.md line 42" \
  --skip-validation
```

`--skip-validation` bypasses spec validation for inline prompts. Intentionally explicit.

## File Lock Registry

When tasks run in parallel, two workers might try to modify the same file. The `writes_to` field in the spec declares which files a task will touch. Baton maintains a lock registry to prevent conflicts.

### Lock file

```yaml
# .baton/locks.yaml (managed by baton, do not edit manually)
locks:
  models/session.go:
    held_by: task-002
    since: "2026-04-26T10:30:00Z"
  migrations/:
    held_by: task-002
    type: prefix  # locks anything under migrations/
    since: "2026-04-26T10:30:00Z"
```

### Lock behavior

When `baton run` is called with a spec that has `writes_to`:

1. Baton checks each path against the lock registry.
2. If no conflict: acquires locks, spawns worker.
3. If conflict with a running task: returns error with the conflicting task ID. Example: "cannot start task-003: models/session.go is locked by task-002 (running for 1m23s)."
4. The orchestrator decides: wait and retry later, or rewrite the spec to avoid the conflicting file.
5. On task completion (any status): locks are released.

Locks are advisory -- they prevent baton from spawning conflicting tasks, but they do not prevent a worker from touching unlisted files. The `writes_to` field is a declaration of intent, not an enforcement mechanism. Workers that touch files not listed in `writes_to` create undetected conflicts -- this is caught during Sonnet's coherence checkpoints or Opus's final review.

### Prefix locks

Specs can lock entire directories by using a trailing slash: `migrations/` locks any file under that path. This is useful when a task creates new files in a directory (the exact filename is not known in advance).

## Project Brief

Every worker receives a project brief as a preamble to its prompt, regardless of the task. This gives workers baseline project conventions without the orchestrator repeating them in every spec.

### Brief file

```markdown
# .baton/project-brief.md

Project: MiiTel Session Service
Language: Go 1.22
Framework: stdlib + sqlx
Database: PostgreSQL 15
ID strategy: ULID (see pkg/ulid)
Error handling: return errors, no panic
Naming: snake_case for DB columns, camelCase for Go
Test framework: testing + testify
Migration tool: custom (see migrations/README.md)
Logging: slog with structured fields
API style: REST, JSON responses, standard HTTP status codes
```

### How it works

When baton constructs the worker prompt from a spec, it prepends the project brief:

```
[PROJECT CONTEXT]
{contents of .baton/project-brief.md}

[TASK]
{spec.what}

[WHY THIS MATTERS]
...
```

This raises the worker's baseline from "zero project knowledge" to "understands conventions." The brief is written once (by Opus during initial project setup) and updated occasionally. It is not a substitute for task-specific context in the spec -- it covers universal project rules that apply to every task.

## Escalation Model

Handles uncertainty at every level. Core principle: **escalate over guessing.**

### Escalation Chain

```
Worker confused
  -> signals needs_clarification (exits with clarification status)
  -> Orchestrator reads the clarification request
  -> Orchestrator either:
      (a) understands -> updates spec with answer, re-delegates
      (b) also uncertain -> asks the human (needs_human)

Human clarifies
  -> Orchestrator captures decision in spec's decisions field
  -> Re-delegates with enriched spec
  -> Decision preserved for current and future tasks
```

### How Workers Signal Clarification

Workers are not modified for baton. Baton uses convention-based detection:

**Exit code.** Configurable code (default: 10) means `needs_clarification` rather than `failed`.

**Output pattern matching.** Baton scans worker stdout for patterns:

```yaml
clarification_patterns:
  - "I'm not sure"
  - "unclear which"
  - "could you clarify"
  - "ambiguous"
  - "multiple possible"
  - "CLARIFICATION_NEEDED"
```

If a pattern is detected AND the worker exits non-zero, baton marks the task as `needs_clarification`.

**Explicit marker.** The prompt builder appends this instruction to every worker prompt:

```
If you are uncertain about any aspect of this task, output the line:
CLARIFICATION_NEEDED: <your question here>
and exit. Do NOT guess.
```

### How Orchestrator Escalates to Human

The orchestrator-to-human escalation happens in their conversation, not through baton. The orchestrator:

1. Reads `baton result --clarification` for the worker's question
2. Adds its own analysis ("I think it's X because..., but I'm not sure because...")
3. Asks the human in conversation
4. Captures the human's answer as a decision in the spec
5. Re-delegates

This means the human gets a curated question with analysis, not a raw dump from the worker. That is the value of a smart orchestrator in the middle.

### Task States

```
pending -> running -> completed
                   -> failed (worker error, not confusion)
                   -> timeout (killed after --timeout)
                   -> needs_clarification (worker signaled confusion)
                       -> orchestrator reviews
                           -> re-delegated (new attempt, back to pending)
                           -> needs_human (orchestrator also uncertain)
                               -> human clarifies
                               -> re-delegated (new attempt, back to pending)
```

### Decision Persistence

When the human clarifies, the decision is captured in the task spec:

```yaml
decisions:
  - question: "SessionV1 or SessionV2?"
    answer: "SessionV2"
    reason: "TTL/expires_at required for auto-expiry feature"
    decided_by: human
```

This decision is part of the task record. If re-delegated, retried, or if a related task needs the same info, the decision is preserved. The orchestrator should also write significant decisions to permanent project context (ARCHITECTURE.md, decision logs) so they survive beyond this task's lifecycle.

## Existing Workflow Integration

This project extends an existing orchestrator pattern already running inside Claude Code. The current setup uses a 16-phase pipeline:

```
1 Setup -> 2 Triage -> 3 Discovery -> 4 Skill Discovery
   [reflect]  [reflect]   [reflect]     [reflect]

5 Complexity Assessment

6 Brainstorm -> 7 Architecture -> 8 Implementation
   [reflect]      [reflect]         |
                              DELEGATE TO WORKERS
                              (currently: Claude Code subagents)

9 Design Verify

10 Domain Compliance -> 11 Code Quality -> 12 Test Planning
      [reflect]            [reflect]         [reflect]

13 Testing -> 14 Coverage -> 15 Test Quality
   [reflect]    [reflect]      [reflect]

16 Completion -> feedback summary + update manifest
```

Current system characteristics:

- **Coordinator role** (Opus): Can only Read, Glob, Grep, Bash(verify). Cannot Edit, Write. Plans and observes only.
- **Worker roles**: Domain-routed subagents (server-coder, client-coder, client-ui, client-lang, infra-coder, default agent).
- **Persistence layers**: Ephemeral state in `.data/manifest.yaml` and `.data/feedback/`, permanent knowledge in `ARCHITECTURE.md` and `.claude/memories/`.
- **Reflect loops**: After each major phase, the coordinator reflects and adjusts.

**What changes with Baton:** Only phase 8 changes. Instead of all workers being Claude Code subagents, the coordinator can now route to external runtimes via `baton run`. Everything else stays identical.

Delegation decision:

```
Task complexity: HIGH   -> keep in current runtime (Opus/Sonnet)
Task complexity: MEDIUM -> delegate via baton (Kimi/DeepSeek/Gemini)
Task complexity: LOW    -> delegate via baton (cheapest available model)
Task domain: frontend   -> route to runtime best suited for frontend
Task domain: infra      -> route to runtime best suited for infra
```

## Configuration

### agents.yaml

Located at `~/.baton/agents.yaml` or `./.baton/agents.yaml` (project-local takes precedence).

```yaml
orchestrator:
  runtime: claude-code
  model: opus

runtimes:
  opencode:
    command: "opencode"
    model_flag: "-m"
    prompt_flag: "-p"
    context_flag: "--files"
    workdir: "inherit"
    models:
      - kimi
      - deepseek
      - gemini-flash
      - deepseek-r1

  pi-agent:
    command: "pi-agent"
    model_flag: "--model"
    prompt_flag: "--prompt"
    context_flag: "--context"
    workdir: "inherit"
    models:
      - gemini
      - grok

  aider:
    command: "aider"
    model_flag: "--model"
    prompt_flag: "--message"
    context_flag: ""
    extra_flags:
      - "--yes"
      - "--no-auto-commits"
    workdir: "inherit"
    models:
      - gpt-4o
      - deepseek
      - claude-sonnet

defaults:
  runtime: opencode
  model: kimi

clarification_patterns:
  - "I'm not sure"
  - "unclear which"
  - "could you clarify"
  - "ambiguous"
  - "multiple possible"
  - "CLARIFICATION_NEEDED"

clarification_exit_code: 10

event_log: ".baton/events.ndjson"
task_dir: ".baton/tasks"
result_dir: ".baton/results"
spec_dir: ".baton/specs"
```

### Runtime Adapter Contract

Each runtime adapter translates baton's generic invocation into the runtime's CLI syntax:

```
Generic:
  baton run --runtime opencode --model kimi --spec .baton/specs/task-002.yaml

Baton reads spec, constructs:
  opencode -m kimi -p "<prompt built from spec>" --files src/schema.go,docs/phase2.md
```

### Prompt Construction from Spec

Baton converts structured specs into a coherent prompt for the worker:

```
[TASK]
{spec.what}

[WHY THIS MATTERS]
{spec.why}

[CONSTRAINTS]
- {each constraint}

[RELATED TASKS -- DO NOT CONFLICT]
- {task_id} ({status}): {summary}
  Output: {relevant_output}

[ACCEPTANCE CRITERIA]
- {each criterion}

[DECISIONS ALREADY MADE]
- Q: {question} -> A: {answer} (reason: {reason}, decided by: {decided_by})

[IMPORTANT]
If you are uncertain about any aspect of this task, output the line:
CLARIFICATION_NEEDED: <your question here>
and exit. Do NOT guess.
```

## CLI Interface

### baton run

Spawns a task in an external runtime.

```bash
# With spec file (recommended)
baton run --runtime opencode --model kimi --spec .baton/specs/task-002.yaml

# With inline prompt (simple tasks)
baton run --runtime opencode --model kimi \
  --prompt "Fix typo in README.md" \
  --skip-validation
```

Flags:
- `--runtime` (required): Runtime key from agents.yaml.
- `--model` (required): Model from the runtime's model list.
- `--spec` (optional): Path to task spec YAML file.
- `--prompt` (optional): Inline prompt. Requires `--skip-validation`.
- `--task-id` (optional): Task identifier. Auto-generated as `task-{timestamp}` if omitted.
- `--context-files` (optional): Comma-separated files (only with --prompt).
- `--skip-validation` (optional): Skip spec validation for inline prompts.
- `--background` (optional, default false): Return immediately if true.
- `--timeout` (optional, default 10m): Max time before killing worker.

Behavior:
1. Validates runtime and model exist in config.
2. If `--spec`: loads and validates spec (what, why, constraints, acceptance_criteria required; context_files must exist on disk).
3. Checks `writes_to` against file lock registry. If conflict: exit with error.
4. Acquires file locks for all `writes_to` paths.
5. Creates task record in `.baton/tasks/{task-id}.yaml`.
6. Constructs subprocess command from config + project brief + spec/prompt.
7. Takes git snapshot (if in git repo) for file change detection.
8. Spawns subprocess.
9. Streams stdout/stderr to event log.
10. Scans output for clarification patterns.
11. On worker exit: runs `acceptance_checks` commands if defined in spec.
12. If any acceptance check fails: status = `failed`, includes which check broke.
13. If clarification detected + non-zero exit: status = `needs_clarification`.
14. Updates task record with status, result, files changed.
15. Releases file locks.
16. If not `--background`: prints result to stdout.

Exit codes:
- 0: Completed successfully.
- 1: Failed (worker error).
- 2: Configuration error.
- 3: Spec validation error.
- 4: Lock conflict (file locked by another task).
- 5: Acceptance check failed.
- 10: Needs clarification.
- 124: Timed out.

### baton status

```bash
baton status
```

```
TASK ID     RUNTIME     MODEL      STATUS              DURATION
task-001    opencode    kimi       completed           2m15s
task-002    opencode    kimi       needs human         -
task-003    pi-agent    gemini     running             1m03s
task-004    opencode    deepseek   failed              0m12s
task-005    opencode    kimi       needs clarify       -
```

Flags: `--json`, `--filter {status}`

### baton list

```bash
baton list
```

```
RUNTIME     MODELS                              STATUS
opencode    kimi, deepseek, gemini-flash        available
pi-agent    gemini, grok                        available
aider       gpt-4o, deepseek, claude-sonnet     not found
```

### baton result

```bash
baton result task-001
baton result task-005 --clarification
baton result task-002 --escalation
```

Flags: `--json`, `--files-only`, `--clarification`, `--escalation`

### baton monitor

Launches interactive TUI. See TUI Monitor section.

### baton config

```bash
baton config set orchestrator.runtime opencode
baton config get orchestrator.runtime
```

## Task Record Format

```yaml
# .baton/tasks/task-002.yaml
id: task-002
runtime: opencode
model: kimi
status: running
dispatched_by: claude-code/opus

spec:
  what: "Implement user_sessions migration"
  why: "Needed for auto-expiry feature"
  constraints:
    - "Use SessionV2 schema"
    - "Follow pattern in 002_users.go"
  context_files:
    - src/schema.go
    - migrations/002_users.go
  related_tasks:
    - task_id: task-001
      status: running
      summary: "Modifying session handler -- do NOT touch routes/"
  acceptance_criteria:
    - "Creates user_sessions table"
    - "Indexes on user_id and token"
    - "go build passes"
  writes_to:
    - migrations/003_user_sessions.go
  acceptance_checks:
    - command: "go build ./..."
      expect_exit: 0
      description: "Code must compile"
  criticality: medium
  decisions:
    - question: "SessionV1 or SessionV2?"
      answer: "SessionV2"
      reason: "TTL required for auto-expiry"
      decided_by: human

escalation:
  worker_clarification: null
  orchestrator_analysis: null
  human_decision: null
  human_reason: null

attempts:
  - attempt: 1
    started_at: "2026-04-26T10:30:00Z"
    completed_at: null
    status: running

created_at: "2026-04-26T10:30:00Z"
started_at: "2026-04-26T10:30:01Z"
completed_at: null
duration: null
pid: 45231
exit_code: null
files_changed: []
error: null
```

## Event Log

NDJSON, one event per line, appended to `.baton/events.ndjson`.

```jsonl
{"ts":"...","task_id":"task-002","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"task_created","data":{"spec_summary":"Implement user_sessions migration"}}
{"ts":"...","task_id":"task-002","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"task_started","data":{"pid":45231,"attempt":1}}
{"ts":"...","task_id":"task-002","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"output","data":{"stream":"stdout","line":"Reading src/schema.go..."}}
{"ts":"...","task_id":"task-002","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"file_changed","data":{"path":"migrations/003.go","action":"created"}}
{"ts":"...","task_id":"task-002","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"task_completed","data":{"exit_code":0,"duration":"1m23s"}}
{"ts":"...","task_id":"task-003","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"needs_clarification","data":{"clarification":"Found two session schemas..."}}
{"ts":"...","task_id":"task-003","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"needs_human","data":{"analysis":"I think SessionV2 but want confirmation..."}}
{"ts":"...","task_id":"task-003","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"human_decided","data":{"decision":"SessionV2","reason":"TTL required"}}
{"ts":"...","task_id":"task-003","runtime":"opencode","model":"kimi","dispatched_by":"claude-code/opus","event":"re_delegated","data":{"attempt":2}}
```

Event types: `task_created`, `task_started`, `output`, `file_changed`, `task_completed`, `task_failed`, `task_timeout`, `needs_clarification`, `needs_human`, `human_decided`, `re_delegated`, `lock_acquired`, `lock_released`, `lock_conflict`, `acceptance_check_passed`, `acceptance_check_failed`.

Log rotation: rotate at 10MB (configurable), keep last 3 files.

## TUI Monitor

### Layout

```
+-- Baton Monitor -------------------------------------------------+
|  Orchestrator: claude-code/opus                                   |
|                                                                   |
|  TASKS                                                            |
|  +----------+-----------+----------+--------------+-------------+ |
|  | Task     | Runtime   | Model    | Status       | Duration    | |
|  +----------+-----------+----------+--------------+-------------+ |
|  | task-001 | opencode  | kimi     | completed    | 2m15s       | |
|  | task-002 | opencode  | kimi     | needs human  | -           | |
|  | task-003 | pi-agent  | gemini   | running      | 1m03s       | |
|  | task-004 | opencode  | deepseek | failed       | 0m12s       | |
|  +----------+-----------+----------+--------------+-------------+ |
|                                                                   |
|  OUTPUT: task-003 (pi-agent/gemini)                               |
|  | Reading context files...                                     | |
|  | Analyzing i18n patterns...                                   | |
|  | Adding translation keys for session module                   | |
|  | [cursor]                                                     | |
|                                                                   |
|  ! task-002 blocked -- waiting for human decision                 |
|  Q: "SessionV1 or SessionV2?" (orchestrator thinks: SessionV2)   |
|                                                                   |
|  [up/down] select  [enter] view output  [e] escalation  [q] quit |
+-------------------------------------------------------------------+
```

### Features

- **Real-time event tailing** via fsnotify. New events appear instantly.
- **Task table** with status indicators. Running tasks show spinner and elapsed time.
- **Output panel** for selected task. Auto-scrolls for running, manual scroll for completed.
- **Escalation banner** when any task is `needs_human`. Shows question and orchestrator analysis.
- **Color coding.** Running = yellow, completed = green, failed = red, timeout = orange, needs_clarification = cyan, needs_human = magenta.

### Implementation

- `bubbletea` for TUI framework.
- `lipgloss` for styling.
- `fsnotify` for file watching.

Monitor is read-only. It reads `.baton/events.ndjson` and maintains in-memory task state.

## Project Structure

```
baton/
|-- main.go                     # entrypoint, cobra root command
|-- go.mod
|-- go.sum
|-- agents.example.yaml         # example config
|-- README.md
|-- ARCHITECTURE.md             # this file
|
|-- cmd/
|   |-- run.go                  # "baton run"
|   |-- status.go               # "baton status"
|   |-- list.go                 # "baton list"
|   |-- result.go               # "baton result"
|   |-- monitor.go              # "baton monitor"
|   +-- config.go               # "baton config"
|
+-- internal/
    |-- config/
    |   +-- config.go           # YAML config parsing, validation
    |                           # LoadConfig(), ValidateRuntime()
    |                           # Config precedence: project > home
    |
    |-- spec/
    |   +-- spec.go             # Task spec parsing and validation
    |                           # LoadSpec(), Validate(), BuildPrompt()
    |                           # Ensures context_files exist on disk
    |                           # Validates writes_to, acceptance_checks
    |
    |-- runner/
    |   +-- runner.go           # Subprocess lifecycle management
    |                           # Run(): construct cmd, spawn, capture
    |                           # Prepends project brief to prompt
    |                           # Scans for clarification patterns
    |                           # Runs acceptance_checks after worker exits
    |                           # Timeout via context.WithTimeout
    |                           # Git diff for file change detection
    |
    |-- task/
    |   +-- task.go             # Task record CRUD
    |                           # Create(), Update(), List(), Get()
    |                           # AddAttempt() for retry tracking
    |                           # Escalation field management
    |
    |-- lock/
    |   +-- lock.go             # File lock registry
    |                           # Acquire(), Release(), Check()
    |                           # Manages .baton/locks.yaml
    |                           # Supports prefix locks (directories)
    |                           # Auto-release on task completion
    |
    |-- brief/
    |   +-- brief.go            # Project brief loader
    |                           # LoadBrief() reads .baton/project-brief.md
    |                           # Returns empty string if file missing
    |
    |-- events/
    |   +-- events.go           # NDJSON event log
    |                           # Emitter: Emit() with flock
    |                           # Tailer: tail with fsnotify
    |
    +-- tui/
        |-- tui.go              # bubbletea model, update, view
        |-- taskview.go         # task table component
        |-- outputview.go       # output panel with ring buffer
        +-- escalation.go      # escalation detail view ([e] key)
```

## Implementation Notes

### Subprocess Management

- **Process group.** `Setpgid: true` so baton can kill entire process tree on timeout.
- **Timeout.** `context.WithTimeout`. On timeout: SIGTERM, wait 5s, SIGKILL.
- **Stdin.** Pass everything via flags for v1. Add `stdin_mode: pipe|none` config later if needed.
- **Working directory.** `cmd.Dir` set to project root.
- **Environment.** Inherit current env. Do not strip API keys.

### Concurrency

- **Event log.** Use flock or channel-based serialization for concurrent writes.
- **Task files.** Each task has its own file, no contention. Writes are atomic (temp + rename).
- **Background tasks.** Start foreground-only. Add daemon mode in v2 if needed.

### Git Integration

```go
// Before spawn
beforeDiff, _ := exec.Command("git", "diff", "--name-only").Output()
untracked, _ := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output()

// After completion: compare to find changes
```

### Error Handling

- Runtime not in PATH: clear error with install suggestion.
- Model not in runtime's list: error with available models.
- Spec validation failure: error listing missing fields ("spec missing 'why'").
- Lock conflict: "cannot start task-003: models/session.go is locked by task-002 (running 1m23s)."
- Acceptance check failure: "task-002 failed acceptance check: 'go build ./...' exited with code 1."
- Worker crash: check clarification patterns first, then mark failed.
- Baton crash: on startup, scan stale running tasks (PID dead), mark failed. Release orphaned locks.

## Build Phases

### Phase 1: Core CLI + Spec System (MVP)
- Config loading from agents.yaml (runtimes, models, defaults)
- Spec parsing and validation (what, why, constraints, context_files, acceptance_criteria)
- Prompt construction from spec (concatenates fields into worker prompt)
- Project brief loading (.baton/project-brief.md prepended to all prompts)
- `baton run` foreground execution (spec file + inline prompt modes)
- `baton status` with table output
- `baton list` with runtime availability detection
- `baton result` with --clarification flag
- Event log (NDJSON) writing
- Task record CRUD with escalation fields
- Clarification pattern detection in worker output
- One runtime: OpenCode

Deliverable: orchestrator can delegate via structured spec, baton validates, spawns worker, prepends project brief, detects confusion, captures results.

### Phase 2: Quality Gates
- `writes_to` field in spec + file lock registry (.baton/locks.yaml)
- Lock acquisition before spawn, release on completion
- Lock conflict detection with clear error messages
- `acceptance_checks` in spec: baton runs automated checks after worker exits
- `examples` field in spec: included in constructed prompt
- `criticality` field in spec: included in task record for orchestrator to act on

Deliverable: parallel tasks cannot conflict on files. Obvious failures caught automatically before orchestrator review.

### Phase 3: TUI Monitor
- `baton monitor` with bubbletea
- Real-time event tailing with fsnotify
- Task table with status indicators (including lock conflicts, acceptance failures)
- Output panel with auto-scroll
- Escalation banner for needs_human tasks
- Keyboard navigation

Deliverable: real-time visibility from a tmux pane.

### Phase 4: Multi-Runtime + Orchestrator Agnostic
- Additional runtime adapters (pi-agent, aider)
- `baton config` for orchestrator switching
- `dispatched_by` tracking in events and tasks
- Routing rules in config (read by orchestrator, not enforced by baton)

Deliverable: any runtime as orchestrator, any runtime as worker. Routing guidance available.

### Phase 5: Advanced Features
- Background execution (daemon mode or detached processes)
- Git integration for file change detection (diff before/after)
- Timeout with graceful shutdown (SIGTERM, wait, SIGKILL)
- Log rotation (10MB default, keep 3)
- JSON output mode for all commands (--json flag)
- Stale task cleanup on startup (dead PIDs marked failed)
- Attempt tracking and retry history

### Phase 6: Smart Features (Optional)
- Automatic runtime+model selection based on routing rules
- Cost tracking per task (if token usage detectable from worker output)
- Decision persistence to project docs (auto-append to ARCHITECTURE.md)
- Coherence checkpoint prompts (generated after every N completed tasks)

## Interaction Examples

### Example 1: Three-Tier Daily Flow

```
MORNING (Opus, 1 message):

Human to Opus: "Today we need to: add session migration, build the
  session API endpoint, add i18n for session strings, and update
  the k8s deployment manifest. Break it down."

Opus produces actual spec files:
  .baton/specs/task-migration.yaml   (criticality: medium)
  .baton/specs/task-api.yaml         (criticality: high -- touches auth)
  .baton/specs/task-i18n.yaml        (criticality: low)
  .baton/specs/task-k8s.yaml         (criticality: medium)

Opus recommends routing:
  "task-migration -> opencode/kimi (standard implementation)
   task-api -> keep in Sonnet (high criticality, needs Opus review)
   task-i18n -> opencode/gemini-flash (trivial, cheapest model)
   task-k8s -> opencode/deepseek (infra domain strength)"

DAY (Sonnet, many messages):

Human to Sonnet: "Execute the plan."

Sonnet:
$ baton run --runtime opencode --model kimi \
    --spec .baton/specs/task-migration.yaml
  -> baton validates spec, checks file locks, spawns opencode
  -> worker completes, baton runs acceptance_checks (go build): passes
  -> status: completed

$ baton run --runtime opencode --model gemini-flash \
    --spec .baton/specs/task-i18n.yaml
  -> runs in parallel with Sonnet's own work on task-api

Meanwhile, Sonnet works on task-api itself (high criticality, not delegated).

$ baton run --runtime opencode --model deepseek \
    --spec .baton/specs/task-k8s.yaml
  -> baton detects lock conflict: writes_to overlaps with running task
  -> Sonnet: "I'll queue k8s until migration finishes."

After 3 tasks complete, Sonnet coherence checkpoint:
  "Migration, i18n, and k8s changes are compatible. No conflicts."

Sonnet to human: "task-api is done but it's high-criticality. Recommend
  Opus review before we build the cleanup job on top of it."

EVENING (Opus, 1 message):

Human to Opus: "Review today's work, especially the session API."

Opus: reviews all diffs, catches that the API endpoint doesn't validate
  token expiry on the read path. Fixes it directly. Updates ARCHITECTURE.md
  with the token validation decision for future reference.
```

### Example 2: Worker Confused, Orchestrator Handles

```
Sonnet runs:
$ baton run --runtime opencode --model kimi \
    --spec .baton/specs/task-sessions.yaml

Worker output: "CLARIFICATION_NEEDED: Found SessionV1 and SessionV2, which one?"
Worker exits with code 10.
Baton sets status: needs_clarification.

Sonnet:
$ baton result task-sessions --clarification
  -> "Found SessionV1 and SessionV2, which one?"

Sonnet checks Opus's morning specs: decision already recorded.
  "SessionV2, decided by human, reason: TTL required."
Sonnet adds decision to spec, re-runs:
$ baton run --runtime opencode --model kimi \
    --spec .baton/specs/task-sessions.yaml

Worker succeeds on second attempt.
```

### Example 3: Full Escalation Chain

```
Worker: "CLARIFICATION_NEEDED: Migration naming inconsistent.
         002_users.go uses timestamps but 001_init.go uses sequence numbers."

Sonnet reads this. Checks the codebase. Both patterns exist. Genuinely unsure.

Sonnet to human:
"The worker found inconsistent migration naming -- timestamps vs sequences.
I checked and both exist. Which convention should we standardize on?"
Human: "Use timestamps. Update the project brief so this is documented."

Sonnet captures decision in spec:
  decisions:
    - question: "Migration naming: timestamps or sequence numbers?"
      answer: "timestamps"
      reason: "Human standardized on timestamps"
      decided_by: human

Sonnet re-delegates with updated spec. Worker succeeds.
Sonnet also updates .baton/project-brief.md with the new convention
so future tasks get it automatically.
```

### Example 4: Lock Conflict

```
Sonnet tries to run two tasks in parallel:

$ baton run --runtime opencode --model kimi --spec .baton/specs/task-a.yaml
  -> acquires lock on models/session.go
  -> status: running

$ baton run --runtime opencode --model deepseek --spec .baton/specs/task-b.yaml
  -> task-b writes_to includes models/session.go
  -> baton: "cannot start task-b: models/session.go locked by task-a (running 45s)"
  -> exit code 4

Sonnet: "task-b depends on the same file. I will wait for task-a to
  finish, then run task-b with the updated file as context."

task-a completes. Lock released.
Sonnet re-runs task-b successfully.
```
