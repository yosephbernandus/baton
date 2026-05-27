package phase

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/yosephbernandus/baton/internal/advisor"
	"github.com/yosephbernandus/baton/internal/brief"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/knowledge"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/role"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/session"
	"github.com/yosephbernandus/baton/internal/skill"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

type PhaseRunner interface {
	Run(ctx context.Context, taskID, runtimeName, model, prompt string,
		s *spec.Spec, liveness runner.LivenessConfig, extraArgs ...string) (*runner.Result, error)
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
	L2Cycles        int
	DirtyFiles      map[int][]string // phase ID → files changed
	DirtyBitSkips   []int            // phase IDs skipped due to no upstream changes
	Compactions     int              // number of compaction gate activations
}

type Pipeline struct {
	config           PipelineConfig
	phases           []Phase
	runner           PhaseRunner
	cfg              *config.Config
	spec             *spec.Spec
	store            *task.Store
	emitter          *events.Emitter
	specID           string
	manifest         *session.Manifest
	manifestPath     string
	advisor          *advisor.Advisor
	compactionGate   *CompactionGate
	resumeFromPhase  int
	resumeBriefing   string
	lastPhaseNotes   []string
	lastPhaseErrors  []string
	completedRecords []session.PhaseRecord
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

func (p *Pipeline) SetManifest(m *session.Manifest, path string) {
	p.manifest = m
	p.manifestPath = path
}

func (p *Pipeline) SetAdvisor(a *advisor.Advisor) {
	p.advisor = a
}

func (p *Pipeline) SetResume(fromPhase int, briefing string, records []session.PhaseRecord) {
	p.resumeFromPhase = fromPhase
	p.resumeBriefing = briefing
	for _, r := range records {
		if r.Status == "completed" {
			p.completedRecords = append(p.completedRecords, r)
		}
	}
}

func (p *Pipeline) Run(ctx context.Context) (*PipelineResult, error) {
	start := time.Now()
	active := ActivePhases(p.phases, p.config.Complexity)
	skipped := SkippedPhaseIDs(p.phases, p.config.Complexity)
	totalPhases := len(p.phases)
	maxRetries := p.resolveMaxRetries()
	maxL2 := p.resolveMaxL2Cycles()

	result := &PipelineResult{
		PhasesSkipped:   skipped,
		AttemptsByPhase: make(map[int]int),
		DirtyFiles:      make(map[int][]string),
	}

	if p.manifest != nil {
		p.manifest.SetSkipped(skipped)
		p.saveManifest()
	}

	if p.cfg.PhaseMachine.CompactionEnabled == nil || *p.cfg.PhaseMachine.CompactionEnabled {
		p.compactionGate = NewCompactionGate(
			p.cfg.PhaseMachine.CompactionGateThreshold,
			p.cfg.PhaseMachine.ContextBudgetTokens,
			p.cfg.PhaseMachine.CompactionGatePhases,
		)
	}

	projectBrief := brief.Load(p.cfg.ProjectBrief)
	basePrompt := spec.BuildPrompt(p.spec, projectBrief)

	skillContext := p.loadSkillContext()
	if skillContext != "" {
		basePrompt += "\n[DOMAIN CONTEXT]\n" + skillContext + "\n"
	}

	l2Cycles := 0
	if p.manifest != nil {
		l2Cycles = p.manifest.Budget.L2CyclesTotal
	}

	startIdx := 0
	if p.resumeFromPhase > 0 {
		for j, ap := range active {
			if ap.ID > p.resumeFromPhase {
				startIdx = j
				break
			}
		}
	}

	i := startIdx
	resumeBriefingUsed := false
	for i < len(active) {
		ph := active[i]

		select {
		case <-ctx.Done():
			result.Status = "failed"
			result.FailReason = "pipeline cancelled"
			result.Duration = time.Since(start)
			return result, ctx.Err()
		default:
		}

		if p.shouldSkipDirtyBit(ph.ID, result.DirtyFiles, l2Cycles) {
			result.PhasesSkipped = append(result.PhasesSkipped, ph.ID)
			result.DirtyBitSkips = append(result.DirtyBitSkips, ph.ID)
			p.emitDirtyBitSkipEvent(ph)
			if p.manifest != nil {
				p.manifest.RecordDirtyBitSkip(ph.ID)
				p.saveManifest()
			}
			i++
			continue
		}

		phasePromptBase := basePrompt
		if p.resumeBriefing != "" && !resumeBriefingUsed {
			phasePromptBase = basePrompt + "\n" + p.resumeBriefing + "\n"
			resumeBriefingUsed = true
		}

		if p.compactionGate != nil && p.compactionGate.IsGatePhase(ph.ID) {
			estimate := p.estimatePromptTokens(phasePromptBase, result.DirtyFiles)
			if p.compactionGate.ShouldCompact(estimate) {
				p.completedRecords = CompactRecords(p.completedRecords)
				result.Compactions++
				p.emitCompactionEvent(ph, estimate)
				if p.manifest != nil {
					p.manifest.RecordCompaction(ph.ID)
					p.saveManifest()
				}
			}
		}

		phaseStart := time.Now()
		phaseOutcome, failReason := p.executePhaseWithRetries(
			ctx, ph, phasePromptBase, totalPhases, maxRetries, result)

		phaseDuration := time.Since(phaseStart)

		switch phaseOutcome {
		case outcomeCompleted:
			rec := session.PhaseRecord{
				ID:           ph.ID,
				Name:         ph.Name,
				Status:       "completed",
				Notes:        p.lastPhaseNotes,
				Errors:       p.lastPhaseErrors,
				FilesChanged: result.DirtyFiles[ph.ID],
				Attempts:     result.AttemptsByPhase[ph.ID],
				Duration:     phaseDuration.Round(time.Second).String(),
				CompletedAt:  time.Now(),
			}
			p.completedRecords = append(p.completedRecords, rec)
			if p.manifest != nil {
				p.manifest.AdvancePhase(ph.ID)
				p.manifest.AddPhaseRecord(rec)
				if files := result.DirtyFiles[ph.ID]; len(files) > 0 {
					p.manifest.AddPipelineFiles(files)
				}
				p.saveManifest()
			}
			i++

		case outcomeCancelled:
			result.Status = "failed"
			result.FailReason = "pipeline cancelled"
			result.Duration = time.Since(start)
			return result, ctx.Err()

		case outcomeBlocked:
			result.Duration = time.Since(start)
			p.saveManifestStatus("failed")
			return result, nil

		case outcomeFailed:
			if IsVerificationPhase(ph.ID) && l2Cycles < maxL2 {
				l2Cycles++
				result.L2Cycles = l2Cycles

				implTaskID := fmt.Sprintf("%s-phase-%d", p.specID, L2StartPhase)
				implScratchpad := NewScratchpad(p.cfg.TaskDir, implTaskID)
				_ = implScratchpad.AppendAttempt(0, []string{
					fmt.Sprintf("L2 cycle %d: phase %d (%s) failed: %s",
						l2Cycles, ph.ID, ph.Name, failReason),
				}, fmt.Sprintf("verification failed in phase %d, looping back to implementation", ph.ID))

				p.emitL2Event(ph, l2Cycles, failReason)

				implIdx := -1
				for j, ap := range active {
					if ap.ID == L2StartPhase {
						implIdx = j
						break
					}
				}
				if implIdx < 0 {
					result.Status = "failed"
					failID := ph.ID
					result.FailedPhase = &failID
					result.FailReason = fmt.Sprintf("%s (no implementation phase to loop back to)", failReason)
					result.Duration = time.Since(start)
					p.saveManifestStatus("failed")
					return result, nil
				}
				if p.manifest != nil {
					p.manifest.RecordL2Cycle()
					p.manifest.LoopBackTo(L2StartPhase)
					p.saveManifest()
				}
				select {
				case <-ctx.Done():
					result.Status = "failed"
					result.Duration = time.Since(start)
					p.saveManifestStatus("failed")
					return result, nil
				case <-time.After(p.resolveL2Cooldown()):
				}
				i = implIdx
				continue
			}

			result.Status = "failed"
			result.Duration = time.Since(start)
			if l2Cycles >= maxL2 && IsVerificationPhase(ph.ID) {
				result.FailReason = fmt.Sprintf("%s (L2 cycles exhausted: %d/%d)", failReason, l2Cycles, maxL2)
			}
			p.saveManifestStatus("failed")
			return result, nil

		case outcomeStuck:
			if IsVerificationPhase(ph.ID) && l2Cycles < maxL2 {
				l2Cycles++
				result.L2Cycles = l2Cycles

				implTaskID := fmt.Sprintf("%s-phase-%d", p.specID, L2StartPhase)
				implScratchpad := NewScratchpad(p.cfg.TaskDir, implTaskID)
				_ = implScratchpad.AppendAttempt(0, []string{
					fmt.Sprintf("L2 cycle %d: phase %d (%s) stuck (loop detected)",
						l2Cycles, ph.ID, ph.Name),
				}, "verification stuck in loop, looping back to implementation with different approach needed")

				p.emitL2Event(ph, l2Cycles, "loop detected — worker stuck")

				implIdx := -1
				for j, ap := range active {
					if ap.ID == L2StartPhase {
						implIdx = j
						break
					}
				}
				if implIdx < 0 {
					result.Status = "failed"
					failID := ph.ID
					result.FailedPhase = &failID
					result.FailReason = failReason
					result.Duration = time.Since(start)
					p.saveManifestStatus("failed")
					return result, nil
				}
				if p.manifest != nil {
					p.manifest.RecordL2Cycle()
					p.manifest.LoopBackTo(L2StartPhase)
					p.saveManifest()
				}
				select {
				case <-ctx.Done():
					result.Status = "failed"
					result.Duration = time.Since(start)
					p.saveManifestStatus("failed")
					return result, nil
				case <-time.After(p.resolveL2Cooldown()):
				}
				i = implIdx
				continue
			}

			result.Status = "failed"
			result.Duration = time.Since(start)
			p.saveManifestStatus("failed")
			return result, nil

		case outcomeRateLimited:
			if p.manifest != nil {
				rec := session.PhaseRecord{
					ID:         ph.ID,
					Name:       ph.Name,
					Status:     "rate_limited",
					Attempts:   result.AttemptsByPhase[ph.ID],
					FailReason: failReason,
				}
				p.manifest.AddPhaseRecord(rec)
				p.manifest.MarkRateLimited(failReason)
				p.saveManifest()
			}
			result.Status = "rate_limited"
			result.FailedPhase = intPtr(ph.ID)
			result.FailReason = failReason
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	result.Status = "completed"
	result.Duration = time.Since(start)
	p.saveManifestStatus("completed")
	return result, nil
}

type phaseOutcome int

const (
	outcomeCompleted phaseOutcome = iota
	outcomeFailed
	outcomeBlocked
	outcomeCancelled
	outcomeStuck
	outcomeRateLimited
)

func (p *Pipeline) executePhaseWithRetries(
	ctx context.Context,
	ph Phase,
	basePrompt string,
	totalPhases int,
	maxRetries int,
	result *PipelineResult,
) (phaseOutcome, string) {

	runtimeName, model := p.resolveRoleRuntime(ph.Role)
	taskID := fmt.Sprintf("%s-phase-%d", p.specID, ph.ID)
	scratchpad := NewScratchpad(p.cfg.TaskDir, taskID)
	loopDetector := p.newLoopDetector()
	boBase, boMax, boJitter := p.resolveBackoff()

	var toolRestrictionFlags []string
	if rt, ok := p.cfg.Runtimes[runtimeName]; ok {
		if tools := role.AllowedTools(ph.Role); len(tools) > 0 {
			toolRestrictionFlags = runner.BuildToolRestrictionFlags(&rt, tools)
		}
	}

	var lastFailReason string
	totalAttempts := maxRetries + 1

	if p.store != nil {
		now := time.Now().UTC()
		phaseTask := &task.Task{
			ID:        taskID,
			Runtime:   runtimeName,
			Model:     model,
			Status:    "running",
			Spec:      p.spec,
			CreatedAt: now,
			StartedAt: &now,
		}
		_ = p.store.Create(phaseTask)
	}

	for attempt := 1; attempt <= totalAttempts; attempt++ {
		select {
		case <-ctx.Done():
			p.finalizePhaseTask(taskID, "failed")
			return outcomeCancelled, "pipeline cancelled"
		default:
		}

		scratchpadContent := scratchpad.ForPrompt()
		phasePrompt := BuildPhasePrompt(basePrompt, ph, p.config.Complexity, totalPhases, p.completedRecords, scratchpadContent, result.DirtyFiles)
		phasePrompt = spec.BuildPromptWithProtocol(phasePrompt, taskID, p.cfg.TaskDir)

		liveness := p.buildLiveness()

		if attempt == 1 {
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_started")
		} else {
			if p.manifest != nil {
				p.manifest.RecordL1Retry()
				p.saveManifest()
			}
			p.emitPhaseRetryEvent(taskID, runtimeName, model, ph, attempt)
		}

		runResult, err := p.runner.Run(ctx, taskID, runtimeName, model, phasePrompt, p.spec, liveness, toolRestrictionFlags...)
		if err != nil {
			var notes []string
			var output []string
			if runResult != nil {
				notes = extractNotes(runResult.Output)
				output = runResult.Output
			}
			_ = scratchpad.AppendAttempt(attempt, notes, fmt.Sprintf("runner error: %v", err))
			lastFailReason = fmt.Sprintf("phase %d (%s) runner error: %v", ph.ID, ph.Name, err)
			if loopDetector != nil {
				loopDetector.Record(output)
				if loopDetector.IsStuck() {
					_ = scratchpad.AppendAttempt(0, nil,
						"[LOOP DETECTED] Worker produced identical output across attempts. This approach is fundamentally stuck.")
					result.FailedPhase = intPtr(ph.ID)
					result.FailReason = fmt.Sprintf("%s (loop detected — worker stuck)", lastFailReason)
					result.AttemptsByPhase[ph.ID] = attempt
					p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_stuck")
					p.finalizePhaseTask(taskID, "failed")
					return outcomeStuck, lastFailReason
				}
			}
			select {
			case <-ctx.Done():
				p.finalizePhaseTask(taskID, "failed")
				return outcomeCancelled, "pipeline cancelled"
			case <-time.After(backoffDelay(attempt, boBase, boMax, boJitter)):
			}
			continue
		}

		if runResult.Status == "rate_limited" {
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_rate_limited")
			rlRetries, rlBase, rlMax := p.resolveRateLimitRetry()
			rateLimitResolved := false
			for rlAttempt := 1; rlAttempt <= rlRetries; rlAttempt++ {
				wait := backoffDelay(rlAttempt, rlBase, rlMax, true)
				select {
				case <-ctx.Done():
					p.finalizePhaseTask(taskID, "failed")
					return outcomeCancelled, "pipeline cancelled"
				case <-time.After(wait):
				}
				rlResult, rlErr := p.runner.Run(ctx, taskID, runtimeName, model, phasePrompt, p.spec, liveness, toolRestrictionFlags...)
				if rlErr != nil || rlResult.Status == "rate_limited" {
					continue
				}
				runResult = rlResult
				rateLimitResolved = true
				break
			}
			if !rateLimitResolved {
				result.AttemptsByPhase[ph.ID] = attempt
				p.finalizePhaseTask(taskID, "rate_limited")
				return outcomeRateLimited, fmt.Sprintf("phase %d (%s): rate limited after %d retries", ph.ID, ph.Name, rlRetries)
			}
		}

		// Heartbeat budget check
		if budget := p.cfg.PhaseMachine.HeartbeatBudget; budget > 0 {
			hbCount := countHeartbeats(runResult.Output)
			if hbCount > budget {
				budgetMsg := fmt.Sprintf("heartbeat budget exceeded: %d/%d", hbCount, budget)
				_ = scratchpad.AppendAttempt(attempt, []string{budgetMsg}, budgetMsg)
				lastFailReason = fmt.Sprintf("phase %d (%s): %s", ph.ID, ph.Name, budgetMsg)
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_budget_exceeded")
				select {
				case <-ctx.Done():
					p.finalizePhaseTask(taskID, "failed")
					return outcomeCancelled, "pipeline cancelled"
				case <-time.After(backoffDelay(attempt, boBase, boMax, boJitter)):
				}
				continue
			}
		}

		completion := extractCompletion(runResult.Output, ph.CompletionSignal)

		switch {
		case completion.Status == "done":
			if violations := role.VerifyBoundary(ph.Role, runResult.FilesChanged); len(violations) > 0 {
				var msgs []string
				for _, v := range violations {
					msgs = append(msgs, fmt.Sprintf("%s: %s", v.File, v.Reason))
				}
				violationMsg := fmt.Sprintf("role boundary violation: %s", strings.Join(msgs, "; "))
				_ = scratchpad.AppendAttempt(attempt, []string{violationMsg}, "role boundary violated")
				lastFailReason = fmt.Sprintf("phase %d (%s): %s", ph.ID, ph.Name, violationMsg)
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_boundary_violation")
				select {
				case <-ctx.Done():
					p.finalizePhaseTask(taskID, "failed")
					return outcomeCancelled, "pipeline cancelled"
				case <-time.After(backoffDelay(attempt, boBase, boMax, boJitter)):
				}
				continue
			}
			if len(runResult.FilesChanged) > 0 {
				result.DirtyFiles[ph.ID] = runResult.FilesChanged
			}
			result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
			result.AttemptsByPhase[ph.ID] = attempt
			p.lastPhaseNotes = extractNotes(runResult.Output)
			p.lastPhaseErrors = extractErrors(runResult.Output)
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")
			p.finalizePhaseTask(taskID, "completed")
			return outcomeCompleted, ""

		case completion.Status == "fail":
			notes := extractNotes(runResult.Output)
			_ = scratchpad.AppendAttempt(attempt, notes, completion.Detail)
			lastFailReason = fmt.Sprintf("phase %d (%s): %s", ph.ID, ph.Name, completion.Detail)
			if loopDetector != nil {
				loopDetector.Record(runResult.Output)
				if loopDetector.IsStuck() {
					_ = scratchpad.AppendAttempt(0, nil,
						"[LOOP DETECTED] Worker produced identical output across attempts. This approach is fundamentally stuck.")
					result.FailedPhase = intPtr(ph.ID)
					result.FailReason = fmt.Sprintf("%s (loop detected — worker stuck)", lastFailReason)
					result.AttemptsByPhase[ph.ID] = attempt
					p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_stuck")
					p.finalizePhaseTask(taskID, "failed")
					return outcomeStuck, lastFailReason
				}
			}

		case completion.Status == "blocked":
			result.Status = "blocked"
			result.FailedPhase = intPtr(ph.ID)
			result.FailReason = fmt.Sprintf("phase %d (%s) blocked: %s", ph.ID, ph.Name, completion.Detail)
			result.AttemptsByPhase[ph.ID] = attempt
			p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_blocked")
			p.finalizePhaseTask(taskID, "failed")
			return outcomeBlocked, result.FailReason

		default:
			if runResult.Status == "completed" {
				if violations := role.VerifyBoundary(ph.Role, runResult.FilesChanged); len(violations) > 0 {
					var msgs []string
					for _, v := range violations {
						msgs = append(msgs, fmt.Sprintf("%s: %s", v.File, v.Reason))
					}
					violationMsg := fmt.Sprintf("role boundary violation: %s", strings.Join(msgs, "; "))
					_ = scratchpad.AppendAttempt(attempt, []string{violationMsg}, "role boundary violated")
					lastFailReason = fmt.Sprintf("phase %d (%s): %s", ph.ID, ph.Name, violationMsg)
					p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_boundary_violation")
					select {
					case <-ctx.Done():
						p.finalizePhaseTask(taskID, "failed")
						return outcomeCancelled, "pipeline cancelled"
					case <-time.After(backoffDelay(attempt, boBase, boMax, boJitter)):
					}
					continue
				}
				if len(runResult.FilesChanged) > 0 {
					result.DirtyFiles[ph.ID] = runResult.FilesChanged
				}
				result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
				result.AttemptsByPhase[ph.ID] = attempt
				p.lastPhaseNotes = extractNotes(runResult.Output)
				p.lastPhaseErrors = extractErrors(runResult.Output)
				p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")
				p.finalizePhaseTask(taskID, "completed")
				return outcomeCompleted, ""
			}
			notes := extractNotes(runResult.Output)
			_ = scratchpad.AppendAttempt(attempt, notes,
				fmt.Sprintf("worker exited with status %s, no completion promise", runResult.Status))
			lastFailReason = fmt.Sprintf("phase %d (%s): worker exited with status %s, no completion promise",
				ph.ID, ph.Name, runResult.Status)
			if loopDetector != nil {
				loopDetector.Record(runResult.Output)
				if loopDetector.IsStuck() {
					_ = scratchpad.AppendAttempt(0, nil,
						"[LOOP DETECTED] Worker produced identical output across attempts. This approach is fundamentally stuck.")
					result.FailedPhase = intPtr(ph.ID)
					result.FailReason = fmt.Sprintf("%s (loop detected — worker stuck)", lastFailReason)
					result.AttemptsByPhase[ph.ID] = attempt
					p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_stuck")
					p.finalizePhaseTask(taskID, "failed")
					return outcomeStuck, lastFailReason
				}
			}
		}
	}

	// All retries exhausted — consult advisor if available
	if p.advisor != nil {
		advisorResp := p.consultAdvisor(ctx, taskID, ph, scratchpad, result, loopDetector != nil && loopDetector.IsStuck())
		if advisorResp != nil && advisorResp.Action == advisor.ActionRetryWithHint {
			_ = scratchpad.AppendAttempt(0, []string{
				fmt.Sprintf("[ADVISOR HINT] %s", advisorResp.Detail),
			}, "advisor suggested retry with hint")

			scratchpadContent := scratchpad.ForPrompt()
			phasePrompt := BuildPhasePrompt(basePrompt, ph, p.config.Complexity, totalPhases, p.completedRecords, scratchpadContent, result.DirtyFiles)
			phasePrompt = spec.BuildPromptWithProtocol(phasePrompt, taskID, p.cfg.TaskDir)
			liveness := p.buildLiveness()

			p.emitPhaseRetryEvent(taskID, runtimeName, model, ph, totalAttempts+1)
			runResult, err := p.runner.Run(ctx, taskID, runtimeName, model, phasePrompt, p.spec, liveness, toolRestrictionFlags...)
			if err == nil {
				completion := extractCompletion(runResult.Output, ph.CompletionSignal)
				if completion.Status == "done" || runResult.Status == "completed" {
					if len(runResult.FilesChanged) > 0 {
						result.DirtyFiles[ph.ID] = runResult.FilesChanged
					}
					result.PhasesCompleted = append(result.PhasesCompleted, ph.ID)
					result.AttemptsByPhase[ph.ID] = totalAttempts + 1
					p.lastPhaseNotes = extractNotes(runResult.Output)
					p.lastPhaseErrors = extractErrors(runResult.Output)
					p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_completed")
					p.finalizePhaseTask(taskID, "completed")
					return outcomeCompleted, ""
				}
			}
		}
	}

	result.FailedPhase = intPtr(ph.ID)
	result.FailReason = fmt.Sprintf("%s (after %d attempts)", lastFailReason, totalAttempts)
	result.AttemptsByPhase[ph.ID] = totalAttempts
	p.emitPhaseEvent(taskID, runtimeName, model, ph, "phase_failed")
	p.finalizePhaseTask(taskID, "failed")
	return outcomeFailed, lastFailReason
}

func (p *Pipeline) finalizePhaseTask(taskID, status string) {
	if p.store == nil {
		return
	}
	t, err := p.store.Get(taskID)
	if err != nil {
		return
	}
	t.Status = status
	now := time.Now().UTC()
	t.CompletedAt = &now
	_ = p.store.Update(t)
}

func intPtr(i int) *int { return &i }

func (p *Pipeline) saveManifest() {
	if p.manifest != nil && p.manifestPath != "" {
		_ = p.manifest.Save(p.manifestPath)
	}
}

func (p *Pipeline) saveManifestStatus(status string) {
	if p.manifest == nil {
		return
	}
	switch status {
	case "completed":
		p.manifest.MarkCompleted()
	case "failed":
		p.manifest.MarkFailed("")
	}
	p.saveManifest()
}

func (p *Pipeline) newLoopDetector() *LoopDetector {
	if p.cfg.PhaseMachine.LoopDetectionEnabled != nil && !*p.cfg.PhaseMachine.LoopDetectionEnabled {
		return nil
	}
	window := p.cfg.PhaseMachine.LoopWindowSize
	threshold := p.cfg.PhaseMachine.LoopThreshold
	tailLines := p.cfg.PhaseMachine.LoopTailLines
	return NewLoopDetector(window, threshold, tailLines)
}

func (p *Pipeline) consultAdvisor(ctx context.Context, taskID string, ph Phase, scratchpad *Scratchpad, result *PipelineResult, loopDetected bool) *advisor.Response {
	if p.advisor == nil {
		return nil
	}

	scratchpadContent, _ := scratchpad.Read()

	var filesChanged []string
	for _, files := range result.DirtyFiles {
		filesChanged = append(filesChanged, files...)
	}

	specSummary := ""
	if p.spec != nil {
		specSummary = fmt.Sprintf("What: %s\nWhy: %s", p.spec.What, p.spec.Why)
	}

	req := advisor.Request{
		Spec:         specSummary,
		Scratchpad:   scratchpadContent,
		Phase:        ph.ID,
		PhaseName:    ph.Name,
		Role:         ph.Role,
		L1Attempts:   result.AttemptsByPhase[ph.ID],
		L2Cycles:     result.L2Cycles,
		LoopDetected: loopDetected,
		FilesChanged: filesChanged,
	}

	resp, err := p.advisor.Consult(ctx, taskID, req)
	if err != nil {
		return nil
	}

	p.emitAdvisorEvent(taskID, ph, resp)
	return resp
}

func (p *Pipeline) emitAdvisorEvent(taskID string, ph Phase, resp *advisor.Response) {
	if p.emitter == nil {
		return
	}
	_ = p.emitter.TaskEvent(taskID, "", "", "baton-pipeline", "advisor_consulted",
		map[string]interface{}{
			"phase_id":   ph.ID,
			"phase_name": ph.Name,
			"action":     resp.Action,
			"confidence": resp.Confidence,
			"detail":     resp.Detail,
		})
}

func (p *Pipeline) loadSkillContext() string {
	domain := ""
	if p.spec != nil {
		domain = p.spec.Domain
	}

	// Try graph-aware domain inference first, fall back to extension-based
	if domain == "" && p.spec != nil {
		cwd, _ := os.Getwd()
		graph, _ := knowledge.Load(cwd)
		modPath := ""
		if data, err := os.ReadFile("go.mod"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "module ") {
					modPath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
					break
				}
			}
		}
		domain = skill.InferDomainFromGraph(graph, p.spec.ContextFiles, modPath)
	}
	if domain == "" {
		return ""
	}

	router := skill.NewRouter(p.cfg.Skills.Dir, p.cfg.Skills.DomainMap)
	skillCtx, _ := router.LoadContext(domain)

	// Merge structural knowledge into skill context
	var knowledgeCtx string
	if p.spec != nil {
		cwd, _ := os.Getwd()
		graph, err := knowledge.Load(cwd)
		if err == nil {
			modPath := ""
			if data, err := os.ReadFile("go.mod"); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, "module ") {
						modPath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
						break
					}
				}
			}
			queryFiles := make([]string, 0, len(p.spec.ContextFiles)+len(p.spec.WritesTo))
			queryFiles = append(queryFiles, p.spec.ContextFiles...)
			queryFiles = append(queryFiles, p.spec.WritesTo...)
			knowledgeCtx = knowledge.Inject(graph, queryFiles, modPath, knowledge.DefaultTokenBudget)
		}
	}

	if skillCtx == "" && knowledgeCtx == "" {
		return ""
	}

	var b strings.Builder
	if knowledgeCtx != "" {
		b.WriteString(knowledgeCtx)
	}
	if skillCtx != "" {
		b.WriteString("## Domain Skills\n\n")
		b.WriteString(skillCtx)
	}
	return b.String()
}

func (p *Pipeline) resolveMaxRetries() int {
	if p.cfg.PhaseMachine.MaxL1Retries > 0 {
		return p.cfg.PhaseMachine.MaxL1Retries
	}
	return 2
}

func (p *Pipeline) resolveMaxL2Cycles() int {
	if p.cfg.PhaseMachine.MaxL2Cycles > 0 {
		return p.cfg.PhaseMachine.MaxL2Cycles
	}
	return 3
}

func backoffDelay(attempt int, base, max time.Duration, jitter bool) time.Duration {
	exp := math.Pow(2, float64(attempt-1))
	d := time.Duration(float64(base) * exp)
	if d > max {
		d = max
	}
	if jitter {
		d += time.Duration(rand.Int63n(int64(base)))
	}
	return d
}

func (p *Pipeline) resolveBackoff() (base, max time.Duration, jitter bool) {
	cfg := p.cfg.PhaseMachine
	base = 1 * time.Second
	max = 8 * time.Second
	jitter = true
	if cfg.BackoffBaseMs > 0 {
		base = time.Duration(cfg.BackoffBaseMs) * time.Millisecond
	}
	if cfg.BackoffMaxMs > 0 {
		max = time.Duration(cfg.BackoffMaxMs) * time.Millisecond
	}
	if cfg.BackoffBaseMs > 0 || cfg.BackoffMaxMs > 0 {
		jitter = cfg.BackoffJitter
	}
	return
}

func (p *Pipeline) resolveRateLimitRetry() (retries int, base, max time.Duration) {
	cfg := p.cfg.PhaseMachine
	retries = 10
	base = 30 * time.Second
	max = 60 * time.Second
	if cfg.RateLimitRetries > 0 {
		retries = cfg.RateLimitRetries
	}
	if cfg.RateLimitBaseWaitMs > 0 {
		base = time.Duration(cfg.RateLimitBaseWaitMs) * time.Millisecond
	}
	if cfg.RateLimitMaxWaitMs > 0 {
		max = time.Duration(cfg.RateLimitMaxWaitMs) * time.Millisecond
	}
	return
}

func (p *Pipeline) resolveL2Cooldown() time.Duration {
	if p.cfg.PhaseMachine.L2CooldownMs > 0 {
		return time.Duration(p.cfg.PhaseMachine.L2CooldownMs) * time.Millisecond
	}
	return 5 * time.Second
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
	if d, err := time.ParseDuration(p.cfg.StartupTimeout); err == nil {
		lc.StartupTimeout = d
	}
	if d, err := time.ParseDuration(p.cfg.NetworkIdleTimeout); err == nil {
		lc.NetworkIdleTimeout = d
	}
	if d, err := time.ParseDuration(p.cfg.AttemptTimeout); err == nil {
		lc.AttemptTimeout = d
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

func (p *Pipeline) emitL2Event(failedPhase Phase, cycle int, reason string) {
	if p.emitter == nil {
		return
	}
	_ = p.emitter.TaskEvent(
		fmt.Sprintf("%s-l2-cycle-%d", p.specID, cycle),
		"", "", "baton-pipeline", "l2_loop_back",
		map[string]interface{}{
			"cycle":             cycle,
			"failed_phase_id":   failedPhase.ID,
			"failed_phase_name": failedPhase.Name,
			"reason":            reason,
			"looping_back_to":   L2StartPhase,
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

func extractErrors(output []string) []string {
	var errs []string
	for _, line := range output {
		mk, ok := proto.ParseMarker(line)
		if ok && mk.Type == proto.MarkerError {
			errs = append(errs, mk.Msg)
		}
	}
	return errs
}

func countHeartbeats(output []string) int {
	count := 0
	for _, line := range output {
		mk, ok := proto.ParseMarker(line)
		if ok && mk.Type == proto.MarkerHeartbeat {
			count++
		}
	}
	return count
}

func (p *Pipeline) estimatePromptTokens(basePrompt string, dirtyFiles map[int][]string) int {
	tokens := EstimateTokens(basePrompt)
	for _, r := range p.completedRecords {
		tokens += EstimateTokens(buildRecordDetail(r))
		for _, n := range r.Notes {
			tokens += EstimateTokens(n)
		}
		for _, e := range r.Errors {
			tokens += EstimateTokens(e)
		}
	}
	for _, files := range dirtyFiles {
		for _, f := range files {
			tokens += EstimateTokens(f)
		}
	}
	return tokens
}

func (p *Pipeline) emitDirtyBitSkipEvent(ph Phase) {
	if p.emitter == nil {
		return
	}
	_ = p.emitter.TaskEvent(
		fmt.Sprintf("%s-phase-%d", p.specID, ph.ID),
		"", "", "baton-pipeline", "phase_dirty_bit_skip",
		map[string]interface{}{
			"phase_id":   ph.ID,
			"phase_name": ph.Name,
			"reason":     "no upstream file changes",
		})
}

func (p *Pipeline) emitCompactionEvent(ph Phase, tokensBefore int) {
	if p.emitter == nil {
		return
	}
	_ = p.emitter.TaskEvent(
		fmt.Sprintf("%s-phase-%d", p.specID, ph.ID),
		"", "", "baton-pipeline", "compaction_triggered",
		map[string]interface{}{
			"phase_id":        ph.ID,
			"phase_name":      ph.Name,
			"tokens_before":   tokensBefore,
			"records_compacted": len(p.completedRecords),
		})
}
