# ADR-002: Go Single Binary with Minimal Dependencies

**Status:** Accepted
**Date:** 2026-04-26

## Context

Baton is a developer tool installed on individual machines, invoked per-task by the orchestrator. The implementation language affects distribution, startup time, dependency management, and cross-platform support.

## Decision

Implement in Go with four external dependencies:

- `cobra` — CLI framework
- `bubbletea` + `lipgloss` — TUI for `baton monitor`
- `fsnotify` — Cross-platform file watching
- `gopkg.in/yaml.v3` — YAML parsing

No agent frameworks (LangChain, CrewAI). No ORMs. No unnecessary abstractions.

## Consequences

### Positive

- **Single binary distribution.** `go build` produces one file. Install is `cp baton /usr/local/bin/`.
- **~5ms startup.** Orchestrator can invoke `baton run` dozens of times per session without perceptible delay. Python equivalent adds ~200ms per invocation.
- **Cross-compilation.** `GOOS=linux GOARCH=amd64 go build` from macOS. Trivial CI/CD.
- **bubbletea is production-grade.** Used by GitHub CLI, Charm tools. Go TUI ecosystem is more mature than Rust's ratatui.
- **Minimal attack surface.** Four dependencies, no transitive tree sprawl.

### Negative

- **Verbose YAML handling.** Explicit struct tags, more boilerplate than Python's dictionary access or Rust's serde.
- **Error handling verbosity.** `if err != nil { return err }` throughout. Acceptable for ~3-5K lines.
- **No async/await.** Goroutines work differently from async patterns. Not a problem for subprocess management.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Rust | Compile times slow prototyping feedback loop. bubbletea more mature than ratatui for TUI. Sufficient for a tool that mostly shells out. |
| Python | Requires runtime + venv. Slow startup (~200ms). Distribution painful. |
| TypeScript/Node | Requires Node runtime. `node_modules` bloat. Startup overhead. |
