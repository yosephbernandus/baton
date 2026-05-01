# Integrating Baton Into Your Project

This guide is for LLMs and developers. If someone says "integrate baton into my project", follow this guide.

## What Baton Does

Baton is a multi-agent orchestrator. It dispatches coding tasks to external AI agent runtimes (Claude Code, OpenCode, aider, pi-agent, etc.) with structured context, tracks their output, and manages the feedback loop between orchestrator, workers, and humans.

---

## Interactive Setup (for LLMs)

When a user asks you to integrate baton into their project, DO NOT generate config immediately. First, run this interview to understand their setup. Ask these questions one at a time or in small groups:

### Question 1: Check Prerequisites
Before asking anything, verify baton is installed:
```bash
baton --version
```
If not installed, help them install:
```bash
# Go install
go install github.com/yosephbernandus/baton@latest
# Or one-line install
curl -fsSL https://raw.githubusercontent.com/yosephbernandus/baton/main/install.sh | sh
```

### Question 2: Detect Available Runtimes
Check which agent runtimes are installed on their system:
```bash
which opencode 2>/dev/null && echo "opencode: available" || echo "opencode: not found"
which claude 2>/dev/null && echo "claude-code: available" || echo "claude-code: not found"
which aider 2>/dev/null && echo "aider: available" || echo "aider: not found"
which pi-agent 2>/dev/null && echo "pi-agent: available" || echo "pi-agent: not found"
which codex 2>/dev/null && echo "codex: available" || echo "codex: not found"
```

Then ask the user:
> I detected these agent runtimes on your system: [list].
> Which ones do you want to use with baton? Are there others I missed?

### Question 3: Agent Roles
Ask:
> How many agents do you want to set up? What role should each agent play?
>
> Common patterns:
> 1. **Single worker** — one runtime handles everything
> 2. **Orchestrator + workers** — one LLM plans/delegates, others execute
> 3. **Specialized workers** — different agents for different domains (frontend, backend, tests, etc.)
>
> Which pattern fits your workflow?

### Question 4: Models Per Runtime
For each runtime the user selected, ask:
> What models do you want to use with [runtime]?
>
> For example:
> - **OpenCode**: kimi-k2.5, minimax-m2.7, deepseek, gemini-flash
> - **Claude Code**: claude-sonnet-4-6, claude-opus-4-6
> - **Aider**: gpt-4o, deepseek, claude-sonnet
>
> Which models are available/configured for your [runtime]?

### Question 5: Orchestrator Selection
Ask:
> Which agent should be the orchestrator (the one that plans and delegates)?
>
> The orchestrator:
> - Reads task results via `baton result --output`
> - Dispatches work via `baton run`
> - Handles clarification routing
> - Usually runs the most capable model (Opus, Sonnet, GPT-4o)
>
> Which runtime + model should orchestrate?

### Question 6: Task Routing (Optional)
Ask:
> Do you want automatic task routing? This means baton auto-selects the runtime/model based on the task type.
>
> Example routing rules:
> - Frontend/UI tasks → OpenCode/Kimi (fast, good at UI)
> - Backend/API tasks → OpenCode/DeepSeek (good at systems code)
> - Tests/boilerplate → cheapest model (Gemini Flash)
> - Architecture/security → escalate to Opus
>
> Want me to set up routing rules? If yes, what domains map to which agents?

### Question 7: Project Context
Ask:
> Tell me about your project so I can write the project brief:
> - What's the project name?
> - What language(s) and framework(s)?
> - Any important coding conventions workers should follow?
> - What's the current focus/priority?

### After the Interview

Once you have answers, generate:
1. `.baton/` directory structure
2. `.baton/agents.yaml` — configured with their runtimes, models, and routing
3. `.baton/project-brief.md` — with project context and baton CLI commands
4. `.gitignore` additions
5. A sample task spec for their first task

Then explain: "You can now run `baton run --spec .baton/specs/<task>.yaml` to dispatch your first task."

---

## Manual Setup (step-by-step)

If you prefer to set up manually or need to understand each file:

### Prerequisites

Install baton:
```bash
go install github.com/yosephbernandus/baton@latest
# or
curl -fsSL https://raw.githubusercontent.com/yosephbernandus/baton/main/install.sh | sh
```

Install at least one agent runtime:
- `opencode` — works with Kimi, DeepSeek, MiniMax, Nvidia models
- `claude` (Claude Code CLI) — works with Anthropic models
- `aider` — works with GPT-4o, DeepSeek, Claude
- `pi-agent` — works with Gemini, Grok

### Step 1: Create .baton/ Directory

```bash
mkdir -p .baton/specs .baton/tasks .baton/results
```

### Step 2: Create agents.yaml

Create `.baton/agents.yaml` with your available runtimes. Adapt this template to match what you have installed:

```yaml
# Who orchestrates (the LLM coordinating workers)
orchestrator:
  runtime: claude-code    # or opencode, etc.
  model: sonnet           # or opus, kimi, etc.

# Available runtimes
runtimes:
  # OpenCode (supports many model providers)
  opencode:
    command: opencode
    model_flag: "--model"
    context_flag: "--file"
    positional:
      - run
      - "{{prompt}}"
    extra_flags:
      - "--dangerously-skip-permissions"
    workdir: inherit
    models:
      - minimax-coding-plan/MiniMax-M2.7-highspeed
      - nvidia/moonshotai/kimi-k2.5
      # Add your available models

  # Claude Code (Anthropic models)
  claude-code:
    command: claude
    model_flag: "--model"
    extra_flags:
      - "--print"
      - "--dangerously-skip-permissions"
    positional:
      - "{{prompt}}"
    workdir: inherit
    models:
      - claude-sonnet-4-6
      - claude-opus-4-6

  # Aider (if installed)
  # aider:
  #   command: aider
  #   model_flag: "--model"
  #   prompt_flag: "--message"
  #   extra_flags:
  #     - "--yes"
  #     - "--no-auto-commits"
  #   workdir: inherit
  #   models:
  #     - gpt-4o

# Defaults when flags omitted
defaults:
  runtime: opencode
  model: minimax-coding-plan/MiniMax-M2.7-highspeed

# Routing rules (optional — auto-select runtime by task type)
routing:
  rules:
    - match:
        domain: [frontend, css, ui]
      action: delegate
      runtime: opencode
      model: kimi
    - match:
        domain: [tests, boilerplate]
      action: delegate
      runtime: opencode
      model: gemini-flash
    - match:
        default: true
      action: delegate
      runtime: opencode
      model: minimax-coding-plan/MiniMax-M2.7-highspeed
  checkpoint_interval: 3
  critical_review: opus

# Clarification detection
clarification_patterns:
  - CLARIFICATION_NEEDED
  - "I'm not sure"
  - "could you clarify"
clarification_exit_code: 10

# Paths
event_log: .baton/events.ndjson
task_dir: .baton/tasks
result_dir: .baton/results
spec_dir: .baton/specs
lock_file: .baton/locks.yaml
project_brief: .baton/project-brief.md
default_timeout: "10m"
output_tail_lines: 50
```

### Runtime Configuration Notes

- **OpenCode**: Use `positional: ["run", "{{prompt}}"]`. Context flag is `--file` (singular). Add `--dangerously-skip-permissions` for non-interactive mode.
- **Claude Code**: Use `positional: ["{{prompt}}"]` with `--print` flag. Do NOT use `--bare` (breaks auth).
- **Aider**: Use `prompt_flag: "--message"` with `--yes --no-auto-commits`.
- **Custom runtime**: Any CLI that accepts a model flag and prompt works. Configure command, flags, and models.

## Step 3: Create Project Brief

Create `.baton/project-brief.md`. This is prepended to EVERY worker prompt. Include:

```markdown
# Project Brief

Project: <project name>
Language: <primary language(s)>
Framework: <framework(s)>

## Conventions
- <coding conventions workers must follow>
- <naming conventions>
- <error handling patterns>

## Baton CLI (orchestrator must use these)

**Dispatch:** `baton run --spec .baton/specs/<task>.yaml --task-id <id>`
**Read output:** `baton result <id> --output` (ALWAYS read after task completes)
**Wait:** `baton wait <id1> <id2>` (block until done, use for parallel tasks)
**Blocked tasks:** `baton result <id> --clarify-context` → then `baton respond <id> --answer "..." --resume` or `baton escalate <id> --reason "..."`
**Park:** `baton defer <id>` | **Kill:** `baton kill <id>` | **Cost:** `baton cost`

## Agent Workflow
- **Orchestrator** (<runtime>/<model>): Plans, delegates, reviews
- **Workers** (<runtime>/<model>): Execute tasks via baton
```

The baton CLI section is critical — it tells the orchestrator LLM how to use the feedback loop.

## Step 4: Create Your First Task Spec

Create `.baton/specs/<task-name>.yaml`:

```yaml
spec:
  what: |
    <What the worker must produce. Be specific.>

  why: |
    <Why this task exists. Most important field.
    Without it, workers make technically correct but contextually wrong decisions.>

  constraints:
    - "<What the worker must NOT do>"
    - "<Files/patterns to avoid>"

  context_files:
    - <path/to/relevant/file>

  acceptance_criteria:
    - "<How to verify the work>"

  # Optional but recommended:
  writes_to:
    - <path/to/file/worker/will/edit>

  decisions:
    - question: "<settled question>"
      answer: "<answer>"
      reason: "<why>"
      decided_by: human

  acceptance_checks:
    - command: "<shell command to verify>"
      expect_exit: 0
      description: "<what it checks>"

  criticality: medium  # low | medium | high
```

## Step 5: Run Your First Task

```bash
# Run a structured task
baton run --spec .baton/specs/my-task.yaml --task-id task-001

# Or quick inline task (skip validation)
baton run --runtime opencode --model kimi --skip-validation \
  --prompt "Add input validation to login handler" --task-id task-002

# Monitor in TUI
baton monitor

# Check status
baton status
```

## Full Command Reference

### Dispatch & Monitor
| Command | Description |
|---------|-------------|
| `baton run --spec <path>` | Dispatch structured task |
| `baton run --prompt "..." --skip-validation` | Quick inline task |
| `baton status` | List all tasks |
| `baton status --filter running` | Filter by status |
| `baton monitor` | Live TUI dashboard |
| `baton list` | Show available runtimes |
| `baton cost` | Cost tracking summary |

### Read Results
| Command | Description |
|---------|-------------|
| `baton result <id>` | Task metadata |
| `baton result <id> --output` | Worker stdout (last 50 lines) |
| `baton result <id> --output-full` | Full stdout from event log |
| `baton result <id> --json` | Machine-readable record |
| `baton result <id> --clarify-context` | Decision context for blocked task |

### Feedback Loop
| Command | Description |
|---------|-------------|
| `baton wait <id> [id...]` | Block until tasks complete |
| `baton wait <id> --any` | Return when first task finishes |
| `baton respond <id> --answer "..." --resume` | Answer and re-dispatch |
| `baton respond <id> --by orchestrator --answer "..."` | Orchestrator auto-answer |
| `baton defer <id>` | Park task until ready |
| `baton escalate <id> --reason "..."` | Escalate to human |

### Control
| Command | Description |
|---------|-------------|
| `baton kill <id>` | Kill a running task |
| `baton kill --all` | Kill all running tasks |

## Orchestration Patterns

### Pattern 1: Sequential
```bash
baton run --spec specs/step1.yaml --task-id s1
baton result s1 --output
# Read output, decide next step
baton run --spec specs/step2.yaml --task-id s2
```

### Pattern 2: Parallel Dispatch + Wait
```bash
baton run --spec specs/server.yaml --task-id w1 &
baton run --spec specs/sdk.yaml --task-id w2 &
baton run --spec specs/dashboard.yaml --task-id w3 &
baton wait w1 w2 w3
baton result w1 --output
baton result w2 --output
baton result w3 --output
```

### Pattern 3: Clarification Flow
```bash
# Worker blocks with needs_clarification
baton result task-1 --clarify-context
# Has enough context? Auto-answer:
baton respond task-1 --by orchestrator --answer "use v2 schema" --resume
# Needs human? Escalate:
baton escalate task-1 --reason "multiple valid approaches, need product decision"
# Human responds later:
baton respond task-1 --answer "use v2 schema" --resume
```

### Pattern 4: Auto-Route by Domain
```bash
# Routing rules in agents.yaml auto-select runtime/model
baton run --auto-route --spec specs/frontend-task.yaml --task-id fe-1
baton run --auto-route --spec specs/backend-task.yaml --task-id be-1
```

## .gitignore

Add to your project's `.gitignore`:
```
.baton/tasks/
.baton/events.ndjson*
.baton/locks.yaml
.baton/results/
```

Keep in git:
```
.baton/agents.yaml
.baton/project-brief.md
.baton/specs/
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "Not logged in" with claude-code | Remove `--bare` from extra_flags. Run `claude auth status` to verify. |
| Worker completes but no output | Use `baton result <id> --output-full` to check event log |
| Task stuck as "running" | `baton kill <id>` — handles dead PIDs and process groups |
| Worker chats instead of executing | Model may not support tool-use in run mode. Try a different model. |
| Lock conflict | Another task holds the file lock. Check `baton status` for running tasks. |
