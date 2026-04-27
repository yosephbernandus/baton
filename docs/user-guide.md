# User Guide: From Brainstorm to Baton Task

This guide shows how to use Baton effectively in your daily workflow. It teaches you how to go from raw LLM discussion to a well-structured task spec that produces predictable results.

## The Problem Baton Solves

When you brainstorm with an LLM, context degrades:

```
You (100% context) -> LLM (70%) -> Another LLM (40%) -> Code (30%)
```

Baton stops this decay by requiring **structured task specs** that preserve context across agent boundaries.

## The Daily Workflow

```
┌─────────────────────────────────────────────────────────────────────┐
│  1. BRAINSTORM          2. DECIDE            3. SPEC               │
│  Raw discussion          ADR (if needed)     Structured YAML        │
│  with LLM               or skip              for baton              │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│  4. RUN                                                                │
│  baton run --spec .baton/specs/my-task.yaml                          │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Step 1: Brainstorm with an LLM

Start a free-form discussion. No structure needed yet.

**Example session:**

```
You: I want to add auto-expiring sessions to our API
LLM: Sure, we could add a expires_at column to sessions
You: But we need it to work with the cleanup job that's already running
LLM: Oh right, the cleanup job queries for expires_at. So we need that column.
You: And we can't touch the existing session_v1 schema because other services depend on it
LLM: Got it. We need a session_v2 table then.
You: And we need ULIDs for IDs since that's our project standard
LLM: Makes sense. I'll create a migration for user_sessions_v2.
```

**At this point, you have:**
- A clear goal (auto-expiring sessions)
- A dependency (cleanup job needs expires_at)
- A constraint (can't touch session_v1)
- A decision already made (session_v2 with ULIDs)

---

## Step 2: Decide Whether to Create an ADR

**Create an ADR when:**
- The decision affects multiple parts of the codebase
- The decision is irreversible or hard to change later
- Other developers need to understand the decision
- The decision settles a debate that might otherwise reoccur

**Skip ADR when:**
- The decision is task-specific and contained
- The change is straightforward implementation
- You're iterating on an existing design

**From our example:** The session_v2 vs v1 decision is significant because it affects schema design across the project. Create an ADR.

```markdown
# ADR-011: Session Auto-Expiry via TTL

**Status:** Accepted
**Date:** 2026-04-27

## Context
The cleanup job (task-005) runs periodically to delete expired sessions. It currently
has nothing to query against because sessions don't have an expiry column.

## Decision
Create a new `user_sessions_v2` table (not modifying session_v1) with an `expires_at`
column for TTL-based auto-expiry. Use ULIDs for IDs per project standard.

## Consequences
- Positive: Cleanup job can query `WHERE expires_at < NOW()` efficiently
- Negative: Dual schema maintenance until v1 deprecation

## Decisions Made
- session_v2 (not v1): TTL field required
- ULID: Per ARCHITECTURE.md project standard
```

---

## Step 3: Write the Task Spec

Now distill everything from brainstorming + ADR into a structured spec.

```yaml
# .baton/specs/session-auto-expiry.yaml
spec:
  what: |
    Create migration 003_user_sessions_v2 that creates the user_sessions_v2
    table with expires_at column for TTL-based auto-expiry.

  why: |
    The cleanup job (task-005) depends on expires_at to auto-delete expired
    sessions. Without this migration, the cleanup job has nothing to query
    against. This enables the Phase 2 auto-expiry feature.

  constraints:
    - "Use session_v2 schema, NOT session_v1 (other services depend on v1)"
    - "Follow migration pattern in migrations/002_users.go exactly"
    - "Do NOT modify schema.go -- only create a new migration file"
    - "Do NOT touch any files in routes/ or handlers/"
    - "Table name must be user_sessions_v2 (matching project convention)"

  context_files:
    - migrations/002_users.go
    - docs/phase2-spec.md
    - docs/adr/011-session-auto-expiry.md

  related_tasks:
    - task_id: task-005
      status: running
      summary: "Cleanup job queries expires_at column"

  decisions:
    - question: "session_v1 or session_v2?"
      answer: "v2"
      reason: "TTL field required; v1 used by other services"
      decided_by: human

  writes_to:
    - migrations/003_user_sessions_v2.go

  acceptance_criteria:
    - "Migration file created at migrations/003_user_sessions_v2.go"
    - "Creates user_sessions_v2 table with columns: id, user_id, token, created_at, expires_at"
    - "id is ULID primary key"
    - "user_id is foreign key to users table"
    - "go build passes with no errors"

  criticality: medium
```

---

## Step 4: Run the Task

```bash
baton run --runtime opencode --model kimi --spec .baton/specs/session-auto-expiry.yaml
```

Baton will:
1. Validate the spec (ensure all required fields present)
2. Check file locks (prevent conflicts with parallel tasks)
3. Spawn the worker with context from project-brief.md + spec
4. Emit events to events.ndjson for auditing
5. Track costs based on model and duration

---

## Why This Works

### The "why" is the most important field

Without it, workers make technically correct but contextually wrong decisions:

| Without `why` | With `why` |
|---------------|------------|
| "I'll add the expires_at column" | "I need expires_at for the cleanup job" |
| Might modify session_v1 | Knows to create session_v2 |
| Might skip indexes | Creates composite index for cleanup query |

### Constraints prevent common mistakes

Workers don't know your project conventions unless you tell them. Constraints like:
- "Do NOT modify schema.go"
- "Follow migration pattern in 002_users.go"

Prevent rework and frustrated back-and-forth.

### Context files give workers what they need

Don't make workers hunt for patterns. Give them:
- Files to read
- Examples to mimic
- Decisions already made

### Related tasks prevent conflicts

If another task is modifying handlers that depend on this migration, the worker knows not to break them.

---

## Quick Reference: Required vs Optional Fields

### Required (Baton validates these)

| Field | Purpose |
|-------|---------|
| `what` | What the worker must produce |
| `why` | Why this task exists (most important) |
| `constraints` | What NOT to do |
| `context_files` | Files worker should read |
| `acceptance_criteria` | How to verify work |

### Optional (but recommended)

| Field | Purpose |
|-------|---------|
| `decisions` | Prevents re-litigating settled questions |
| `writes_to` | File locking to prevent conflicts |
| `related_tasks` | Cross-task awareness |
| `examples` | Concrete code snippets to mimic |
| `acceptance_checks` | Automated verification commands |
| `criticality` | Risk level for review depth |

---

## Common Mistakes

### 1. Skipping the "why"

```yaml
# BAD: Worker doesn't know why this matters
what: "Add expires_at column"

# GOOD: Worker understands the context
why: "The cleanup job needs expires_at to auto-delete expired sessions"
```

### 2. Vague constraints

```yaml
# BAD: Too vague
constraints:
  - "Be careful with existing code"

# GOOD: Specific
constraints:
  - "Do NOT modify routes/auth.go"
  - "Do NOT touch middleware/ratelimit.go"
```

### 3. Missing context files

```yaml
# BAD: Worker has to guess
context_files:
  - src/  # too broad

# GOOD: Specific files
context_files:
  - migrations/002_users.go
  - handlers/session.go
```

### 4. No acceptance criteria

```yaml
# BAD: How do you verify success?
acceptance_criteria: []

# GOOD: Clear pass/fail conditions
acceptance_criteria:
  - "go build passes"
  - "Migration creates user_sessions_v2 table"
```

---

## Project Brief Setup

Place at `.baton/project-brief.md`. It's prepended to every worker prompt.

```markdown
# Project Brief
Project: MyApp
Language: Go 1.22

## Conventions
- IDs: ULID (never UUID)
- Naming: snake_case DB cols, camelCase Go
- Migrations: sequential numbering, one file per migration

## Patterns
- Errors: always return, never panic
- DB: sqlx with named queries
- Logging: slog with structured fields

## Decisions
- Auth: JWT with RS256 (decided 2026-04-01)
- Session: v2 schema with TTL (ADR-011)
```

---

## Next Steps

1. **Install Baton:** `go install github.com/yosephbernandus/baton@latest`
2. **Initialize project:** `mkdir -p .baton && cp agents.examples.yaml .baton/agents.yaml`
3. **Create project-brief:** `.baton/project-brief.md`
4. **Start small:** Pick one task, write the spec, run it with baton
5. **Iterate:** Adjust your spec format based on what works for your team
