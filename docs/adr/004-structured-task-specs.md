# ADR-004: Structured Task Specs for Context Preservation

**Status:** Accepted
**Date:** 2026-04-26

## Context

Context degrades at each hop in multi-agent delegation:

```
Human (100% context)
  -> Orchestrator (70-80% with unstructured handoff)
    -> Worker (40-60% with ad-hoc prompting)
```

The primary failure mode: orchestrators write casual prompts that drop critical information. Workers then make technically correct but contextually wrong decisions because they lack the "why" behind the task.

Unstructured prompts fail in predictable ways:
- Missing constraints ("don't touch routes/") leads to workers modifying shared files
- Missing decisions ("use v2 schema") leads to workers re-litigating settled questions
- Missing related task awareness leads to parallel workers conflicting

## Decision

Every delegated task carries a structured YAML spec with required and optional fields:

**Required fields:**
- `what` — What the worker must produce
- `why` — Why this task exists (most important field — prevents contextually wrong decisions)
- `constraints` — What NOT to do (must be present, can be empty array)
- `context_files` — Specific files worker needs to read (validated to exist on disk)
- `acceptance_criteria` — How the orchestrator verifies the work (at least one item)

**Optional fields:**
- `related_tasks` — What other workers are doing (prevents conflicts)
- `decisions` — Audit trail of choices already made (prevents re-litigation)
- `writes_to` — Files this task will create/modify (used for file locking)
- `examples` — Concrete code snippets to mimic (raises first-attempt accuracy from ~70% to ~85%)
- `acceptance_checks` — Automated commands run after worker exits
- `criticality` — Risk level (low/medium/high) determining review depth

Baton validates specs before spawning workers. Missing required fields return errors with specific field names.

## Consequences

### Positive

- **Context preservation: 75-85% at worker level** vs. 40-60% with ad-hoc prompts.
- **Forced completeness.** The template makes it impossible to forget the "why" — baton rejects specs without it.
- **Decision persistence.** Human escalation decisions propagate to workers via the `decisions` field. No information loss across retry cycles.
- **Self-contained execution.** Workers get everything they need in one spec. No ambient context dependency.
- **Reproducible delegation.** Same spec produces same results. Debuggable when things go wrong.

### Negative

- **Spec writing overhead.** Orchestrator must fill structured fields instead of writing a quick prompt. Mitigated by Opus writing specs directly during morning planning.
- **Rigidity.** Some quick tasks don't need full specs. Mitigated by `--skip-validation` for inline prompts.
- **Schema evolution.** Adding new fields requires updating validation logic and documentation. Mitigated by making most fields optional.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Free-form prompts only | Primary cause of context degradation. Workers lack constraints, decisions, and cross-task awareness. |
| JSON specs | YAML is more human-readable and editable. Orchestrators and humans will read/write specs frequently. |
| Spec-per-runtime templates | Runtime-specific templates fragment the spec format. One universal format is simpler. |
