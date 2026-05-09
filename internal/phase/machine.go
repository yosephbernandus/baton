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

type PhaseRunner interface {
	Run(ctx context.Context, taskID, runtimeName, model, prompt string,
		s *spec.Spec, liveness runner.LivenessConfig) (*runner.Result, error)
}

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
	AttemptsByPhase map[int]int
}

type Pipeline struct {
	config  PipelineConfig
	phases  []Phase
	runner  PhaseRunner
	cfg     *config.Config
	spec    *spec.Spec
	store   *task.Store
	emitter *events.Emitter
	specID  string
}

func NewPipeline(cfg *config.Config, r PhaseRunner, store *task.Store,
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
	maxRetries := p.resolveMaxRetries()

	result := &PipelineResult{
		PhasesSkipped:   skipped,
		AttemptsByPhase: make(map[int]int),
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
		taskID := fmt.Sprintf("%s-phase-%d", p.specID, ph.ID)
		scratchpad := NewScratchpad(p.cfg.TaskDir, taskID)

		var phaseCompleted bool
		var lastFailReason string
		totalAttempts := maxRetries + 1

		for attempt := 1; attempt <= totalAttempts; attempt++ {
			select {
			case <-ctx.Done():
				result.Status = "failed"
				result.FailReason = "pipeline cancelled"
				result.Duration = time.Since(start)
				return result, ctx.Err()
			default:
			}

			scratchpadContent := scratchpad.ForPrompt()
			phasePrompt := BuildPhasePrompt(basePrompt, ph, p.config.Complexity, totalPhases, prevOutputs, scratchpadContent)
			phasePrompt = spec.BuildPromptWithProtocol(phasePrompt, taskID, p.cfg.TaskDir)

			liveness := p.buildLiveness()

			if attempt == 1 {
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_started")
			} else {
				p.emitPhaseRetryEvent(taskID, runtimeName, model, ph, attempt)
			}

			runResult, err := p.runner.Run(ctx, taskID, runtimeName, model, phasePrompt, p.spec, liveness)
			if err != nil {
				var notes []string
				if runResult != nil {
					notes = extractNotes(runResult.Output)
				}
				_ = scratchpad.AppendAttempt(attempt, notes, fmt.Sprintf("runner error: %v", err))
				lastFailReason = fmt.Sprintf("phase %d (%s) runner error: %v", ph.ID, ph.Name, err)
				continue
			}

			completion := extractCompletion(runResult.Output, ph.CompletionSignal)

			switch {
			case completion.Status == "done":
				phaseCompleted = true
				prevOutputs[ph.ID] = lastNLines(runResult.Output, 20)
				result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
				result.AttemptsByPhase[ph.ID] = attempt
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")

			case completion.Status == "fail":
				notes := extractNotes(runResult.Output)
				_ = scratchpad.AppendAttempt(attempt, notes, completion.Detail)
				lastFailReason = fmt.Sprintf("phase %d (%s): %s", ph.ID, ph.Name, completion.Detail)

			case completion.Status == "blocked":
				result.Status = "blocked"
				failID := ph.ID
				result.FailedPhase = &failID
				result.FailReason = fmt.Sprintf("phase %d (%s) blocked: %s", ph.ID, ph.Name, completion.Detail)
				result.Duration = time.Since(start)
				result.AttemptsByPhase[ph.ID] = attempt
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_blocked")
				return result, nil

			default:
				if runResult.Status == "completed" {
					phaseCompleted = true
					prevOutputs[ph.ID] = lastNLines(runResult.Output, 20)
					result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
					result.AttemptsByPhase[ph.ID] = attempt
					p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")
				} else {
					notes := extractNotes(runResult.Output)
					_ = scratchpad.AppendAttempt(attempt, notes,
						fmt.Sprintf("worker exited with status %s, no completion promise", runResult.Status))
					lastFailReason = fmt.Sprintf("phase %d (%s): worker exited with status %s, no completion promise",
						ph.ID, ph.Name, runResult.Status)
				}
			}

			if phaseCompleted {
				break
			}
		}

		if !phaseCompleted {
			result.Status = "failed"
			failID := ph.ID
			result.FailedPhase = &failID
			result.FailReason = fmt.Sprintf("%s (after %d attempts)", lastFailReason, totalAttempts)
			result.Duration = time.Since(start)
			result.AttemptsByPhase[ph.ID] = totalAttempts
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_failed")
			return result, nil
		}
	}

	result.Status = "completed"
	result.Duration = time.Since(start)
	return result, nil
}

func (p *Pipeline) resolveMaxRetries() int {
	if p.cfg.PhaseMachine.MaxL1Retries > 0 {
		return p.cfg.PhaseMachine.MaxL1Retries
	}
	return 2
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

func (p *Pipeline) emitPhaseRetryEvent(taskID, runtimeName, model string, ph Phase, attempt int) {
	if p.emitter == nil {
		return
	}
	_ = p.emitter.TaskEvent(taskID, runtimeName, model, "baton-pipeline", "phase_retry",
		map[string]interface{}{
			"phase_id":   ph.ID,
			"phase_name": ph.Name,
			"role":       ph.Role,
			"complexity": p.config.Complexity,
			"attempt":    attempt,
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

func extractNotes(output []string) []string {
	var notes []string
	for _, line := range output {
		mk, ok := proto.ParseMarker(line)
		if ok && mk.Type == proto.MarkerNote {
			notes = append(notes, mk.Msg)
		}
	}
	return notes
}
