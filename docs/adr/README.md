# Architecture Decision Records

ADRs for Baton — a runtime-agnostic multi-agent orchestrator.

## Index

| ADR | Title | Status |
|-----|-------|--------|
| [001](001-filesystem-as-protocol.md) | Filesystem as IPC Protocol | Accepted |
| [002](002-go-single-binary.md) | Go Single Binary with Minimal Dependencies | Accepted |
| [003](003-three-tier-model.md) | Three-Tier Execution Model (Opus/Sonnet/Workers) | Accepted |
| [004](004-structured-task-specs.md) | Structured Task Specs for Context Preservation | Accepted |
| [005](005-convention-based-clarification.md) | Convention-Based Clarification Detection | Accepted |
| [006](006-advisory-file-locks.md) | Advisory File Locks via writes_to Declarations | Accepted |
| [007](007-runtime-adapter-pattern.md) | Runtime Adapter Pattern via CLI Flags | Accepted |
| [008](008-orchestrator-agnostic.md) | Orchestrator-Agnostic Design | Accepted |
| [009](009-ndjson-event-log.md) | NDJSON Event Log with fsnotify Tailing | Accepted |
| [010](010-stdlib-cli-no-cobra.md) | Stdlib CLI Instead of Cobra | Proposed (post-MVP) |

## Format

Each ADR follows: Status, Context, Decision, Consequences (positive + negative), Alternatives Considered.
