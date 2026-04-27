# ADR-006: Advisory File Locks via writes_to Declarations

**Status:** Accepted
**Date:** 2026-04-26

## Context

When multiple tasks run in parallel, two workers might modify the same file simultaneously. This creates merge conflicts, data loss, or subtle bugs that are expensive to debug.

The system needs a way to prevent file-level conflicts between parallel tasks. Options:

1. **No parallel execution** — serialize all tasks. Safe but eliminates throughput gains.
2. **OS-level file locks (flock/fcntl)** — workers would need to acquire locks. Requires agent modification.
3. **Advisory locks based on declared intent** — tasks declare which files they'll touch; Baton prevents conflicting tasks from starting.
4. **Git branch isolation** — each worker gets its own branch. Clean but complex merge resolution.

## Decision

Advisory file locks via the `writes_to` field in task specs.

**Lock file:** `.baton/locks.yaml`, managed by Baton.

**Behavior:**
1. Task spec declares `writes_to: [file1, file2, dir/]`
2. Before spawning, Baton checks each path against the lock registry
3. No conflict: acquire locks, spawn worker
4. Conflict: return error with conflicting task ID and duration. Exit code 4.
5. On task completion (any status): release all locks

**Prefix locks:** Trailing slash (`migrations/`) locks the entire directory. Useful when the exact filename is unknown in advance (e.g., new migration files).

**Locks are advisory.** They prevent Baton from spawning conflicting tasks, but do not prevent a worker from touching unlisted files. Workers that touch files not in `writes_to` create undetected conflicts — caught during coherence checkpoints or final review.

## Consequences

### Positive

- **Prevents the most common parallel failure.** Two workers editing the same file is the #1 source of conflicts.
- **Zero agent modification.** Lock checking happens in Baton before spawn. Workers are unaware.
- **Clear error messages.** "cannot start task-003: models/session.go locked by task-002 (running 1m23s)" — orchestrator can decide to wait or rewrite the spec.
- **Prefix locks handle new file creation.** `migrations/` covers files that don't exist yet.
- **Automatic cleanup.** Locks released on task completion regardless of status (success, failed, timeout).

### Negative

- **Advisory, not enforced.** A worker can touch any file regardless of `writes_to`. The declaration is a contract of intent, not a sandbox.
- **Depends on accurate spec writing.** If the orchestrator forgets to list a file in `writes_to`, the lock won't protect it.
- **Stale locks on Baton crash.** If Baton is killed mid-task, locks persist. Mitigated by stale lock cleanup on startup (check if holding PID is still alive).
- **No read locks.** A task reading a file while another modifies it has no protection. Acceptable for the target use case — conflicts are rare and caught at review.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| No parallel execution | Eliminates the throughput advantage of multi-agent delegation. |
| OS-level flock | Requires agents to acquire locks. Breaks the "no agent modification" principle. |
| Git branch per worker | Clean isolation but complex merge resolution. Overkill for 5-15 parallel tasks on one machine. |
| Mandatory file sandboxing | Would require OS-level containers or chroot. Massive complexity increase for marginal benefit. |
