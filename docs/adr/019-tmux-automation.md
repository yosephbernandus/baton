# ADR-019: tmux-Based Multi-Window Automation (Future Improvement)

**Status:** Proposed (deferred)  
**Date:** 2026-05-15

## Context

Baton's conversational worker protocol (ADR-012, extended in this release) requires workers to run in their own terminal sessions. Currently, the human manually starts each agent (Claude Code, OpenCode, etc.) and points it at the generated instructions.

For single-worker setups this is fine. For multi-worker pipelines (e.g., lead → developer → reviewer running concurrently on different phases), manually managing terminals becomes tedious.

tmux is a terminal multiplexer available on macOS and Linux that can programmatically create, manage, and destroy terminal sessions. It could automate the "open terminal + start agent" step.

## Decision

**Defer tmux integration.** Design it as an optional layer ON TOP of the conversational worker protocol. Never as a replacement.

The worker protocol must function without tmux. tmux adds convenience, not capability.

## What tmux Would Add

```bash
# Spawn a worker in a new tmux window
baton tmux spawn <task-id> --runtime claude-code

# Attach to a worker's tmux window (for human observation)
baton tmux attach <task-id>

# Split-pane dashboard showing all active workers
baton tmux dashboard

# Kill a worker's tmux session
baton tmux kill <task-id>
```

### Spawn Flow

```
baton tmux spawn auth-task --runtime claude-code
  1. Creates tmux window named "baton-auth-task"
  2. Copies generated instructions to .baton/tasks/auth-task/CLAUDE.md
  3. Runs: cd <project-root> && claude
  4. Claude Code reads CLAUDE.md, starts calling baton worker commands
  5. baton watches task directory via fsnotify (same as manual mode)
```

### Dashboard Layout

```
┌─────────────────────────────┬─────────────────────────────┐
│ baton-auth-task              │ baton-api-task              │
│ Phase 8: Implementation     │ Phase 3: Discovery          │
│ [claude running]            │ [opencode running]          │
│                             │                             │
├─────────────────────────────┴─────────────────────────────┤
│ baton-watcher                                             │
│ [watching all tasks, showing events]                      │
└───────────────────────────────────────────────────────────┘
```

## Architecture

tmux is a **launcher**, not a protocol change. The worker still calls `baton worker` commands. baton just automates the "open terminal + start agent" step.

```
baton tmux spawn <task-id>
    │
    ├─ tmux new-window -n baton-{task-id}
    ├─ tmux send-keys "cd <project-root>" Enter
    ├─ Copy instructions to correct location for runtime
    ├─ tmux send-keys "<runtime-command>" Enter
    │
    └─ Worker runs, calls baton worker commands
       └─ Same protocol as manual mode
```

## Why Deferred

1. **Protocol must work without tmux first.** The conversational worker protocol is the foundation. tmux is ergonomic sugar.

2. **Platform dependency.** Not all environments have tmux (CI/CD, containers, Windows via WSL). The protocol must be portable.

3. **Manual mode is simpler to debug.** When something goes wrong, having direct terminal access is easier than debugging through tmux abstraction.

4. **Incremental addition.** tmux can be added without any protocol changes — it's purely additive. No existing code needs modification.

5. **Agent SDK alternative.** Claude Code's Agent SDK may provide a better programmatic interface than tmux session management. Wait and evaluate.

## When to Implement

Implement tmux support when:
- Multi-worker pipelines are common (>3 concurrent workers)
- Manual terminal management becomes the primary user complaint
- The conversational worker protocol is proven stable (>1 month in production use)

## Consequences

### Positive (when implemented)
- One-command multi-worker setup
- Visual dashboard for monitoring
- No manual terminal juggling for complex pipelines

### Negative
- tmux dependency (optional, but still)
- Session naming conflicts possible
- Detached sessions may accumulate if not cleaned up
- Additional testing surface for tmux interactions

### Alternatives Considered

| Alternative | Why Deferred |
|---|---|
| Docker containers per worker | Too heavy, slow startup, complex networking |
| Background processes (nohup) | No terminal for human observation |
| Screen instead of tmux | tmux is more widely used, better scripting API |
| VS Code terminal API | IDE-specific, not portable |
| Agent SDK | Not yet stable enough for production use |
