package coordinator

import (
	"strings"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/spec"
)

type CoordinatorConfig struct {
	TaskID     string
	Spec       *spec.Spec
	Complexity string
	BatonBin   string
	Config     *config.Config
}

type DispatchTarget struct {
	Self    bool
	Runtime string
	Model   string
}

// GenerateCoordinatorInstructions produces coordinator instructions
// that teach an LLM to execute the full baton pipeline.
func GenerateCoordinatorInstructions(cfg CoordinatorConfig) string {
	if cfg.BatonBin == "" {
		cfg.BatonBin = "baton"
	}

	phases := phase.DefaultPhases()
	active := phase.ActivePhases(phases, cfg.Complexity)
	dispatchMap := BuildDispatchMap(cfg.Config, active)

	maxL1 := cfg.Config.PhaseMachine.MaxL1Retries
	if maxL1 <= 0 {
		maxL1 = 2
	}
	maxL2 := cfg.Config.PhaseMachine.MaxL2Cycles
	if maxL2 <= 0 {
		maxL2 = 3
	}

	var b strings.Builder

	b.WriteString(buildIdentitySection())
	b.WriteString(buildTaskSection(cfg.Spec))
	b.WriteString(buildPhaseTableSection(active, dispatchMap, cfg.Complexity))
	b.WriteString(buildSelfPhaseProtocol(cfg.BatonBin, cfg.TaskID))
	b.WriteString(buildDispatchProtocol(cfg.BatonBin, cfg.TaskID, active, dispatchMap))
	b.WriteString(buildRetryProtocol(maxL1, maxL2, cfg.BatonBin, cfg.TaskID))
	b.WriteString(buildL2LoopProtocol(cfg.BatonBin, cfg.TaskID))
	b.WriteString(buildStuckProtocol(cfg.BatonBin))
	b.WriteString(buildCommandReference(cfg.BatonBin, cfg.TaskID))
	b.WriteString(buildPhaseGuidance(active))
	b.WriteString(buildReflectionSection(active))

	return b.String()
}

// BuildDispatchMap determines which phases the coordinator does itself vs dispatches.
func BuildDispatchMap(cfg *config.Config, phases []phase.Phase) map[int]DispatchTarget {
	m := make(map[int]DispatchTarget, len(phases))
	orchRuntime := cfg.Orchestrator.Runtime

	for _, ph := range phases {
		switch ph.Role {
		case phase.RoleLead, phase.RoleReviewer, phase.RoleTestLead:
			m[ph.ID] = DispatchTarget{Self: true}
		case phase.RoleDeveloper, phase.RoleTester:
			if cfg.RoleModels != nil {
				if rm, ok := cfg.RoleModels[ph.Role]; ok && rm.Runtime != "" && rm.Runtime != orchRuntime {
					m[ph.ID] = DispatchTarget{Self: false, Runtime: rm.Runtime, Model: rm.Model}
					continue
				}
			}
			m[ph.ID] = DispatchTarget{Self: true}
		default:
			m[ph.ID] = DispatchTarget{Self: true}
		}
	}

	return m
}
