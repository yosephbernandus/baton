# Conversational Worker Protocol

Reference: [ADR-013](adr/013-unix-socket-ipc.md)

## Overview

Three-layer IPC enabling real-time bidirectional communication between workers, baton, and orchestrators. Replaces the filesystem-only mailbox approach (ADR-012).

**Platform:** macOS + Linux only (Unix domain sockets required).

## Architecture

```
┌────────────┐     Unix socket      ┌────────────┐     stdout markers    ┌────────────┐
│Orchestrator│◄────────────────────►│  baton run │◄─────────────────────│   Worker   │
│(Claude Code│  JSON-line protocol  │            │  BATON:X:payload      │ (opencode) │
│  or human) │                      │            │                       │            │
└────────────┘                      │            │───── inbox.ndjson ───►│            │
                                    └────────────┘     (file-based)      └────────────┘
```

## Layer 1: Stdout Markers (worker → baton)

Workers print compact markers to stdout. Parsed by baton's runner in real-time.

```
BATON:H:what you are doing              heartbeat — emit every 60 seconds
BATON:P:30:implementing auth            progress — percent:description
BATON:S:your specific question          stuck — when blocked, needs help
BATON:E:what failed                     error — something broke
BATON:M:what you completed              milestone — subtask done
```

**Why stdout?** Workers are unmodified CLI tools. Stdout is the only universal output channel. Text markers are token-efficient — LLMs pay per character.

**Parser:** `internal/proto/marker.go` — detects `BATON:` prefix, splits on `:`, returns typed `Marker` struct.

## Layer 2: Unix Domain Socket (baton ↔ clients)

Per-task socket at `.baton/tasks/{id}/baton.sock`. Created by `baton run`, destroyed on completion.

### Server → clients (push, broadcast to all connected)

```json
{"m":"heartbeat","msg":"reading codebase"}
{"m":"progress","p":30,"msg":"implementing auth"}
{"m":"stuck","msg":"which schema v1 or v2?"}
{"m":"error","msg":"build failed","detail":"undefined reference"}
{"m":"milestone","msg":"all tests passing"}
{"m":"completed","dur":"45s","files":["main.go"]}
{"m":"failed","code":1,"msg":"exit code 1"}
```

### Client → server (requests)

```json
{"m":"guide","id":1,"msg":"use v2 schema","from":"human"}
{"m":"abort","id":2}
```

### Server → client (responses)

```json
{"m":"ok","id":1}
```

**Implementation:** `internal/socket/server.go` + `internal/socket/client.go`. Pure Go stdlib (`net`, `bufio`, `encoding/json`).

## Layer 3: Filesystem Audit Trail

- `events.ndjson` — all worker_* events persisted for TUI replay and debugging
- `task.yaml` — task record with status, output tail, escalation state
- `inbox.ndjson` — guidance messages for worker to read

Filesystem layer is the crash-safe permanent record. Socket is ephemeral.

## Guidance Delivery

Workers can't listen on sockets (they're arbitrary CLI tools). Guidance path:

```
orchestrator calls: baton guide <id> --msg "use v2"
    → socket client connects to .baton/tasks/{id}/baton.sock
    → sends: {"m":"guide","id":1,"msg":"use v2","from":"orchestrator"}
    → baton socket server receives
    → writes to .baton/tasks/{id}/inbox.ndjson
    → responds: {"m":"ok","id":1}
    → worker reads inbox.ndjson (per prompt instructions)
    → worker continues with guidance
```

## Prompt Injection

`spec.BuildPromptWithProtocol()` appends these instructions to every worker prompt:

```
[COMMUNICATION PROTOCOL]
Report progress by printing these exact markers to stdout:

  BATON:H:what you are doing          (heartbeat — every 60 seconds)
  BATON:P:30:implementing auth        (progress — percent:description)
  BATON:S:your specific question      (stuck — when blocked)
  BATON:E:what failed                 (error — when something breaks)
  BATON:M:what you completed          (milestone — subtask done)

When stuck, print BATON:S and wait up to 60 seconds.
Check {taskDir}/inbox.ndjson for guidance.
If guidance arrives, follow it and continue.
If no guidance in 60 seconds, exit with code 10.
```

## Sequences

### Happy Path (no intervention)

```
Time  Worker (stdout)              baton run                    Orchestrator
────  ──────────────               ─────────                    ────────────
0:00  BATON:H:starting             → events + socket broadcast
0:01  BATON:P:20:reading code      → events + socket broadcast  (watching via progress --watch)
0:03  BATON:P:60:implementing      → events + socket broadcast
0:05  BATON:M:tests passing        → events + socket broadcast
0:06  exit 0                       → task completed
```

### Worker Stuck → Guidance via Socket

```
Time  Worker (stdout)              baton run                    Orchestrator
────  ──────────────               ─────────                    ────────────
0:00  BATON:H:starting             → broadcast
0:02  BATON:S:v1 or v2?            → broadcast "stuck"          sees stuck msg
                                                                runs: baton guide --msg "v2"
0:02  (waiting, checks inbox)      ← socket: guide msg
                                   → writes inbox.ndjson
                                   → replies: ok
0:03  reads inbox, continues
0:05  BATON:M:done                 → broadcast
0:06  exit 0                       → task completed
```

### Worker Stuck → No Response → Fallback

```
Time  Worker                       baton run                    Orchestrator
────  ──────                       ─────────                    ────────────
0:00  BATON:H:starting
0:02  BATON:S:question?            → broadcast                  (not connected)
0:02  (waiting 60s...)
0:03  no guidance arrives
0:03  exit 10                      → needs_clarification        (later) reads status, re-runs
```

Graceful degradation to ADR-011 clarification flow.

## CLI Usage

```bash
# Monitor historical progress (from events.ndjson)
baton progress <task-id>

# Live stream via socket
baton progress <task-id> --watch

# Send guidance to stuck worker
baton guide <task-id> --msg "use v2 schema"
baton guide <task-id> --msg "use monorepo" --by human
```

## Compatibility Matrix

| Worker Behavior | Result |
|---|---|
| Prints BATON: markers + reads inbox | Full bidirectional communication |
| Prints BATON: markers, ignores inbox | Progress visible, guidance unread → exits code 10 |
| Prints nothing (legacy behavior) | Falls back to timeout + exit code detection (ADR-005) |

System degrades gracefully. No worker modification required for basic operation.
