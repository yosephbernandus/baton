package phase

import (
	"context"
	"fmt"
	"time"

	"github.com/yosephbernandus/baton/internal/brief"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

type PipelineConfig struct {
	Complexity string
}

type PipelineResult struct {
	Status          string
	PhasesCompleted []int
	PhasesSkipped   []int
	FailedPhase     *int
	FailReason      string
	Duration        time.Duration
}

type Pipeline struct {
	config  PipelineConfig
	phases  []Phase
	runner  *runner.Runner
	cfg     *config.Config
	spec    *spec.Spec
	store   *task.Store
	emitter *events.Emitter
	specID  string
}

func NewPipeline(cfg *config.Config, r *runner.Runner, store *task.Store,
	emitter *events.Emitter, s *spec.Spec, specID string, pcfg PipelineConfig) *Pipeline {
	return &Pipeline{
		config:  pcfg,
		phases:  DefaultPhases(),
		runner:  r,
		cfg:     cfg,
		spec:    s,
		store:   store,
		emitter: emitter,
		specID:  specID,
	}
}

func (p *Pipeline) Run(ctx context.Context) (*PipelineResult, error) {
	start := time.Now()
	active := ActivePhases(p.phases, p.config.Complexity)
	skipped := SkippedPhaseIDs(p.phases, p.config.Complexity)
	totalPhases := len(p.phases)

	result := &PipelineResult{
		PhasesSkipped: skipped,
	}

	projectBrief := brief.Load(p.cfg.ProjectBrief)
	basePrompt := spec.BuildPrompt(p.spec, projectBrief)

	prevOutputs := make(map[int]string)

	for _, ph := range active {
		select {
		case <-ctx.Done():
			result.Status = "failed"
			result.FailReason = "pipeline cancelled"
			result.Duration = time.Since(start)
			return result, ctx.Err()
		default:
		}

		runtimeName, model := p.resolveRoleRuntime(ph.Role)

		phasePrompt := BuildPhasePrompt(basePrompt, ph, p.config.Complexity, totalPhases, prevOutputs)
		taskID := fmt.Sprintf("%s-phase-%d", p.specID, ph.ID)

		phasePrompt = spec.BuildPromptWithProtocol(phasePrompt, taskID, p.cfg.TaskDir)

		liveness := p.buildLiveness()

		p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_started")

		runResult, err := p.runner.Run(ctx, taskID, runtimeName, model, phasePrompt, p.spec, liveness)
		if err != nil {
			result.Status = "failed"
			failID := ph.ID
			result.FailedPhase = &failID
			result.FailReason = fmt.Sprintf("phase %d (%s) runner error: %v", ph.ID, ph.Name, err)
			result.Duration = time.Since(start)
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_failed")
			return result, nil
		}

		completion := extractCompletion(runResult.Output, ph.CompletionSignal)

		prevOutputs[ph.ID] = lastNLines(runResult.Output, 20)

		switch {
		case completion.Status == "done":
			result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")

		case completion.Status == "fail":
			result.Status = "failed"
			failID := ph.ID
			result.FailedPhase = &failID
			result.FailReason = fmt.Sprintf("phase %d (%s): %s", ph.ID, ph.Name, completion.Detail)
			result.Duration = time.Since(start)
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_failed")
			return result, nil

		case completion.Status == "blocked":
			result.Status = "blocked"
			failID := ph.ID
			result.FailedPhase = &failID
			result.FailReason = fmt.Sprintf("phase %d (%s) blocked: %s", ph.ID, ph.Name, completion.Detail)
			result.Duration = time.Since(start)
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_blocked")
			return result, nil

		default:
			if runResult.Status == "completed" {
				result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")
			} else {
				result.Status = "failed"
				failID := ph.ID
				result.FailedPhase = &failID
				result.FailReason = fmt.Sprintf("phase %d (%s): worker exited with status %s, no completion promise",
					ph.ID, ph.Name, runResult.Status)
				result.Duration = time.Since(start)
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_failed")
				return result, nil
			}
		}
	}

	result.Status = "completed"
	result.Duration = time.Since(start)
	return result, nil
}

func (p *Pipeline) resolveRoleRuntime(role string) (string, string) {
	if p.cfg.RoleModels != nil {
		if rm, ok := p.cfg.RoleModels[role]; ok {
			rt := rm.Runtime
			m := rm.Model
			if rt != "" && m != "" {
				return rt, m
			}
		}
	}
	return p.cfg.ResolveRuntime("", "")
}

func (p *Pipeline) buildLiveness() runner.LivenessConfig {
	lc := runner.LivenessConfig{}
	if d, err := time.ParseDuration(p.cfg.AbsoluteTimeout); err == nil {
		lc.AbsoluteTimeout = d
	}
	if d, err := time.ParseDuration(p.cfg.SilenceTimeout); err == nil {
		lc.SilenceTimeout = d
	}
	if d, err := time.ParseDuration(p.cfg.SilenceWarning); err == nil {
		lc.SilenceWarning = d
	}
	return lc
}

func (p *Pipeline) emitPhaseEvent(taskID, runtimeName, model string, ph Phase, eventType string) {
	if p.emitter == nil {
		return
	}
	_ = p.emitter.TaskEvent(taskID, runtimeName, model, "baton-pipeline", eventType,
		map[string]interface{}{
			"phase_id":   ph.ID,
			"phase_name": ph.Name,
			"role":       ph.Role,
			"complexity": p.config.Complexity,
		})
}

func extractCompletion(output []string, expectedSignal string) proto.CompletionPromise {
	for i := len(output) - 1; i >= 0; i-- {
		mk, ok := proto.ParseMarker(output[i])
		if !ok || mk.Type != proto.MarkerComplete {
			continue
		}
		cp, ok := proto.ParseCompletion(mk)
		if !ok {
			continue
		}
		if cp.Phase == expectedSignal {
			return cp
		}
	}
	return proto.CompletionPromise{}
}
