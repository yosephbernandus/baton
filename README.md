# Baton

Runtime-agnostic multi-agent orchestrator for teams. Baton coordinates coding tasks across multiple agents and team members with structured context preservation.

## The Problem

AI coding agents are siloed. When your team uses multiple agents:

- Claude Code only talks to Anthropic models
- OpenCode only talks to its ecosystem
- Team members running different agents step on each other
- Decisions made by one agent are lost when another agent picks up the work
- File conflicts from parallel work cause frustration and rework

Baton solves this by being the coordination layer between agents and team members.

## Who Should Use This

Baton is designed for:

- **IT departments** running multiple AI coding agents across the team
- **Engineering teams** with developers using different agent runtimes (Claude Code, OpenCode, etc.)
- **Organizations** where multiple agents work on the same codebase

If you are a solo developer running one agent, Baton adds complexity without much benefit. Baton shines when multiple agents or team members need to coordinate.

## How It Works

```
Team Member (Orchestrator)
    |
    | baton run --spec .baton/specs/my-task.yaml
    v
Baton
    |-- Validates task spec
    |-- Acquires file locks (prevents conflicts)
    |-- Spawns the configured runtime (OpenCode, aider, etc.)
    |-- Tracks events to events.ndjson
    |-- Records cost to costs.ndjson
    v
Worker Agent (Kim, OpenCode, etc.)
    |
    | Produces code + acceptance results
    v
Baton <-- Records completion, releases locks
```

Baton does not plan, reason, or make decisions. It executes what the orchestrator assigns and tracks the results.

## Use Cases

### Use Case 1: Two Agents Editing the Same File

**Scenario:** Developer Alice runs OpenCode to add a login feature. Developer Bob runs OpenCode to refactor the same login handler.

**Without Baton:**
- Alice's agent modifies `handlers/login.go` and saves
- Bob's agent modifies `handlers/login.go` and saves
- One overwrite is lost
- Alice spends 2 hours re-doing work that Bob's agent erased

**With Baton:**
- Alice runs: `baton run --spec specs/login-feature.yaml`
- Baton records that `handlers/login.go` is locked by task-001
- Bob tries to run his task but Baton sees the lock conflict
- Bob waits until Alice's task finishes
- No lost work, no frustration

**What you need to do:**
Alice's spec includes:
```yaml
writes_to:
  - handlers/login.go
```

### Use Case 2: The "Just Implement It" Problem

**Scenario:** You ask an agent to "add user authentication."

**Without Baton:**
- Agent decides to use JWT with RS256
- Next sprint, another agent "adds authentication" but uses HS256
- Incompatible auth systems exist in the same codebase
- You spend a week untangling the mess

**With Baton:**
- You write the spec with the decision already made:
```yaml
decisions:
  - question: "JWT algorithm?"
    answer: "RS256"
    reason: "ADR-015: HS256 is insecure for service-to-service"
    decided_by: security-team
```

- Agent implements RS256, no re-litigation
- Consistent auth across the codebase

### Use Case 3: Handoff Between Team Members

**Scenario:** Junior developer picks up a task started by a senior developer who went on vacation.

**Without Baton:**
- Junior reads the code, does not understand why it was designed this way
- Junior changes the approach because it "looks wrong"
- Senior returns, realizes the design was deliberate, rewrites
- 2 weeks wasted

**With Baton:**
- Senior writes the spec before vacation:
```yaml
why: |
  This uses a queue because the legacy sync API has 30s timeout.
  The queue allows async processing so users do not wait.
  See ADR-022 for the full decision tree.
```
- Junior reads the spec, understands the constraints
- Junior completes the task correctly

### Use Case 4: Multiple Runtimes, One Coordination Point

**Scenario:** Your team has developers using different agents:
- Alice uses Claude Code (Anthropic models)
- Bob uses OpenCode (Kimi, DeepSeek)
- Carol uses a custom internal agent

**Without Baton:**
- Alice does not know what Bob's agent is doing
- Carol's agent steps on both of them
- You manage coordination in Slack, meetings, Notion
- Context is scattered across 5 tools

**With Baton:**
- All tasks go through Baton
- `baton status` shows what every agent is doing
- `baton list` shows which runtimes are available
- `baton cost` shows cost per runtime
- One source of truth for task state

### Use Case 5: Cost Reporting

**Scenario:** End of month, your manager asks "how much did we spend on AI coding last month?"

**Without Baton:**
- You check Claude Code subscription
- You check OpenAI API usage
- You check Kimi API usage
- You manually compile a spreadsheet
- 3 hours later you have incomplete data

**With Baton:**
```bash
baton cost --json > monthly-report.json
```
- Every task is logged with model, duration, tokens
- Per-agent breakdown
- Exportable to CSV or JSON
- 5 minutes to compile the report

## How Baton Fits Into Your Workflow

Baton is the execution layer. The orchestration logic (who does what, when, in what order) lives in your existing system. Baton receives structured tasks and executes them against configured runtimes.

A complete context design workflow looks like:

```
1. Brainstorm with an LLM
   |
   v
2. Write ADR if the decision is significant
   |
   v
3. Write task spec (what, why, constraints, context, acceptance)
   |
   v
4. baton run --spec .baton/specs/my-task.yaml
   |
   v
5. Worker produces code + acceptance results
   |
   v
6. baton records events, cost, releases locks
```

The orchestration, role definitions, wiki-based knowledge base, and findings workflow are your responsibility. Baton handles step 4 and 6.

See [docs/user-guide.md](docs/user-guide.md) for a detailed guide on going from raw LLM discussion to a Baton task spec.

## Integrate Baton Into Your Project

See **[INTEGRATION.md](INTEGRATION.md)** for a complete step-by-step guide. Works with any LLM — tell your AI assistant:

> "Read INTEGRATION.md from the baton repository and integrate baton into this project."

The guide covers: runtime configuration, project brief setup, task spec format, orchestration patterns, feedback loop commands, and troubleshooting.

## Install

### One-line install (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/yosephbernandus/baton/main/install.sh | sh
```

Works with bash, zsh, and fish. Detects OS and architecture automatically.

Custom install directory:

```bash
BATON_INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/yosephbernandus/baton/main/install.sh | sh
```

### Go install

```bash
go install github.com/yosephbernandus/baton@latest
```

### Build from source

```bash
git clone https://github.com/yosephbernandus/baton.git
cd baton
go build -o baton .
```

### Download binary manually

Pre-built binaries for Linux, macOS, and Windows (amd64/arm64) are available on the [Releases](https://github.com/yosephbernandus/baton/releases) page.

## Quick Start

1. Create config:

```bash
mkdir -p .baton
cp agents.examples.yaml .baton/agents.yaml
# Edit to match your installed runtimes
```

2. Create a task spec:

```yaml
# .baton/specs/my-task.yaml
spec:
  what: "Add input validation to the login handler"
  why: "Users can submit empty credentials, bypassing rate limiting"
  constraints:
    - "Do not modify the auth middleware"
  context_files:
    - handlers/login.go
  acceptance_criteria:
    - "Empty email returns 400"
    - "Empty password returns 400"
```

3. Run it:

```bash
baton run --runtime opencode --model kimi --spec .baton/specs/my-task.yaml
```

## Commands

| Command | Description |
|---------|-------------|
| `baton run` | Spawn a task in an external runtime |
| `baton status` | List tasks and their statuses |
| `baton list` | List configured runtimes and availability |
| `baton result <task-id>` | Show task result details |
| `baton monitor` | Live TUI dashboard |
| `baton config get/set` | Read or write config values |
| `baton cost` | Show cost tracking summary |

## Run Flags

```
--runtime        Runtime key from agents.yaml
--model          Model from runtime's model list
--spec           Path to task spec YAML file
--prompt         Inline prompt (requires --skip-validation)
--task-id        Task identifier (default: task-{timestamp})
--context-files  Comma-separated context files (with --prompt)
--timeout        Max time before killing worker (default: 10m)
--json           Output task record as JSON
--auto-route     Auto-select runtime/model from routing rules
--skip-validation  Skip spec validation
```

## Architecture

```
Orchestrator (Claude Code / OpenCode / human)
    |
    | baton run --spec ...
    v
baton CLI (single Go binary)
    |
    +-- Config Loader -----> agents.yaml
    +-- Spec Validator ----> task spec YAML
    +-- Lock Registry -----> locks.yaml
    +-- Runner -----------> subprocess (opencode/aider/pi-agent)
    +-- Event Emitter -----> events.ndjson
    +-- Task Store --------> tasks/*.yaml
    +-- Cost Tracker ------> costs.ndjson
    +-- Decision Store ----> decisions.yaml
    +-- TUI Monitor -------> bubbletea
```

**Filesystem is the IPC protocol.** No servers, no databases, no wire protocols. YAML files for config/specs/tasks, NDJSON for events and costs.

## Three-Tier Model

| Tier | Role | Model | When |
|------|------|-------|------|
| Architect | Planning, hard decisions | Opus | Morning planning, evening review |
| Orchestrator | Daily coordination, delegation | Sonnet | Throughout the day |
| Workers | Code writing | Kimi, DeepSeek, etc. | Per-task via baton |

## Task Specs

Structured YAML specs preserve context across agent boundaries. Required fields:

- **what** -- What the worker must produce
- **why** -- Why this task exists (most important field)
- **constraints** -- What the worker must NOT do
- **context_files** -- Files the worker should read
- **acceptance_criteria** -- How to verify the work

Optional fields: `decisions`, `writes_to`, `examples`, `acceptance_checks`, `criticality`, `related_tasks`.

See [Task.spec.example.yaml](Task.spec.example.yaml) for a complete example.

## Routing Rules

Define routing rules in `agents.yaml` to auto-select runtime and model based on task properties:

```yaml
routing:
  rules:
    - match:
        domain: [frontend, css, ui]
      action: delegate
      runtime: opencode
      model: kimi

    - match:
        criticality: high
      action: escalate
      target: opus
      reason: "Requires deep reasoning"

    - match:
        domain: [tests, boilerplate]
      action: delegate
      model: gemini-flash
```

Use `--auto-route` flag to enable automatic selection:

```bash
baton run --auto-route --spec .baton/specs/my-task.yaml
```

## File Locks

Specs declare `writes_to` paths. Baton prevents parallel tasks from writing to the same files:

```yaml
spec:
  writes_to:
    - src/handlers/login.go
    - migrations/       # trailing slash = directory lock
```

## Cost Tracking

Baton estimates per-task cost based on model rates and duration:

```bash
baton cost            # summary table
baton cost --json     # machine-readable
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Task failed |
| 2 | Config error |
| 3 | Spec validation error |
| 4 | Lock conflict |
| 10 | Needs clarification |
| 124 | Timeout |

## Configuration

Config files are loaded with precedence: `.baton/agents.yaml` (project) > `~/.baton/agents.yaml` (user) > built-in defaults.

See [agents.examples.yaml](agents.examples.yaml) for a complete example.

### OpenCode Configuration

OpenCode requires special configuration because it uses subcommands with positional arguments:

```yaml
runtimes:
  opencode:
    command: "opencode"
    model_flag: "--model"
    context_flag: "--file"
    positional:
      - "run"
      - "{{prompt}}"
    extra_flags:
      - "--dangerously-skip-permissions"
    models:
      - your-model
```

Key points:
- `positional` places args after the command: `opencode run <prompt>`
- `--file` (singular) for context files, not `--files`
- `--dangerously-skip-permissions` skips permission prompts in non-interactive mode

## Runtime Data

All runtime state lives in `.baton/` at the project root:

```
.baton/
  agents.yaml          # project config
  project-brief.md     # prepended to all worker prompts
  specs/               # task spec YAML files
  tasks/               # task records (managed by baton)
  locks.yaml           # file lock registry
  events.ndjson        # append-only event log
  results/
    costs.ndjson       # cost tracking log
    decisions.yaml     # persisted decisions
```

## Supported Runtimes

Any CLI tool that accepts model, prompt, and optional context flags:

| Runtime | Command | Status |
|---------|---------|--------|
| OpenCode | `opencode -m <model> -p <prompt>` | Primary |
| aider | `aider --model <model> --message <prompt>` | Supported |
| pi-agent | `pi-agent --model <model> --prompt <prompt>` | Supported |
| Custom | Any CLI matching the flag pattern | Via config |

## License

MIT
