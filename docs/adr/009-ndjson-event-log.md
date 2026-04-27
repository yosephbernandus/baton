# ADR-009: NDJSON Event Log with fsnotify Tailing

**Status:** Accepted
**Date:** 2026-04-26

## Context

The TUI monitor needs real-time visibility into task progress. The orchestrator needs to check task status. Post-mortem debugging needs a complete history of what happened.

These are three different access patterns:
1. **Real-time streaming** — monitor needs new events as they happen
2. **Point-in-time query** — orchestrator checks current status of a specific task
3. **Historical analysis** — debugging and review of past sessions

Options for event storage:
- SQLite — good for queries, requires driver, binary format
- Structured log files (JSON) — one file per task, no streaming
- NDJSON append log — one file, append-only, streamable, human-readable
- Redis Streams — real-time capable but infrastructure dependency

## Decision

Single NDJSON (Newline-Delimited JSON) append log at `.baton/events.ndjson`.

**Format:** One JSON object per line, appended atomically.
```jsonl
{"ts":"...","task_id":"task-002","event":"task_started","data":{"pid":45231}}
{"ts":"...","task_id":"task-002","event":"output","data":{"stream":"stdout","line":"Reading..."}}
{"ts":"...","task_id":"task-002","event":"task_completed","data":{"exit_code":0,"duration":"1m23s"}}
```

**Event types:** `task_created`, `task_started`, `output`, `file_changed`, `task_completed`, `task_failed`, `task_timeout`, `needs_clarification`, `needs_human`, `human_decided`, `re_delegated`, `lock_acquired`, `lock_released`, `lock_conflict`, `acceptance_check_passed`, `acceptance_check_failed`.

**Streaming:** The TUI monitor tails the file using `fsnotify`. New lines appear at filesystem-write latency (microseconds).

**Concurrent writes:** Protected by flock or channel-based serialization when multiple tasks write simultaneously.

**Rotation:** 10MB max (configurable), keep last 3 files.

## Consequences

### Positive

- **Human-readable.** `tail -f .baton/events.ndjson` works. No special tooling for debugging.
- **Append-only simplicity.** No update-in-place, no transactions, no schema migrations.
- **Real-time via fsnotify.** Monitor gets events at filesystem latency without polling or sockets.
- **grep-friendly.** `grep task-002 events.ndjson` extracts one task's history instantly.
- **Language-agnostic.** Any tool that reads lines of JSON can consume the log.

### Negative

- **No indexed queries.** Finding all events for a task requires scanning the file. Acceptable at target scale (<10K events/day).
- **File size growth.** High-output workers (many stdout lines) can grow the log quickly. Mitigated by rotation at 10MB.
- **Concurrent write ordering.** Two tasks writing simultaneously can interleave at the byte level without flock. Events are timestamped so logical ordering is recoverable.
- **No built-in retention policy beyond rotation.** Old rotated files accumulate. Users must clean up manually or via cron.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| SQLite | Good for queries but binary format. Can't `tail -f`. Can't `grep`. Requires driver dependency. |
| One file per task | No real-time streaming of cross-task activity. Monitor would need to watch many files. |
| Structured logging library (zerolog/zap) | Adds dependency for what is essentially `file.WriteString(json + "\n")`. Over-engineered. |
| Redis Streams | Infrastructure dependency. Violates "no external services" principle. |
