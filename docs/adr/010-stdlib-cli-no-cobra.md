# ADR-010: Stdlib CLI Instead of Cobra

**Status:** Proposed (post-MVP)
**Date:** 2026-04-26
**Supersedes:** Cobra recommendation in ADR-002

## Context

ADR-002 selected Go with minimal dependencies. Original design assumed `cobra` for CLI framework. On review, Baton has exactly 6 flat subcommands:

```
baton run
baton status
baton list
baton result
baton monitor
baton config
```

No nested subcommands. No complex flag interactions. No shell completion requirement for MVP. Cobra pulls in `pflag`, optionally `viper`, and adds ~15 transitive dependencies for functionality Baton doesn't need.

Project principle: "No frameworks, no heavy dependencies." Cobra contradicts this for a 6-command CLI.

## Decision

Use Go stdlib only for CLI parsing. No cobra, no urfave/cli, no kong.

**Implementation pattern:**

```go
func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }

    switch os.Args[1] {
    case "run":
        cmdRun(os.Args[2:])
    case "status":
        cmdStatus(os.Args[2:])
    case "list":
        cmdList(os.Args[2:])
    case "result":
        cmdResult(os.Args[2:])
    case "monitor":
        cmdMonitor(os.Args[2:])
    case "config":
        cmdConfig(os.Args[2:])
    default:
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
        printUsage()
        os.Exit(1)
    }
}
```

Each command uses `flag.NewFlagSet` for its own flags:

```go
func cmdRun(args []string) {
    fs := flag.NewFlagSet("run", flag.ExitOnError)
    runtime := fs.String("runtime", "", "runtime key from agents.yaml")
    model := fs.String("model", "", "model from runtime's model list")
    spec := fs.String("spec", "", "path to task spec YAML")
    prompt := fs.String("prompt", "", "inline prompt")
    taskID := fs.String("task-id", "", "task identifier")
    skipValidation := fs.Bool("skip-validation", false, "skip spec validation")
    timeout := fs.String("timeout", "10m", "max time before kill")
    fs.Parse(args)
    // ...
}
```

Help text via `printUsage()` function — ~30 lines of hardcoded output. Updated manually when commands change. Acceptable for 6 commands.

## Consequences

### Positive

- **Zero external dependencies for CLI.** Entire command layer is stdlib. `go.sum` stays minimal.
- **No transitive dependency risk.** Cobra's dependency tree includes pflag, mousetrap (Windows), and optional viper chain. None needed.
- **Faster compile.** Fewer packages to compile. Marginal but real during development iteration.
- **Full control.** Flag parsing, help text, error messages — all explicit. No framework magic to debug.
- **Consistent with project philosophy.** "No frameworks beyond what the problem requires."

### Negative

- **Manual help text.** Must update `printUsage()` when adding commands or flags. Cobra auto-generates this. Acceptable at 6 commands.
- **No shell completion.** Cobra generates bash/zsh/fish completions. Stdlib does not. Can add later via standalone script if users request it.
- **`flag` uses single-dash long flags.** `-runtime` not `--runtime`. Go's `flag` package doesn't support GNU-style double-dash. Workaround: accept both by registering aliases, or accept single-dash as convention.
- **No built-in version command.** Trivial to add manually via `case "version"` or `-version` flag.

### Double-dash workaround

Go `flag` parses `-runtime` and `--runtime` identically — both work. The `flag` package strips leading dashes and matches the registered name. Users can type either form.

### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Cobra | Pulls 15+ transitive deps for 6 flat commands. Over-engineered. |
| urfave/cli | Lighter than cobra but still external dep. Stdlib sufficient. |
| kong | Elegant struct-based parsing. Still external dep. Not justified for flat command set. |
| pflag standalone | GNU-style flags without cobra. Still a dep for marginal benefit. |

### Migration Path

If Baton grows to 15+ commands or needs nested subcommands, revisit. Migrating from stdlib switch+flagset to cobra is straightforward — each `cmdX` function becomes a cobra command handler. No architectural lock-in.
