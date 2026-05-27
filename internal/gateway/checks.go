package gateway

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/yosephbernandus/baton/internal/brief"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/lock"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/spec"
)

func CheckRuntimeAvailable(cfg *config.Config, runtimeName string) []Finding {
	if cfg == nil || runtimeName == "" {
		return nil
	}
	if !cfg.RuntimeAvailable(runtimeName) {
		cmd := runtimeName
		if rt, ok := cfg.Runtimes[runtimeName]; ok {
			cmd = rt.Command
		}
		return []Finding{{
			Check:    "runtime_available",
			Severity: SeverityError,
			Message:  fmt.Sprintf("runtime %q command %q not found in PATH", runtimeName, cmd),
		}}
	}
	return nil
}

func CheckPromptBudget(cfg *config.Config, s *spec.Spec) []Finding {
	if cfg == nil || s == nil {
		return nil
	}

	budgetTokens := cfg.PhaseMachine.ContextBudgetTokens
	if budgetTokens <= 0 {
		budgetTokens = 180000
	}

	projectBrief := brief.Load(cfg.ProjectBrief)
	basePrompt := spec.BuildPrompt(s, projectBrief)
	tokens := phase.EstimateTokens(basePrompt)

	estimatedTotal := int(float64(tokens) * 1.2)

	if estimatedTotal > budgetTokens {
		return []Finding{{
			Check:    "prompt_budget",
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"initial prompt ~%d tokens exceeds %d token budget; pipeline cannot start",
				estimatedTotal, budgetTokens,
			),
		}}
	}

	threshold := float64(budgetTokens) * 0.85
	if cfg.PhaseMachine.CompactionGateThreshold > 0 {
		threshold = float64(budgetTokens) * cfg.PhaseMachine.CompactionGateThreshold
	}

	if float64(estimatedTotal) > threshold {
		return []Finding{{
			Check:    "prompt_budget",
			Severity: SeverityWarn,
			Message: fmt.Sprintf(
				"initial prompt ~%d tokens exceeds %.0f%% of %d token budget; compaction will trigger early",
				estimatedTotal,
				(threshold/float64(budgetTokens))*100,
				budgetTokens,
			),
		}}
	}

	return nil
}

func CheckAcceptanceCommands(s *spec.Spec) []Finding {
	if s == nil || len(s.AcceptanceChecks) == 0 {
		return nil
	}

	var findings []Finding
	for i, check := range s.AcceptanceChecks {
		if check.Command == "" {
			continue
		}
		parts := strings.Fields(check.Command)
		if len(parts) == 0 {
			continue
		}
		bin := parts[0]
		if strings.Contains(bin, "/") {
			continue
		}
		if _, err := exec.LookPath(bin); err != nil {
			desc := check.Description
			if desc == "" {
				desc = fmt.Sprintf("check[%d]", i)
			}
			findings = append(findings, Finding{
				Check:    "acceptance_commands",
				Severity: SeverityError,
				Message:  fmt.Sprintf("acceptance check %q: command %q not found in PATH", desc, bin),
			})
		}
	}
	return findings
}

func CheckLockConflicts(cfg *config.Config, s *spec.Spec) []Finding {
	if cfg == nil || s == nil || len(s.WritesTo) == 0 || cfg.LockFile == "" {
		return nil
	}

	reg := lock.NewRegistry(cfg.LockFile)
	conflicts, err := reg.Check(s.WritesTo)
	if err != nil {
		return []Finding{{
			Check:    "lock_conflict",
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("unable to check locks: %v", err),
		}}
	}

	var findings []Finding
	for _, c := range conflicts {
		findings = append(findings, Finding{
			Check:    "lock_conflict",
			Severity: SeverityError,
			Message:  c.String(),
		})
	}
	return findings
}

func CheckActivePhases(complexity string) []Finding {
	if complexity == "" {
		return nil
	}

	active := phase.ActivePhases(phase.DefaultPhases(), complexity)
	if len(active) == 0 {
		return []Finding{{
			Check:    "active_phases",
			Severity: SeverityError,
			Message:  fmt.Sprintf("complexity %q produces 0 active phases", complexity),
		}}
	}
	return nil
}
