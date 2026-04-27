# Baton

Runtime-agnostic multi-agent orchestrator. Single Go binary. Delegates coding tasks from any orchestrator to external agent runtimes (OpenCode, aider, pi-agent) with structured context preservation.

## Build & Run

```bash
go build -o baton .
go test ./...
go test -race ./...
go vet ./...
```

## Project Structure

```
main.go              # entrypoint, cobra root command
cmd/                 # CLI subcommands (run, status, list, result, monitor, config)
internal/
  config/            # YAML config parsing, runtime validation
  spec/              # task spec parsing, validation, prompt building
  runner/            # subprocess lifecycle, timeout, output capture
  task/              # task record CRUD, attempt tracking
  lock/              # advisory file lock registry
  brief/             # project brief loader
  events/            # NDJSON event emitter + tailer
  tui/               # bubbletea monitor (Phase 3)
docs/
  adr/               # architecture decision records
  TED.md             # technical engineering design
```

## Key Design Decisions

- Filesystem is IPC protocol. No wire protocols. YAML files + NDJSON event log.
- Cobra for CLI (MVP). Planned migration to stdlib post-MVP (ADR-010).
- Structured task specs required: `what`, `why`, `constraints`, `context_files`, `acceptance_criteria`.
- Advisory file locks via `writes_to` declarations in specs.
- Convention-based clarification detection (exit codes + stdout pattern matching).
- Three-tier model: Opus architects, Sonnet orchestrates, cheap models implement.

## Runtime Data

All runtime state lives in `.baton/` at project root:
- `agents.yaml` — project config (overrides ~/.baton/agents.yaml)
- `project-brief.md` — prepended to all worker prompts
- `specs/` — task spec YAML files
- `tasks/` — task records (managed by baton)
- `locks.yaml` — file lock registry
- `events.ndjson` — append-only event log

## Conventions

- Go 1.22+
- Error handling: return errors, no panic
- Atomic file writes: temp + os.Rename
- YAML for config/specs/tasks, NDJSON for events
- Exit codes: 0=success, 1=failed, 2=config error, 3=spec validation, 4=lock conflict, 5=acceptance fail, 10=needs clarification, 124=timeout
- No mocks in tests — use mock-runtime shell scripts for integration tests

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/charmbracelet/bubbletea` + `lipgloss` — TUI (Phase 3)
- `github.com/fsnotify/fsnotify` — file watching (Phase 3)
- `gopkg.in/yaml.v3` — YAML parsing

## Build Phases

Phase 1 (MVP): config, spec, runner, task, events, brief + run/status/list/result commands. OpenCode only.
Phase 2: file locks, acceptance checks, examples, criticality.
Phase 3: TUI monitor with bubbletea + fsnotify.
Phase 4: multi-runtime adapters, orchestrator switching.
Phase 5: background exec, git integration, timeout, log rotation, JSON output.
Phase 6: smart routing, cost tracking, decision persistence.

## Reference Docs

- `ARCHITECTURE.md` — full system design (source of truth)
- `docs/TED.md` — technical engineering design with Go struct definitions
- `docs/adr/` — 10 architecture decision records
- `agents.examples.yaml` — example config
- `Task.spec.example.yaml` — example task spec
- `Project.brief.example.md` — example project brief
