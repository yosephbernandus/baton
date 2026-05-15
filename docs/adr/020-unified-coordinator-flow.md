# ADR-020: Unified Coordinator-First Execution Flow

**Status:** Accepted
**Date:** 2026-05-15

## Context

Baton accumulated three execution modes over its development phases:

1. **`baton run`** (Phase 1 MVP): Single-shot subprocess dispatch. Loads config/spec, calls `runner.Run()` once. No phase awareness.
2. **`baton pipeline run`** (Phase 7): 16-phase deterministic pipeline. Baton spawns workers as subprocesses via `runner.Run()` per phase. Contains L1/L2 retry logic, loop detection, scratchpad, advisor. The orchestrator is Go code.
3. **`baton worker`** (Phase 8): Conversational worker protocol. Workers run independently and call `baton worker` CLI commands to advance through phases. Filesystem-based state.

All three solve the same problem differently: execute a task through phases with workers. The user's actual workflow is:
- Human discusses with Claude Code (Opus) as coordinator
- Coordinator plans (lead phases), dispatches implementation to OpenCode (developer phases), reviews results (reviewer phases)
- This is one flow, not three modes

### Problems

- **Pipeline mode is automated but unintelligent.** Every phase gets a generic prompt. No human-in-the-loop. The Go orchestrator can't reason about WHY a phase failed.
- **Worker mode is smart but lacks resilience.** No L1 retry, no L2 loopback, no scratchpad injection, no budget enforcement.
- **Run mode has no phase awareness.** Useful only as a building block, but presented as a primary command.
- **Users must choose** between automated-but-dumb and smart-but-fragile. No way to get both.

## Decision

Unify into a single **coordinator-first flow** where an LLM (Claude Code / Opus) is the intelligent orchestrator. Baton generates coordinator instructions (CLAUDE.md) that teach the LLM to execute the full pipeline.

### `baton init` — Primary Entry Point

New top-level command that initializes a task and generates coordinator instructions:

```bash
baton init spec.yaml --complexity MEDIUM
```

Generates a CLAUDE.md that teaches the coordinator to:
- Execute lead/reviewer phases itself
- Dispatch developer/tester phases to other runtimes via `baton run`
- Handle L1 retries, L2 loopbacks, scratchpad context
- Answer mandatory phase reflection questions

### Role of Each Command After Unification

| Command | Role |
|---------|------|
| `baton init` | Primary entry point. Generates coordinator CLAUDE.md |
| `baton worker *` | State protocol. Both coordinator and workers call these |
| `baton run` | Dispatch tool. Coordinator sends work to other runtimes |
| `baton pipeline run` | Headless/CI mode. Fully automated, no human coordinator |

### Worker Protocol Extensions

Added `retry`, `loopback`, and `status` commands to give the coordinator the same resilience primitives that `pipeline run` has in Go:

- `baton worker retry <task-id>` — L1 retry with scratchpad injection
- `baton worker loopback <task-id> 8` — L2 cycle back to implementation
- `baton worker status <task-id>` — Budget summary (L1/L2 remaining)

### Coordinator Instruction Generator

New `internal/coordinator/` package generates CLAUDE.md with 11 sections:
1. Identity and role
2. Task context (from spec)
3. Phase table with Self/Dispatch column
4. Self-phase protocol (baton worker commands)
5. Dispatch protocol (baton run commands)
6. L1 retry protocol with budget
7. L2 loop protocol
8. Stuck/escalation protocol
9. Command reference
10. Per-phase guidance
11. Mandatory reflection questions

### Dispatch Map

The coordinator instruction generator determines which phases the coordinator does itself vs. dispatches based on:
- Lead/reviewer/test_lead roles → always Self
- Developer/tester roles → dispatch if `role_models` config maps them to a different runtime
- Same runtime as orchestrator → Self (coordinator can do it)

## Consequences

### Positive

- **One conceptual flow.** Users learn one model: init → coordinate → dispatch → review → complete.
- **Intelligent orchestrator.** Opus reasons about failures, adjusts retry strategy, provides specific guidance.
- **Human in the loop.** Coordinator is a Claude Code session. Human can intervene anytime.
- **Pipeline mode preserved.** CI/automation still works via `baton pipeline run`.
- **Minimal code changes.** Heavy lifting is instruction generation, not refactoring.
- **Worker protocol stronger.** Retry/loopback/status benefit all modes.

### Negative

- **Depends on LLM instruction-following.** Coordinator must follow structured retry/loopback rules. Mitigation: `baton worker` commands enforce budget constraints.
- **Two instruction generators.** Worker instructions (`internal/worker/`) and coordinator instructions (`internal/coordinator/`). Both pull from same source packages.
- **Three modes still in codebase.** Conceptual overhead. Mitigation: docs position `baton init` as primary.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Remove pipeline mode | Loses headless/CI capability |
| Make pipeline call LLM for retry decisions | Mixes deterministic and nondeterministic in one loop |
| Put retry logic in `baton worker next` automatically | Removes coordinator's agency to decide retry strategy |
| Generate shell script instead of CLAUDE.md | Scripts can't reason about failures |

## New/Modified Files

| File | Purpose |
|------|---------|
| `internal/coordinator/coordinator.go` | Dispatch map + main instruction generator |
| `internal/coordinator/sections.go` | 11 section builders |
| `internal/coordinator/coordinator_test.go` | Tests |
| `cmd/init.go` | Top-level `baton init` command |
| `internal/worker/protocol.go` | Added Retry, Loopback, Status functions |
| `internal/session/manifest.go` | Added RemainingL1Retries, RemainingL2Cycles helpers |
| `cmd/worker.go` | Added retry, loopback, status subcommands |
| `main.go` | Registered init command |
