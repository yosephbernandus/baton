# ADR-007: Runtime Adapter Pattern via CLI Flags

**Status:** Accepted
**Date:** 2026-04-26

## Context

Baton must target multiple agent runtimes: OpenCode, aider, pi-agent, and future tools not yet released. Each runtime has its own CLI interface with different flag names for model selection, prompt input, and context file specification.

Options for runtime integration:
1. **Hardcoded adapters** — one Go function per runtime with embedded knowledge of its CLI
2. **Plugin system** — dynamic loading of runtime adapters
3. **Declarative flag mapping** — YAML config that maps generic concepts to runtime-specific flags

## Decision

Declarative flag mapping in `agents.yaml`. Each runtime is defined by its command and flag names:

```yaml
runtimes:
  opencode:
    command: "opencode"
    model_flag: "-m"
    prompt_flag: "-p"
    context_flag: "--files"
    workdir: "inherit"
    models: [kimi, deepseek, gemini-flash]

  aider:
    command: "aider"
    model_flag: "--model"
    prompt_flag: "--message"
    context_flag: ""
    extra_flags: ["--yes", "--no-auto-commits"]
    workdir: "inherit"
    models: [gpt-4o, deepseek, claude-sonnet]
```

Baton constructs subprocess commands by reading config:
```
baton run --runtime opencode --model kimi --spec task.yaml
  -> opencode -m kimi -p "<constructed prompt>" --files file1,file2
```

Adding a new runtime = adding a YAML block to config. No code changes.

## Consequences

### Positive

- **New runtime = config entry.** When a new agent CLI ships, users add it to agents.yaml. No Baton code change, no release cycle.
- **User-customizable.** Users can override flags, add extra flags, or define custom model lists per project.
- **Transparent.** The constructed command is logged in the event log. Users can see exactly what Baton ran.
- **Testable.** Config parsing is pure data transformation. Easy to unit test flag construction.

### Negative

- **Lowest common denominator.** The flag mapping covers `model`, `prompt`, `context_files`, and `extra_flags`. Runtimes with richer interfaces (interactive modes, streaming APIs) are reduced to basic CLI invocation.
- **Context file format varies.** Some runtimes want comma-separated, some want repeated flags, some want @ file lists. The `context_flag` abstraction may need per-runtime formatting logic.
- **No runtime-specific features.** aider's `/add`, Cursor's apply model, OpenCode's conversation mode — none are accessible through the generic adapter.
- **Runtime binary detection.** `baton list` checks if the command is in PATH. Unreliable if runtimes are installed via different package managers or version managers.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Hardcoded adapters | Requires code changes and new releases for each runtime. Violates runtime-agnostic principle. |
| Plugin system (Go plugins / Wasm) | Over-engineered for CLI flag mapping. Go plugin system has significant limitations (same Go version, same OS). |
| REST API adapters | Runtimes are CLI tools, not servers. Would require wrapper services around each runtime. |
