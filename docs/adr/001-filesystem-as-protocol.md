# ADR-001: Filesystem as IPC Protocol

**Status:** Accepted
**Date:** 2026-04-26

## Context

Baton orchestrates external coding agents (OpenCode, aider, pi-agent) as separate processes. These agents need to receive task instructions, report results, and stream progress events.

Standard IPC options include gRPC, JSON-RPC, WebSocket, MCP, and message queues. All solve problems Baton does not have:

1. **Service discovery** — Workers are CLI binaries in PATH. No registration needed.
2. **Persistent connections** — Each task is a fresh subprocess with a clear start and end.
3. **Bidirectional messaging** — Orchestrator sends a task via CLI args, worker produces output via stdout. Mid-task negotiation is handled via exit-and-retry, not callbacks.

Every target agent runtime already reads and writes files natively. File I/O is the universal capability across all coding agents.

## Decision

Use the filesystem as the sole communication protocol.

- **Task specs**: YAML files in `.baton/specs/`
- **Task records**: YAML files in `.baton/tasks/`
- **Event log**: NDJSON appended to `.baton/events.ndjson`
- **File locks**: `.baton/locks.yaml`
- **Results**: `.baton/results/`
- **Project brief**: `.baton/project-brief.md`

The TUI monitor reads the event log via `fsnotify` — no socket, no polling.

## Consequences

### Positive

- **Zero integration burden.** Any agent that runs shell commands can use Baton. No SDK, no client library.
- **Universal debuggability.** Every piece of state is a human-readable file. `cat .baton/tasks/task-002.yaml` shows exact state.
- **Crash resilience.** All state survives on disk. On restart, scan for stale tasks (dead PIDs) and recover.
- **No infrastructure dependencies.** No Redis, no message broker, no server process.
- **Works in containers, CI, SSH sessions.** No port binding, no firewall rules.

### Negative

- **No interactive mid-task negotiation.** Worker must exit with `needs_clarification` and be re-spawned. Adds seconds of latency vs. milliseconds for a callback.
- **fsnotify platform quirks.** File watching varies across macOS (kqueue), Linux (inotify), and is unreliable on network filesystems.
- **Concurrent write interleaving.** NDJSON lines from parallel tasks need flock to prevent corruption.
- **Scales to ~50 concurrent tasks, not 500.** Sufficient for developer machine use case (5-15 parallel tasks).

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| gRPC | Both sides must implement protobuf schemas. Too high integration burden for CLI tools. |
| MCP | Agents would need MCP server implementations. None of the target agents expose MCP for task execution. |
| Unix domain sockets | Requires Baton to run as a daemon. Files are simpler for process-per-task model. |
| SQLite | Possible, but YAML files are more debuggable and editable. No query needs beyond key-value lookup. |
