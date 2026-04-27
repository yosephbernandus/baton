# ADR-008: Orchestrator-Agnostic Design

**Status:** Accepted
**Date:** 2026-04-26

## Context

The AI coding tool landscape is fragmented and evolving rapidly. Claude Code, OpenCode, pi-agent, Cursor CLI, Codex — any of these could be the user's primary tool, and the dominant tool may change within months.

If Baton assumes a specific orchestrator, it becomes tightly coupled to that tool's lifecycle, limitations, and breaking changes. Users switching orchestrators would need to re-learn a different workflow.

Additionally, some orchestrators have unique strengths: OpenCode offers 2M context windows for large codebases, Claude Code offers deep Anthropic integration, and a human can orchestrate directly from the shell for simple task sets.

## Decision

Baton does not know or care who calls it. Any entity that can execute bash commands is a valid orchestrator.

**Supported orchestration modes:**

```
Mode 1: Claude Code (Sonnet orchestrates, Opus architects)
  Human <-> Opus (morning) <-> writes specs
  Human <-> Sonnet (day) <-> baton <-> workers

Mode 2: OpenCode orchestrates (2M context for large codebases)
  Human <-> OpenCode (Kimi) <-> baton <-> workers

Mode 3: Human orchestrates directly
  Human <-> baton <-> workers
```

The `orchestrator` field in agents.yaml is informational, not enforced:
```yaml
orchestrator:
  runtime: claude-code
  model: sonnet
```

Routing rules, checkpoint intervals, and critical review targets are read by the orchestrator from config — Baton does not enforce them. The orchestrator is trusted to follow the rules.

## Consequences

### Positive

- **Tool-agnostic.** Users can switch orchestrators without changing Baton config or workflow.
- **Future-proof.** New agent CLIs can orchestrate immediately. No Baton update needed.
- **Human-in-the-loop.** A human can run `baton run` directly from the shell. Useful for debugging, one-off tasks, or when no AI orchestrator is available.
- **Composable.** Different orchestrators for different phases — Opus for planning, Sonnet for execution, OpenCode for large codebase tasks.

### Negative

- **No orchestrator enforcement.** Routing rules are advisory. The orchestrator can ignore them. Mistakes (delegating high-criticality to a cheap model) are not prevented by Baton.
- **No orchestrator state management.** Baton doesn't track which orchestrator is active or manage orchestrator handoffs. The human is responsible for switching.
- **Config is advisory.** The `checkpoint_interval`, `critical_review`, and routing rules are documentation for the orchestrator, not enforced policy. A forgetful orchestrator skips coherence checkpoints.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Claude Code-specific tool | Couples to one provider. Users on OpenCode or future tools are excluded. |
| Baton as orchestrator | Makes Baton a competitor to existing agents. The agent tool space is crowded; Baton's value is coordination, not conversation. |
| Enforced routing | Baton would need to understand task domains and complexity. That's the orchestrator's job. |
