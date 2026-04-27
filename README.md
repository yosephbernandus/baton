# Baton

Runtime-agnostic multi-agent orchestrator. Single Go binary. Delegates coding tasks from any orchestrator (Claude Code, OpenCode, human shell) to external agent runtimes (OpenCode, aider, pi-agent) with structured context preservation.

## Why

AI coding agents are powerful but siloed. Claude Code only talks to Anthropic models. OpenCode only talks to its ecosystem. When you want Opus to plan, Sonnet to orchestrate, and cheap models (Kimi, DeepSeek) to write code there's no bridge.

Baton is that bridge. It doesn't plan, reason, or make decisions. It dispatches structured tasks to external runtimes and tracks what happens.

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
Human
  |
  v
Orchestrator (Claude Code / OpenCode / human shell)
  |  bash commands
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

- **what** — What the worker must produce
- **why** — Why this task exists (most important field)
- **constraints** — What the worker must NOT do
- **context_files** — Files the worker should read
- **acceptance_criteria** — How to verify the work

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
