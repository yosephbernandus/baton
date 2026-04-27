# ADR-003: Three-Tier Execution Model (Opus/Sonnet/Workers)

**Status:** Accepted
**Date:** 2026-04-26

## Context

Claude Pro plan provides limited Opus quota (~5-10 messages/day) and generous Sonnet quota. Using Opus for everything — planning, delegation, code review, and implementation — exhausts quota by afternoon. Most coding tasks (migrations, boilerplate, tests, scaffolding) don't need Opus-level reasoning.

A two-tier model (Opus orchestrates, cheap models implement) wastes Opus quota on mechanical coordination: verifying context files exist, updating task statuses, running `baton run`. Sonnet handles that fine.

## Decision

Three tiers with distinct roles:

**Tier 1 — Architect (Opus, Claude Code):** High-level planning, writes task spec YAML files directly, architecture decisions, final audit of critical tasks. 2-3 interactions/day.

**Tier 2 — Orchestrator (Sonnet, Claude Code):** Daily coordination, delegation via `baton run`, worker output review, routine escalation, coherence checkpoints. 20-30 interactions/day.

**Tier 3 — Workers (Kimi/DeepSeek/Gemini via OpenCode/pi-agent/aider):** Code writing, implementation, tests, boilerplate. Unlimited, ~$0.50-2/day API cost.

Critical design choice: **Opus writes actual spec YAML files directly** during morning planning. Sonnet's job becomes mechanical — verify, delegate, review. Sonnet does not reinterpret Opus's intent.

## Consequences

### Positive

- **5x throughput within same cost budget.** 8-12 delegated tasks/day vs. 3-5 Opus-direct tasks before throttling.
- **Opus quota preserved for high-value work.** Architecture decisions, security review, critical task audit.
- **Sonnet quota is generous.** 20-30 coordination messages fit well within Pro plan limits.
- **Worker cost is negligible.** $1-2/day for heavy API use on Kimi/DeepSeek.
- **Accuracy recoverable.** Reviews catch issues (80% -> 93% -> 97%). Quota is not recoverable.

### Negative

- **~2% accuracy gap vs. Opus-direct.** Acceptable because Opus-direct is also not 100%.
- **Requires morning planning discipline.** Opus must produce complete specs upfront. Ad-hoc Opus messages mid-day break the quota budget.
- **Context boundary at Opus-to-Sonnet handoff.** Mitigated by Opus writing specs directly (not verbal summaries that Sonnet reinterprets).
- **Rigid daily cadence.** Mid-day surprises require either human clarification (free) or spending an Opus message.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Two-tier (Opus + Workers) | Burns Opus quota on mechanical coordination. Sonnet handles delegation fine. |
| Sonnet-only orchestration | Loses Opus's superior architectural reasoning and spec quality. Critical for task decomposition. |
| No tiers (single model) | Either expensive (Opus for everything) or low quality (cheap model for everything). |
