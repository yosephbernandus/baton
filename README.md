# Baton

Runtime-agnostic multi-agent orchestrator. Coordinates coding tasks across Claude Code, OpenCode, aider, and any CLI agent with structured context, file locking, and a 16-phase pipeline.

## Install

```bash
# One-line install (macOS/Linux)
curl -fsSL https://raw.githubusercontent.com/yosephbernandus/baton/main/install.sh | sh

# Or: go install
go install github.com/yosephbernandus/baton@latest

# Or: build from source
git clone https://github.com/yosephbernandus/baton.git && cd baton && go build -o baton .
```

macOS and Linux only (Unix domain sockets required).

## Quick Start: First Task in 5 Minutes

```bash
# 1. Scaffold — auto-detects installed runtimes
baton setup

# 2. Add your project context
vim .baton/project-brief.md

# 3. Create a task spec from a description
baton plan "add JWT authentication to the API"

# 4. Fill in the spec (why, constraints, acceptance criteria)
vim .baton/specs/add-jwt-authentication-to-the-api.yaml

# 5. Initialize the coordinator
baton init .baton/specs/add-jwt-authentication-to-the-api.yaml

# 6. Copy instructions and start Claude Code
cp .baton/tasks/add-jwt-authentication-to-the-api/CLAUDE.md CLAUDE.md
# Claude Code reads CLAUDE.md → plans → dispatches to OpenCode → reviews → done
```

Or dispatch directly (single-shot, no coordinator):

```bash
baton run --runtime opencode --model kimi --spec .baton/specs/my-task.yaml
```

## What Baton Does

```
You (Human) <-> Claude Code (Opus) <-> OpenCode (Worker)
   discuss       plan/review            implement
```

**Coordinator flow** — Claude Code (Opus) is the intelligent orchestrator:
- Plans and designs (phases 1-7) — does itself
- Dispatches implementation (phase 8) — sends to OpenCode via `baton run`
- Reviews and tests (phases 9-15) — does itself, dispatches test writing
- Handles retries (L1: same phase, L2: loop back to implementation)
- Completes (phase 16)

**What baton provides:**
- File locking — prevents parallel agents from overwriting each other
- Structured specs — what/why/constraints/acceptance criteria survive handoffs
- Scratchpad — previous attempt context carries forward on retry
- Role boundaries — reviewer can't edit code, developer can't skip tests
- Event log — everything tracked in `.baton/events.ndjson`

## Commands

| Command | Description |
|---------|-------------|
| `baton setup` | Scaffold `.baton/` with auto-detected runtimes |
| `baton plan <description>` | Generate task spec from description |
| `baton init <spec>` | Initialize task and generate coordinator CLAUDE.md |
| `baton doctor` | Verify runtime configs work (`--probe` to test live) |
| `baton run` | Dispatch a task to an external runtime |
| `baton status` | List tasks and statuses |
| `baton result <task-id>` | Show task result |
| `baton monitor` | Live TUI dashboard |
| `baton worker *` | Worker protocol commands (start, complete, next, retry, etc.) |
| `baton pipeline run` | Headless/CI mode (no human coordinator) |
| `baton cost` | Cost tracking summary |

## Verify Your Setup

```bash
baton doctor          # Check runtimes exist and configs are valid
baton doctor --probe  # Actually spawn each runtime with a test prompt
```

## Clearing the Monitor

```bash
> .baton/events.ndjson    # Clear events
rm -rf .baton/tasks/      # Clear task state
```

## Further Reading

| Doc | Purpose |
|-----|---------|
| [docs/reference.md](docs/reference.md) | Config reference, LLM integration guide, troubleshooting |
| [docs/user-guide.md](docs/user-guide.md) | Workflow guide: brainstorm → ADR → task spec |
| [docs/conversational-worker-protocol.md](docs/conversational-worker-protocol.md) | IPC protocol details |
| [agents.examples.yaml](agents.examples.yaml) | Full config example |
| [Task.spec.example.yaml](Task.spec.example.yaml) | Full spec example |
| [docs/adr/](docs/adr/) | Architecture decision records (001-020) |

## License

MIT
