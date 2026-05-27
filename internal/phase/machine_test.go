package phase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/spec"
)

type mockRunner struct {
	results   []*runner.Result
	errors    []error
	calls     int
	prompts   []string
	extraArgs [][]string
}

func (m *mockRunner) Run(_ context.Context, _, _, _, prompt string,
	_ *spec.Spec, _ runner.LivenessConfig, extraArgs ...string) (*runner.Result, error) {
	i := m.calls
	m.calls++
	m.prompts = append(m.prompts, prompt)
	m.extraArgs = append(m.extraArgs, extraArgs)
	if i < len(m.results) {
		return m.results[i], m.errors[i]
	}
	return nil, fmt.Errorf("unexpected call %d", i)
}

func testConfig(maxRetries int) *config.Config {
	return &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "mock", Model: "test"},
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {Command: "echo"},
		},
		TaskDir:         "/tmp/baton-test",
		AbsoluteTimeout: "5m",
		SilenceTimeout:  "2m",
		PhaseMachine: config.PhaseMachineConfig{
			MaxL1Retries: maxRetries,
		},
	}
}

func testSpec() *spec.Spec {
	return &spec.Spec{
		What:               "test",
		Why:                "testing",
		Constraints:        []string{},
		AcceptanceCriteria: []string{"works"},
	}
}

func TestPipelineRetryOnFail(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{
				"BATON:N:tried approach A",
				"BATON:C:setup:fail:build error",
			}},
			{Status: "completed", Output: []string{
				"BATON:C:setup:done",
			}},
			{Status: "completed", Output: []string{
				"BATON:C:implementation:done",
			}},
			{Status: "completed", Output: []string{
				"BATON:C:completion:done",
			}},
		},
		errors: make([]error, 4),
	}

	cfg := testConfig(2)
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("status=%s, want completed", result.Status)
	}
	if result.AttemptsByPhase[1] != 2 {
		t.Errorf("phase 1 attempts=%d, want 2", result.AttemptsByPhase[1])
	}
	if mr.calls != 4 {
		t.Errorf("runner calls=%d, want 4 (1 retry + 3 phases)", mr.calls)
	}
}

func TestPipelineRetryExhausted(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:fail:error1"}},
			{Status: "completed", Output: []string{"BATON:C:setup:fail:error2"}},
			{Status: "completed", Output: []string{"BATON:C:setup:fail:error3"}},
		},
		errors: make([]error, 3),
	}

	cfg := testConfig(2)
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("status=%s, want failed", result.Status)
	}
	if !strings.Contains(result.FailReason, "after 3 attempts") {
		t.Errorf("fail reason=%q, want 'after 3 attempts'", result.FailReason)
	}
	if result.AttemptsByPhase[1] != 3 {
		t.Errorf("phase 1 attempts=%d, want 3", result.AttemptsByPhase[1])
	}
}

func TestPipelineNoRetryOnBlocked(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:blocked:waiting on API key"}},
		},
		errors: make([]error, 1),
	}

	cfg := testConfig(2)
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" {
		t.Errorf("status=%s, want blocked", result.Status)
	}
	if mr.calls != 1 {
		t.Errorf("runner calls=%d, want 1 (no retry on blocked)", mr.calls)
	}
}

func TestPipelineRetryOnRunnerError(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			nil,
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: []error{
			fmt.Errorf("process crashed"),
			nil, nil, nil,
		},
	}

	cfg := testConfig(2)
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("status=%s, want completed", result.Status)
	}
	if result.AttemptsByPhase[1] != 2 {
		t.Errorf("phase 1 attempts=%d, want 2", result.AttemptsByPhase[1])
	}
}

func TestPipelineScratchpadInjected(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{
				"BATON:N:tried X and failed",
				"BATON:C:setup:fail:X broken",
			}},
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 4),
	}

	cfg := testConfig(2)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("status=%s, want completed", result.Status)
	}

	// Second call (retry) should contain scratchpad
	if len(mr.prompts) < 2 {
		t.Fatal("expected at least 2 prompts")
	}
	if !strings.Contains(mr.prompts[1], "SCRATCHPAD") {
		t.Error("retry prompt should contain SCRATCHPAD section")
	}
	if !strings.Contains(mr.prompts[1], "tried X and failed") {
		t.Error("retry prompt should contain notes from attempt 1")
	}
}

func TestPipelineNoRetryWhenMaxZero(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 3),
	}

	// MaxL1Retries=0 means use default (2), so we test with config that has no retries needed
	cfg := testConfig(0)
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("status=%s, want completed", result.Status)
	}
}

func TestExtractNotes(t *testing.T) {
	output := []string{
		"BATON:H:working",
		"BATON:N:tried X",
		"some random output",
		"BATON:N:Y also failed",
		"BATON:C:setup:done",
	}

	notes := extractNotes(output)
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(notes))
	}
	if notes[0] != "tried X" {
		t.Errorf("notes[0]=%q, want 'tried X'", notes[0])
	}
	if notes[1] != "Y also failed" {
		t.Errorf("notes[1]=%q, want 'Y also failed'", notes[1])
	}
}

func TestPipelineLoopDetectionStuck(t *testing.T) {
	// Same output 3 times → loop detected, stop early
	sameOutput := []string{
		"starting build",
		"error: undefined reference to foo",
		"BATON:C:setup:fail:build error",
	}
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: sameOutput},
			{Status: "completed", Output: sameOutput},
			{Status: "completed", Output: sameOutput},
			{Status: "completed", Output: sameOutput},
		},
		errors: make([]error, 4),
	}

	cfg := testConfig(5) // 6 total attempts, but loop should stop at 3
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("status=%s, want failed", result.Status)
	}
	if !strings.Contains(result.FailReason, "loop detected") {
		t.Errorf("fail reason=%q, want 'loop detected'", result.FailReason)
	}
	// Should have stopped at 3 attempts (window=3), not 6
	if mr.calls > 3 {
		t.Errorf("runner calls=%d, want <=3 (loop should stop early)", mr.calls)
	}
}

func TestPipelineLoopDetectionNotTriggered(t *testing.T) {
	// Different outputs each time → no loop detection
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"attempt 1 output", "BATON:C:setup:fail:error1"}},
			{Status: "completed", Output: []string{"attempt 2 different", "BATON:C:setup:fail:error2"}},
			{Status: "completed", Output: []string{"attempt 3 unique", "BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 5),
	}

	cfg := testConfig(3)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Errorf("status=%s, want completed", result.Status)
	}
}

func TestPipelineLoopDetectionDisabled(t *testing.T) {
	sameOutput := []string{"same error every time", "BATON:C:setup:fail:error"}
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: sameOutput},
			{Status: "completed", Output: sameOutput},
			{Status: "completed", Output: sameOutput},
		},
		errors: make([]error, 3),
	}

	cfg := testConfig(2)
	cfg.TaskDir = t.TempDir()
	disabled := false
	cfg.PhaseMachine.LoopDetectionEnabled = &disabled
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("status=%s, want failed", result.Status)
	}
	// Should exhaust all retries without loop detection
	if !strings.Contains(result.FailReason, "after 3 attempts") {
		t.Errorf("fail reason=%q, want 'after 3 attempts' (no loop detection)", result.FailReason)
	}
}

// L2 tests use SMALL complexity which includes phases:
// 1(setup), 2(triage), 3(discovery), 4(skill_discovery),
func TestPipelineL2LoopBack(t *testing.T) {
	// domain_compliance (phase 10) fails on first pass, implementation reruns, then all pass
	callCount := 0
	mr := &mockRunner{}
	// Build results dynamically based on call order:
	// SMALL phases: setup(1), triage(2), discovery(3), skill_discovery(4),
	//   implementation(8), domain_compliance(10), test_planning(12),
	//   testing(13), coverage(14), test_quality(15), completion(16)
	//
	// First pass: phases 1-4 pass, impl passes, domain_compliance FAILS
	// L2 loop: impl reruns (passes), domain_compliance passes, rest pass
	allResults := []*runner.Result{
		{Status: "completed", Output: []string{"BATON:C:setup:done"}},
		{Status: "completed", Output: []string{"BATON:C:triage:done"}},
		{Status: "completed", Output: []string{"BATON:C:discovery:done"}},
		{Status: "completed", Output: []string{"BATON:C:skill_discovery:done"}},
		{Status: "completed", Output: []string{"BATON:C:implementation:done"},
			FilesChanged: []string{"main.go"}},
		// domain_compliance fails
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:naming violation"}},
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:naming violation"}},
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:naming violation"}},
		// L2 loop back to implementation
		{Status: "completed", Output: []string{"BATON:C:implementation:done"},
			FilesChanged: []string{"main.go"}},
		// domain_compliance passes
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:done"}},
		{Status: "completed", Output: []string{"BATON:C:test_planning:done"}},
		{Status: "completed", Output: []string{"BATON:C:testing:done"}},
		{Status: "completed", Output: []string{"BATON:C:coverage_verification:done"}},
		{Status: "completed", Output: []string{"BATON:C:test_quality:done"}},
		{Status: "completed", Output: []string{"BATON:C:completion:done"}},
	}
	allErrs := make([]error, len(allResults))
	mr.results = allResults
	mr.errors = allErrs
	_ = callCount

	cfg := testConfig(2) // 3 L1 attempts per phase
	cfg.PhaseMachine.MaxL2Cycles = 2
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if result.L2Cycles != 1 {
		t.Errorf("L2Cycles=%d, want 1", result.L2Cycles)
	}
}

func TestPipelineL2Exhausted(t *testing.T) {
	// testing (phase 13) fails every time, exhausting L2 cycles
	var results []*runner.Result
	var errs []error

	// Pre-L2: phases 1,2,3,4 pass
	for _, name := range []string{"setup", "triage", "discovery", "skill_discovery"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	// 3 L2 cycles: impl passes, phases 10,12 pass, testing fails (3 L1 attempts each)
	for cycle := 0; cycle < 3; cycle++ {
		// implementation passes
		results = append(results, &runner.Result{
			Status:       "completed",
			Output:       []string{"BATON:C:implementation:done"},
			FilesChanged: []string{"main.go"},
		})
		errs = append(errs, nil)
		// domain_compliance passes
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{"BATON:C:domain_compliance:done"},
		})
		errs = append(errs, nil)
		// test_planning passes
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{"BATON:C:test_planning:done"},
		})
		errs = append(errs, nil)
		// testing fails 3 times (L1 retries)
		for a := 0; a < 3; a++ {
			results = append(results, &runner.Result{
				Status: "completed",
				Output: []string{fmt.Sprintf("attempt %d cycle %d", a, cycle), "BATON:C:testing:fail:tests broken"},
			})
			errs = append(errs, nil)
		}
	}

	// One more impl + review + test fail after L2 exhausted
	results = append(results, &runner.Result{
		Status:       "completed",
		Output:       []string{"BATON:C:implementation:done"},
		FilesChanged: []string{"main.go"},
	})
	errs = append(errs, nil)
	results = append(results, &runner.Result{
		Status: "completed",
		Output: []string{"BATON:C:domain_compliance:done"},
	})
	errs = append(errs, nil)
	results = append(results, &runner.Result{
		Status: "completed",
		Output: []string{"BATON:C:test_planning:done"},
	})
	errs = append(errs, nil)
	for a := 0; a < 3; a++ {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("final attempt %d", a), "BATON:C:testing:fail:still broken"},
		})
		errs = append(errs, nil)
	}

	mr := &mockRunner{results: results, errors: errs}

	cfg := testConfig(2) // 3 L1 attempts
	cfg.PhaseMachine.MaxL2Cycles = 3
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("status=%s, want failed", result.Status)
	}
	if !strings.Contains(result.FailReason, "L2 cycles exhausted") {
		t.Errorf("fail reason=%q, want 'L2 cycles exhausted'", result.FailReason)
	}
	if result.L2Cycles != 3 {
		t.Errorf("L2Cycles=%d, want 3", result.L2Cycles)
	}
}

func TestPipelineL2NoLoopForNonVerification(t *testing.T) {
	// Implementation (phase 8) fails — NOT a verification phase, no L2 loop
	var results []*runner.Result
	var errs []error

	// Phases 1-4 pass
	for _, name := range []string{"setup", "triage", "discovery", "skill_discovery"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	// Implementation fails 3 times
	for i := 0; i < 3; i++ {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("impl attempt %d", i), "BATON:C:implementation:fail:cant compile"},
		})
		errs = append(errs, nil)
	}

	mr := &mockRunner{results: results, errors: errs}

	cfg := testConfig(2) // 3 L1 attempts
	cfg.PhaseMachine.MaxL2Cycles = 3
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("status=%s, want failed", result.Status)
	}
	if result.L2Cycles != 0 {
		t.Errorf("L2Cycles=%d, want 0 (no L2 for non-verification phase)", result.L2Cycles)
	}
}

func TestPipelineBoundaryViolationRetry(t *testing.T) {
	// Reviewer (phase 10, domain_compliance) modifies a file → violation → retry
	// SMALL active: 1,2,3,4,8,10,12,13,14,15,16
	var results []*runner.Result
	var errs []error

	// Phases 1-4 pass
	for _, name := range []string{"setup", "triage", "discovery", "skill_discovery"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	// Implementation passes
	results = append(results, &runner.Result{
		Status:       "completed",
		Output:       []string{"BATON:C:implementation:done"},
		FilesChanged: []string{"internal/config/config.go"},
	})
	errs = append(errs, nil)

	// Domain compliance (reviewer) — first attempt modifies file = violation
	results = append(results, &runner.Result{
		Status:       "completed",
		Output:       []string{"BATON:C:domain_compliance:done"},
		FilesChanged: []string{"internal/config/config.go"}, // violation!
	})
	errs = append(errs, nil)

	// Retry — no files changed = passes
	results = append(results, &runner.Result{
		Status: "completed",
		Output: []string{"BATON:C:domain_compliance:done"},
	})
	errs = append(errs, nil)

	// Rest pass
	for _, name := range []string{"test_planning", "testing", "coverage_verification", "test_quality", "completion"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	mr := &mockRunner{results: results, errors: errs}

	cfg := testConfig(2)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	// Phase 10 should have taken 2 attempts (1 violation + 1 pass)
	if result.AttemptsByPhase[10] != 2 {
		t.Errorf("phase 10 attempts=%d, want 2", result.AttemptsByPhase[10])
	}
}

func TestPipelineBoundaryViolationTester(t *testing.T) {
	// Tester (phase 13) modifies production file → violation
	var results []*runner.Result
	var errs []error

	// Phases 1-4 pass
	for _, name := range []string{"setup", "triage", "discovery", "skill_discovery"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	// Implementation passes with file changes
	results = append(results, &runner.Result{
		Status:       "completed",
		Output:       []string{"BATON:C:implementation:done"},
		FilesChanged: []string{"internal/phase/machine.go"},
	})
	errs = append(errs, nil)

	// domain_compliance, test_planning pass
	for _, name := range []string{"domain_compliance", "test_planning"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	// Testing — modifies production file (violation)
	results = append(results, &runner.Result{
		Status:       "completed",
		Output:       []string{"BATON:C:testing:done"},
		FilesChanged: []string{"internal/phase/machine.go"}, // not a test file
	})
	errs = append(errs, nil)

	// Testing retry — only test files
	results = append(results, &runner.Result{
		Status:       "completed",
		Output:       []string{"BATON:C:testing:done"},
		FilesChanged: []string{"internal/phase/machine_test.go"},
	})
	errs = append(errs, nil)

	// Rest pass
	for _, name := range []string{"coverage_verification", "test_quality", "completion"} {
		results = append(results, &runner.Result{
			Status: "completed",
			Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
		})
		errs = append(errs, nil)
	}

	mr := &mockRunner{results: results, errors: errs}

	cfg := testConfig(2)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if result.AttemptsByPhase[13] != 2 {
		t.Errorf("phase 13 attempts=%d, want 2", result.AttemptsByPhase[13])
	}
}

func TestPipelineHeartbeatBudgetExceeded(t *testing.T) {
	// Phase 1 outputs too many heartbeats → budget exceeded → retry
	mr := &mockRunner{
		results: []*runner.Result{
			// Attempt 1: 5 heartbeats, budget is 3 → exceeded
			{Status: "completed", Output: []string{
				"BATON:H:working", "BATON:H:still working", "BATON:H:more",
				"BATON:H:even more", "BATON:H:way too many",
				"BATON:C:setup:done",
			}},
			// Attempt 2: 2 heartbeats, within budget
			{Status: "completed", Output: []string{
				"BATON:H:working", "BATON:H:done",
				"BATON:C:setup:done",
			}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 4),
	}

	cfg := testConfig(2)
	cfg.PhaseMachine.HeartbeatBudget = 3
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if result.AttemptsByPhase[1] != 2 {
		t.Errorf("phase 1 attempts=%d, want 2", result.AttemptsByPhase[1])
	}
}

func TestPipelineHeartbeatBudgetExhausted(t *testing.T) {
	// All attempts exceed budget
	overBudget := &runner.Result{Status: "completed", Output: []string{
		"BATON:H:1", "BATON:H:2", "BATON:H:3", "BATON:H:4", "BATON:H:5",
		"BATON:C:setup:done",
	}}
	mr := &mockRunner{
		results: []*runner.Result{overBudget, overBudget, overBudget},
		errors:  make([]error, 3),
	}

	cfg := testConfig(2)
	cfg.PhaseMachine.HeartbeatBudget = 3
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("status=%s, want failed", result.Status)
	}
	if !strings.Contains(result.FailReason, "heartbeat budget exceeded") {
		t.Errorf("reason=%q, want 'heartbeat budget exceeded'", result.FailReason)
	}
}

func TestPipelineDirtyFilesTracked(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"},
				FilesChanged: []string{"main.go", "config.go"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 3),
	}

	cfg := testConfig(2)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed", result.Status)
	}
	files, ok := result.DirtyFiles[8]
	if !ok {
		t.Fatal("expected dirty files for phase 8")
	}
	if len(files) != 2 {
		t.Errorf("dirty files count=%d, want 2", len(files))
	}
}

func TestCountHeartbeats(t *testing.T) {
	output := []string{
		"BATON:H:working",
		"some output",
		"BATON:H:still going",
		"BATON:P:50:progress",
		"BATON:H:almost done",
		"BATON:C:setup:done",
	}
	if c := countHeartbeats(output); c != 3 {
		t.Errorf("count=%d, want 3", c)
	}
}

func TestCountHeartbeatsNone(t *testing.T) {
	output := []string{"plain output", "BATON:C:setup:done"}
	if c := countHeartbeats(output); c != 0 {
		t.Errorf("count=%d, want 0", c)
	}
}

func TestToolRestrictionFlagsPassedToRunner(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
		},
		errors: []error{nil},
	}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "mock", Model: "test"},
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {
				Command: "echo",
				ToolRestriction: &config.ToolRestriction{
					Flag:   "--allowedTools",
					Format: "comma-separated",
				},
			},
		},
		TaskDir:         t.TempDir(),
		AbsoluteTimeout: "5m",
		SilenceTimeout:  "2m",
	}

	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{
		Complexity: ComplexityTrivial,
	})

	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mr.calls < 1 {
		t.Fatal("expected at least 1 call")
	}
	// Phase 1 (setup) has role "lead" → AllowedTools returns ["Read","Grep","Glob","Bash"]
	args := mr.extraArgs[0]
	if len(args) != 2 {
		t.Fatalf("expected 2 extra args (flag + value), got %d: %v", len(args), args)
	}
	if args[0] != "--allowedTools" {
		t.Errorf("flag=%q, want --allowedTools", args[0])
	}
	if !strings.Contains(args[1], "Read") || !strings.Contains(args[1], "Bash") {
		t.Errorf("expected tools in %q", args[1])
	}
}

func TestToolRestrictionFlagsNilForDeveloper(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:ship:done"}},
		},
		errors: []error{nil, nil, nil},
	}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "mock", Model: "test"},
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {
				Command: "echo",
				ToolRestriction: &config.ToolRestriction{
					Flag:   "--allowedTools",
					Format: "comma-separated",
				},
			},
		},
		TaskDir:         t.TempDir(),
		AbsoluteTimeout: "5m",
		SilenceTimeout:  "2m",
	}

	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{
		Complexity: ComplexityTrivial,
	})

	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// TRIVIAL runs phases 1,8,16. Phase 8 (implementation) has role "developer"
	// developer has no tool restrictions → extraArgs should be empty
	if mr.calls < 2 {
		t.Fatal("expected at least 2 calls")
	}
	devArgs := mr.extraArgs[1] // phase 8, developer
	if len(devArgs) != 0 {
		t.Errorf("developer should have no tool restriction flags, got %v", devArgs)
	}
}

func TestSkillContextInjectedIntoPrompt(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "backend")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "conventions.md"), []byte("Always use structured logging"), 0o644)

	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
		},
		errors: []error{nil},
	}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "mock", Model: "test"},
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {Command: "echo"},
		},
		TaskDir:         filepath.Join(dir, "tasks"),
		AbsoluteTimeout: "5m",
		SilenceTimeout:  "2m",
		Skills: config.SkillsConfig{
			Dir: filepath.Join(dir, "skills"),
		},
	}

	s := testSpec()
	s.Domain = "backend"

	p := NewPipeline(cfg, mr, nil, nil, s, "test", PipelineConfig{
		Complexity: ComplexityTrivial,
	})

	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mr.calls < 1 {
		t.Fatal("expected at least 1 call")
	}
	if !strings.Contains(mr.prompts[0], "Always use structured logging") {
		t.Error("skill context not injected into prompt")
	}
	if !strings.Contains(mr.prompts[0], "DOMAIN CONTEXT") {
		t.Error("missing DOMAIN CONTEXT header")
	}
}

func TestSkillContextInferredFromFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "go")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "patterns.md"), []byte("Error wrapping with fmt.Errorf"), 0o644)

	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
		},
		errors: []error{nil},
	}

	// Create dummy context files so spec validation doesn't complain
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "runner.go"), []byte("package runner"), 0o644)

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "mock", Model: "test"},
		Runtimes: map[string]config.RuntimeConfig{
			"mock": {Command: "echo"},
		},
		TaskDir:         filepath.Join(dir, "tasks"),
		AbsoluteTimeout: "5m",
		SilenceTimeout:  "2m",
		Skills: config.SkillsConfig{
			Dir: filepath.Join(dir, "skills"),
		},
	}

	s := &spec.Spec{
		What:               "test",
		Why:                "testing",
		Constraints:        []string{},
		ContextFiles:       []string{filepath.Join(dir, "main.go"), filepath.Join(dir, "runner.go")},
		AcceptanceCriteria: []string{"works"},
	}

	p := NewPipeline(cfg, mr, nil, nil, s, "test", PipelineConfig{
		Complexity: ComplexityTrivial,
	})

	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mr.prompts[0], "Error wrapping") {
		t.Error("inferred skill context not injected")
	}
}

func TestSkillContextEmptyWhenNoDomain(t *testing.T) {
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
		},
		errors: []error{nil},
	}
	cfg := testConfig(0)
	cfg.TaskDir = t.TempDir()

	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{
		Complexity: ComplexityTrivial,
	})

	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(mr.prompts[0], "DOMAIN CONTEXT") {
		t.Error("should not inject domain context when no domain")
	}
}

func TestPipelineDirtyBitSkipsVerification(t *testing.T) {
	// Implementation produces no file changes → verification/testing phases skipped
	// SMALL active phases: 1,2,3,4,8,10,12,13,14,15,16
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:triage:done"}},
			{Status: "completed", Output: []string{"BATON:C:discovery:done"}},
			{Status: "completed", Output: []string{"BATON:C:skill_discovery:done"}},
			// Implementation: no FilesChanged
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			// Phases 10,12,13,14,15 dirty-bit-skipped → straight to completion
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 6),
	}

	cfg := testConfig(0)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if mr.calls != 6 {
		t.Errorf("runner calls=%d, want 6 (dirty bit skip should skip verification)", mr.calls)
	}
	if len(result.DirtyBitSkips) != 5 {
		t.Errorf("dirty bit skips=%d, want 5 (phases 10,12,13,14,15)", len(result.DirtyBitSkips))
	}
	expectedSkips := map[int]bool{10: true, 12: true, 13: true, 14: true, 15: true}
	for _, skip := range result.DirtyBitSkips {
		if !expectedSkips[skip] {
			t.Errorf("unexpected dirty bit skip for phase %d", skip)
		}
	}
}

func TestPipelineDirtyBitNoSkipWithChanges(t *testing.T) {
	// Implementation produces file changes → verification runs normally
	// SMALL active: 1,2,3,4,8,10,12,13,14,15,16
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:triage:done"}},
			{Status: "completed", Output: []string{"BATON:C:discovery:done"}},
			{Status: "completed", Output: []string{"BATON:C:skill_discovery:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"},
				FilesChanged: []string{"main.go"}},
			{Status: "completed", Output: []string{"BATON:C:domain_compliance:done"}},
			{Status: "completed", Output: []string{"BATON:C:test_planning:done"}},
			{Status: "completed", Output: []string{"BATON:C:testing:done"}},
			{Status: "completed", Output: []string{"BATON:C:coverage_verification:done"}},
			{Status: "completed", Output: []string{"BATON:C:test_quality:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 11),
	}

	cfg := testConfig(0)
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if mr.calls != 11 {
		t.Errorf("runner calls=%d, want 11 (all phases should run)", mr.calls)
	}
	if len(result.DirtyBitSkips) != 0 {
		t.Errorf("dirty bit skips=%d, want 0", len(result.DirtyBitSkips))
	}
}

func TestPipelineDirtyBitNoSkipDuringL2(t *testing.T) {
	// L2 cycle: re-implementation with no changes should NOT dirty-bit-skip
	// verification (we need verification to detect if issue was resolved)
	allResults := []*runner.Result{
		{Status: "completed", Output: []string{"BATON:C:setup:done"}},
		{Status: "completed", Output: []string{"BATON:C:triage:done"}},
		{Status: "completed", Output: []string{"BATON:C:discovery:done"}},
		{Status: "completed", Output: []string{"BATON:C:skill_discovery:done"}},
		{Status: "completed", Output: []string{"BATON:C:implementation:done"},
			FilesChanged: []string{"main.go"}},
		// domain_compliance fails → L2 loop
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:style violation"}},
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:style violation"}},
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:style violation"}},
		// L2 loop: re-implementation (no new changes)
		{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
		// Should still run domain_compliance (not dirty-bit-skipped)
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:done"}},
		{Status: "completed", Output: []string{"BATON:C:test_planning:done"}},
		{Status: "completed", Output: []string{"BATON:C:testing:done"}},
		{Status: "completed", Output: []string{"BATON:C:coverage_verification:done"}},
		{Status: "completed", Output: []string{"BATON:C:test_quality:done"}},
		{Status: "completed", Output: []string{"BATON:C:completion:done"}},
	}
	mr := &mockRunner{
		results: allResults,
		errors:  make([]error, len(allResults)),
	}

	cfg := testConfig(2)
	cfg.PhaseMachine.MaxL2Cycles = 2
	cfg.TaskDir = t.TempDir()
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if result.L2Cycles != 1 {
		t.Errorf("L2Cycles=%d, want 1", result.L2Cycles)
	}
	// During L2, dirty bit skip should NOT activate
	for _, skip := range result.DirtyBitSkips {
		if skip == 10 {
			t.Error("domain_compliance should not be dirty-bit-skipped during L2")
		}
	}
}

func TestPipelineDirtyBitDisabled(t *testing.T) {
	// Dirty bit disabled → all phases run even with no changes
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 3),
	}

	cfg := testConfig(0)
	cfg.TaskDir = t.TempDir()
	disabled := false
	cfg.PhaseMachine.DirtyBitSkipEnabled = &disabled
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed", result.Status)
	}
	if len(result.DirtyBitSkips) != 0 {
		t.Error("dirty bit skips should be empty when disabled")
	}
}

func TestPipelineCompactionGateTriggered(t *testing.T) {
	// Very low token budget forces compaction at gate phases
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:triage:done"}},
			{Status: "completed", Output: []string{"BATON:C:discovery:done"}},
			{Status: "completed", Output: []string{"BATON:C:skill_discovery:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"},
				FilesChanged: []string{"main.go"}},
			{Status: "completed", Output: []string{"BATON:C:domain_compliance:done"}},
			{Status: "completed", Output: []string{"BATON:C:test_planning:done"}},
			{Status: "completed", Output: []string{"BATON:C:testing:done"}},
			{Status: "completed", Output: []string{"BATON:C:coverage_verification:done"}},
			{Status: "completed", Output: []string{"BATON:C:test_quality:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 11),
	}

	cfg := testConfig(0)
	cfg.TaskDir = t.TempDir()
	cfg.PhaseMachine.ContextBudgetTokens = 10
	cfg.PhaseMachine.CompactionGateThreshold = 0.5
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexitySmall})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("status=%s, want completed. Reason: %s", result.Status, result.FailReason)
	}
	if result.Compactions == 0 {
		t.Error("expected at least one compaction event with very low budget")
	}
}

func TestPipelineCompactionGateNotTriggered(t *testing.T) {
	// High budget → no compaction
	mr := &mockRunner{
		results: []*runner.Result{
			{Status: "completed", Output: []string{"BATON:C:setup:done"}},
			{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
			{Status: "completed", Output: []string{"BATON:C:completion:done"}},
		},
		errors: make([]error, 3),
	}

	cfg := testConfig(0)
	cfg.TaskDir = t.TempDir()
	cfg.PhaseMachine.ContextBudgetTokens = 999999
	p := NewPipeline(cfg, mr, nil, nil, testSpec(), "test", PipelineConfig{Complexity: ComplexityTrivial})

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Compactions != 0 {
		t.Errorf("compactions=%d, want 0 with high budget", result.Compactions)
	}
}

func TestBackoffDelay(t *testing.T) {
	base := 100 * time.Millisecond
	max := 800 * time.Millisecond

	d1 := backoffDelay(1, base, max, false)
	if d1 != 100*time.Millisecond {
		t.Errorf("attempt 1: want 100ms, got %v", d1)
	}
	d2 := backoffDelay(2, base, max, false)
	if d2 != 200*time.Millisecond {
		t.Errorf("attempt 2: want 200ms, got %v", d2)
	}
	d3 := backoffDelay(3, base, max, false)
	if d3 != 400*time.Millisecond {
		t.Errorf("attempt 3: want 400ms, got %v", d3)
	}
	d5 := backoffDelay(5, base, max, false)
	if d5 != 800*time.Millisecond {
		t.Errorf("attempt 5: want 800ms (capped), got %v", d5)
	}

	for i := 0; i < 20; i++ {
		d := backoffDelay(1, base, max, true)
		if d < 100*time.Millisecond || d >= 200*time.Millisecond {
			t.Errorf("attempt 1 jitter: want [100ms, 200ms), got %v", d)
		}
	}
}
