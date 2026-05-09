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
