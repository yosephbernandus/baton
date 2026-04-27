# ADR-005: Convention-Based Clarification Detection

**Status:** Accepted
**Date:** 2026-04-26

## Context

When a worker agent is confused about a task, it should signal confusion rather than guess. But Baton targets unmodified external agents — OpenCode, aider, pi-agent — that have no built-in protocol for signaling clarification needs.

Options for detecting worker confusion:
1. **Modify each agent** to support a clarification API — high integration burden, fragile across agent updates
2. **Convention-based detection** — use exit codes and output pattern matching, no agent modifications needed
3. **Structured output parsing** — require workers to emit JSON status — agents don't support this

## Decision

Three-layer convention-based detection, no agent modifications required:

**Layer 1 — Exit code.** Configurable code (default: 10) means `needs_clarification` rather than `failed`.

**Layer 2 — Output pattern matching.** Baton scans worker stdout for configurable patterns:
```yaml
clarification_patterns:
  - "I'm not sure"
  - "unclear which"
  - "could you clarify"
  - "ambiguous"
  - "multiple possible"
  - "CLARIFICATION_NEEDED"
```
Pattern detected AND non-zero exit = `needs_clarification`.

**Layer 3 — Explicit marker.** Baton appends this to every worker prompt:
```
If you are uncertain about any aspect of this task, output the line:
CLARIFICATION_NEEDED: <your question here>
and exit. Do NOT guess.
```

This gives the worker a clear convention to follow without requiring any agent-side code changes.

## Consequences

### Positive

- **Zero agent modifications.** Works with any CLI-based agent today and tomorrow.
- **Configurable patterns.** Users can add model-specific uncertainty phrases to agents.yaml.
- **Prompt injection of marker.** The `CLARIFICATION_NEEDED:` instruction works with any LLM — it's a prompt convention, not an API contract.
- **Graceful degradation.** If detection fails (false negative), the task just shows as `failed` instead of `needs_clarification`. Orchestrator still reviews and can re-delegate.

### Negative

- **False positives.** A worker explaining ambiguity in valid output ("the API is ambiguous about...") could trigger detection. Mitigated by requiring non-zero exit AND pattern match together.
- **False negatives.** Models express uncertainty differently. "I'm unable to determine" won't match default patterns. Requires pattern tuning per model.
- **No structured clarification.** The worker's question is extracted from stdout text, not a structured field. Quality depends on how well the model follows the `CLARIFICATION_NEEDED:` format.
- **Exit code 10 is arbitrary.** Some runtimes might use exit code 10 for other purposes. Configurable to mitigate.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Agent-specific plugins | Each agent would need a baton plugin. High maintenance, breaks on agent updates. |
| Structured JSON output | Agents don't reliably produce structured output on demand. |
| Always re-run on failure | Wastes API cost. Better to distinguish "confused" from "broken". |
