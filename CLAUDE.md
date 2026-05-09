# Baton

Runtime-agnostic multi-agent orchestrator. Single Go binary. Two modes: simple task dispatch to external agent runtimes (OpenCode, aider, Claude Code) and 16-phase deterministic pipeline with role enforcement, retry loops, and self-annealing.

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
cmd/                 # CLI subcommands (run, status, list, result, monitor, config, kill, wait,
                     #   respond, defer, escalate, cost, progress, guide, pipeline, session,
                     #   advise, feedback, anneal)
internal/
  config/            # YAML config parsing, runtime validation, pipeline/advisor/annealing config
  spec/              # task spec parsing, validation, prompt building (injects BATON: marker protocol)
  runner/            # subprocess + socket server lifecycle, stdout marker parsing, tool restriction flags
  proto/             # BATON: marker parser (H/P/S/E/M/C/N markers) + JSON socket message types
  socket/            # Unix domain socket server + client
  task/              # task record CRUD, attempt tracking
  phase/             # 16-phase pipeline machine, phase definitions, prompt builder, scratchpad, loop detection
  role/              # five-role definitions (lead/developer/reviewer/test_lead/tester), boundary verification
  skill/             # domain skill router, context file loading, domain inference
  session/           # pipeline session manifest, crash recovery (atomic YAML)
  advisor/           # escalation advisor (opt-in LLM consultation when stuck)
  feedback/          # event log miner, runtime/phase metrics, pattern detection
  annealing/         # self-annealing config patch generation, risk-gated suggestions
  lock/              # advisory file lock registry
  brief/             # project brief loader
  events/            # NDJSON event emitter + tailer + query
  git/               # git snapshot + change detection
  tui/               # bubbletea monitor
docs/
  adr/               # architecture decision records (ADR 001-018)
  TED.md             # technical engineering design
```

## Key Design Decisions

- Three-layer IPC: stdout markers (worker→baton), Unix socket (baton↔orchestrator), filesystem audit trail. See ADR-013.
- macOS + Linux only (Unix domain sockets required). No Windows support.
- Cobra for CLI (MVP). Planned migration to stdlib post-MVP (ADR-010).
- Structured task specs required: `what`, `why`, `constraints`, `context_files`, `acceptance_criteria`.
- Advisory file locks via `writes_to` declarations in specs.
- Convention-based clarification detection (exit codes + stdout pattern matching).
- 16-phase deterministic pipeline with complexity-based phase skipping (ADR-015).
- Five-role system (lead/developer/reviewer/test_lead/tester) with three-level enforcement: prompt, runtime flags, post-hoc verification (ADR-016).
- Scratchpad per task prevents Groundhog Day on retries. Loop detection via Jaccard similarity (ADR-017).
- Self-annealing: event log mining → pattern detection → risk-gated config patches (ADR-018).

## Runtime Data

All runtime state lives in `.baton/` at project root:
- `agents.yaml` — project config (overrides ~/.baton/agents.yaml)
- `project-brief.md` — prepended to all worker prompts
- `specs/` — task spec YAML files
- `tasks/` — task records + per-task `baton.sock` + `inbox.ndjson` + `scratchpad.md`
- `skills/` — domain context directories (backend/, frontend/, etc.)
- `locks.yaml` — file lock registry
- `events.ndjson` — append-only event log
- `session.yaml` — pipeline session manifest (crash recovery)
- `feedback/` — analysis output from event log mining
- `annealing/` — generated config patches

## Conventions

- Go 1.22+
- Error handling: return errors, no panic
- Atomic file writes: temp + os.Rename
- YAML for config/specs/tasks, NDJSON for events
- Exit codes: 0=success, 1=failed, 2=config error, 3=spec validation, 4=lock conflict, 5=acceptance fail, 10=needs clarification, 124=timeout
- No mocks in tests — use mock-runtime shell scripts for integration tests

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/charmbracelet/bubbletea` + `lipgloss` — TUI
- `github.com/fsnotify/fsnotify` — file watching (TUI)
- `gopkg.in/yaml.v3` — YAML parsing
- Go stdlib `net` — Unix domain sockets (no external IPC deps)

## Build Phases

Phase 1 (MVP): config, spec, runner, task, events, brief + run/status/list/result commands. OpenCode only.
Phase 2: file locks, acceptance checks, examples, criticality.
Phase 3: TUI monitor with bubbletea + fsnotify.
Phase 4: multi-runtime adapters, orchestrator switching.
Phase 5: background exec, git integration, timeout, log rotation, JSON output.
Phase 6: smart routing, cost tracking, decision persistence.
Phase 7: 16-phase pipeline, roles, scratchpad, loop detection, L1/L2 retries.
Phase 8: skill routing, escalation advisor, session manifest, self-annealing.

## Integrating Baton Into a Project

If user asks to integrate baton into their project, read `INTEGRATION.md` — it has the complete step-by-step guide: runtime config, project brief, task specs, orchestration patterns, feedback loop commands, and troubleshooting.

## Reference Docs

- `INTEGRATION.md` — step-by-step guide for integrating baton into any project (includes pipeline mode)
- `ARCHITECTURE.md` — full system design (source of truth)
- `llms.txt` — LLM-friendly project overview
- `orchestrator-prompt.md` — system prompt template for LLMs using baton as orchestrator
- `docs/conversational-worker-protocol.md` — IPC protocol details (stdout markers + socket + inbox)
- `docs/TED.md` — technical engineering design with Go struct definitions
- `docs/user-guide.md` — workflow guide from brainstorm to task spec
- `docs/adr/` — 18 architecture decision records
- `agents.examples.yaml` — example config
- `Task.spec.example.yaml` — example task spec
- `Project.brief.example.md` — example project brief
