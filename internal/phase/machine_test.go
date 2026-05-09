package phase

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/spec"
)

type mockRunner struct {
	results []*runner.Result
	errors  []error
	calls   int
	prompts []string
}

func (m *mockRunner) Run(_ context.Context, _, _, _, prompt string,
	_ *spec.Spec, _ runner.LivenessConfig) (*runner.Result, error) {
	i := m.calls
	m.calls++
	m.prompts = append(m.prompts, prompt)
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
		What:             "test",
		Why:              "testing",
		Constraints:      []string{},
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
// 8(implementation), 10(domain_compliance), 12(test_planning),
// 13(testing), 14(coverage_verification), 15(test_quality), 16(completion)

func smallPhaseResults(overrides map[string]*runner.Result) *mockRunner {
	// SMALL active phases in order: 1,2,3,4,8,10,12,13,14,15,16
	phases := []string{
		"setup", "triage", "discovery", "skill_discovery",
		"implementation", "domain_compliance", "test_planning",
		"testing", "coverage_verification", "test_quality", "completion",
	}
	var results []*runner.Result
	var errs []error
	for _, name := range phases {
		if r, ok := overrides[name]; ok {
			results = append(results, r)
			errs = append(errs, nil)
		} else {
			results = append(results, &runner.Result{
				Status: "completed",
				Output: []string{fmt.Sprintf("BATON:C:%s:done", name)},
			})
			errs = append(errs, nil)
		}
	}
	return &mockRunner{results: results, errors: errs}
}

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
		{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
		// domain_compliance fails
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:naming violation"}},
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:naming violation"}},
		{Status: "completed", Output: []string{"BATON:C:domain_compliance:fail:naming violation"}},
		// L2 loop back to implementation
		{Status: "completed", Output: []string{"BATON:C:implementation:done"}},
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
			Status: "completed",
			Output: []string{"BATON:C:implementation:done"},
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
		Status: "completed",
		Output: []string{"BATON:C:implementation:done"},
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
		Status: "completed",
		Output: []string{"BATON:C:implementation:done"},
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

	// Impl, domain_compliance, test_planning pass
	for _, name := range []string{"implementation", "domain_compliance", "test_planning"} {
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
