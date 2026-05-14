package worker

import (
	"fmt"
	"strings"

	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/role"
	"github.com/yosephbernandus/baton/internal/spec"
)

type Runtime string

const (
	RuntimeClaudeCode Runtime = "claude-code"
	RuntimeOpenCode   Runtime = "opencode"
	RuntimeGeneric    Runtime = "generic"
)

type InstructionConfig struct {
	Runtime    Runtime
	TaskID     string
	Spec       *spec.Spec
	Phase      phase.Phase
	Complexity string
	BatonBin   string
}

// GenerateInstructions produces the full instruction set for a worker.
func GenerateInstructions(cfg InstructionConfig) string {
	if cfg.BatonBin == "" {
		cfg.BatonBin = "baton"
	}

	core := buildCoreInstructions(cfg)

	switch cfg.Runtime {
	case RuntimeClaudeCode:
		return wrapClaudeCode(core, cfg)
	case RuntimeOpenCode:
		return wrapOpenCode(core, cfg)
	default:
		return core
	}
}

func buildCoreInstructions(cfg InstructionConfig) string {
	var b strings.Builder

	b.WriteString("# Baton Worker Protocol\n\n")

	// Task section
	if cfg.Spec != nil {
		b.WriteString("## Task\n\n")
		if cfg.Spec.What != "" {
			fmt.Fprintf(&b, "**What:** %s\n\n", cfg.Spec.What)
		}
		if cfg.Spec.Why != "" {
			fmt.Fprintf(&b, "**Why:** %s\n\n", cfg.Spec.Why)
		}
		if len(cfg.Spec.Constraints) > 0 {
			b.WriteString("**Constraints:**\n")
			for _, c := range cfg.Spec.Constraints {
				fmt.Fprintf(&b, "- %s\n", c)
			}
			b.WriteString("\n")
		}
		if len(cfg.Spec.AcceptanceCriteria) > 0 {
			b.WriteString("**Acceptance Criteria:**\n")
			for _, ac := range cfg.Spec.AcceptanceCriteria {
				fmt.Fprintf(&b, "- %s\n", ac)
			}
			b.WriteString("\n")
		}
	}

	// Current phase
	b.WriteString("## Current Phase\n\n")
	fmt.Fprintf(&b, "**Phase %d: %s**\n", cfg.Phase.ID, cfg.Phase.Name)
	fmt.Fprintf(&b, "**Role:** %s\n", cfg.Phase.Role)
	fmt.Fprintf(&b, "**Complexity:** %s\n\n", cfg.Complexity)

	if desc := phase.PhaseDescription(cfg.Phase.Name); desc != "" {
		fmt.Fprintf(&b, "**Objective:** %s\n\n", desc)
	}
	if rdesc := phase.RoleDescription(cfg.Phase.Role); rdesc != "" {
		fmt.Fprintf(&b, "%s\n\n", rdesc)
	}

	// Tool boundaries
	b.WriteString("## Tool Boundaries\n\n")
	if boundary := role.BoundaryText(cfg.Phase.Role); boundary != "" {
		fmt.Fprintf(&b, "%s\n\n", boundary)
	}
	if tools := role.AllowedTools(cfg.Phase.Role); len(tools) > 0 {
		fmt.Fprintf(&b, "Allowed tools: %s\n\n", strings.Join(tools, ", "))
	}

	// Protocol commands
	b.WriteString("## Protocol Commands\n\n")
	b.WriteString("Report state by running these commands:\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "# Liveness — run every 60 seconds while working\n")
	fmt.Fprintf(&b, "%s worker heartbeat \"what you are doing\"\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Progress — when you have measurable progress\n")
	fmt.Fprintf(&b, "%s worker progress 50 \"halfway done\"\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Stuck — when blocked, waits for guidance\n")
	fmt.Fprintf(&b, "%s worker stuck \"your question\"\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Complete — when current phase is done\n")
	fmt.Fprintf(&b, "%s worker complete\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Fail — when you cannot complete the phase\n")
	fmt.Fprintf(&b, "%s worker fail \"reason\"\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Observe — record a reflection or learning\n")
	fmt.Fprintf(&b, "%s worker observe \"what you learned\"\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Context — get current task context\n")
	fmt.Fprintf(&b, "%s worker context\n\n", cfg.BatonBin)
	fmt.Fprintf(&b, "# Next — advance to next phase after completing current\n")
	fmt.Fprintf(&b, "%s worker next\n", cfg.BatonBin)
	b.WriteString("```\n\n")

	// Workflow
	b.WriteString("## Workflow\n\n")
	b.WriteString("1. Read the task and phase objective above\n")
	fmt.Fprintf(&b, "2. Run `%s worker heartbeat \"starting phase\"` to signal you're alive\n", cfg.BatonBin)
	b.WriteString("3. Do the work for this phase\n")
	fmt.Fprintf(&b, "4. Report progress periodically: `%s worker progress <pct> \"description\"`\n", cfg.BatonBin)
	fmt.Fprintf(&b, "5. If stuck, run `%s worker stuck \"your question\"` — it will wait for guidance\n", cfg.BatonBin)
	b.WriteString("6. Before completing, answer the mandatory exit questions below via observe commands\n")
	fmt.Fprintf(&b, "7. Run `%s worker complete` when done\n", cfg.BatonBin)
	fmt.Fprintf(&b, "8. Run `%s worker next` to get the next phase\n", cfg.BatonBin)
	b.WriteString("9. If next returns a new phase, continue working. If it says pipeline done, stop.\n\n")

	// Mandatory exit questions
	reflections := PhaseReflections(cfg.Phase.ID, cfg.Phase.Name)
	if len(reflections) > 0 {
		b.WriteString("## Phase Exit Questions (MANDATORY)\n\n")
		b.WriteString("Before completing this phase, record an observation for each:\n\n")
		for i, q := range reflections {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "Record each answer with: `%s worker observe \"your answer\"`\n\n", cfg.BatonBin)
	}

	// Important rules
	b.WriteString("## Rules\n\n")
	b.WriteString("- Do NOT exit with an error if you can ask for help first — use `stuck` command\n")
	b.WriteString("- Do NOT skip mandatory exit questions\n")
	b.WriteString("- Do NOT modify files outside your role boundaries\n")
	b.WriteString("- Report heartbeats every 60 seconds to avoid being marked unresponsive\n")

	return b.String()
}

func wrapClaudeCode(core string, cfg InstructionConfig) string {
	var b strings.Builder

	b.WriteString("# Baton Worker Instructions\n\n")
	b.WriteString("You are a baton worker agent. Follow the protocol below exactly.\n\n")
	b.WriteString("**IMPORTANT:** All protocol commands must be run via Bash tool.\n")
	b.WriteString("Do NOT print BATON: markers to stdout — use the CLI commands instead.\n\n")

	b.WriteString(core)

	b.WriteString("\n## Claude Code Notes\n\n")
	b.WriteString("- Use the `Bash` tool to run all `baton worker` commands\n")
	b.WriteString("- Use `Read` to examine files before editing\n")
	b.WriteString("- Use `Edit` or `Write` for code changes (if your role allows)\n")
	fmt.Fprintf(&b, "- Task ID for all commands: `%s`\n", cfg.TaskID)

	return b.String()
}

func wrapOpenCode(core string, cfg InstructionConfig) string {
	var b strings.Builder

	b.WriteString("# Baton Worker Instructions (OpenCode)\n\n")
	b.WriteString("You are a baton worker agent. Follow the protocol below exactly.\n\n")
	b.WriteString("**IMPORTANT:** All protocol commands must be run via shell execution.\n\n")

	b.WriteString(core)

	b.WriteString("\n## OpenCode Notes\n\n")
	b.WriteString("- Execute all `baton worker` commands via shell\n")
	fmt.Fprintf(&b, "- Task ID for all commands: `%s`\n", cfg.TaskID)

	return b.String()
}
