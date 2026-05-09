package advisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockAdvisorRunner struct {
	output []string
	err    error
	calls  int
}

func (m *mockAdvisorRunner) Run(_ context.Context, _, _, _, _ string) ([]string, error) {
	m.calls++
	return m.output, m.err
}

func TestConsultDisabledFallsBack(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{Enabled: false}, nil, dir)

	resp, err := a.Consult(context.Background(), "task-1", Request{
		Spec:      "test task",
		Phase:     8,
		PhaseName: "implementation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Action != ActionEscalateToHuman {
		t.Errorf("action=%q, want escalate_to_human", resp.Action)
	}

	contextFile := filepath.Join(dir, "task-1", "advisor-context.yaml")
	if _, err := os.Stat(contextFile); os.IsNotExist(err) {
		t.Error("advisor-context.yaml not written")
	}
}

func TestConsultParsesResponse(t *testing.T) {
	mr := &mockAdvisorRunner{
		output: []string{
			"ACTION: retry_with_hint",
			"CONFIDENCE: high",
			"DETAIL: Try using the v2 API endpoint instead of v1",
		},
	}
	a := New(Config{Enabled: true, Runtime: "mock", Model: "test"}, mr, t.TempDir())

	resp, err := a.Consult(context.Background(), "task-1", Request{
		Spec:      "test task",
		Phase:     8,
		PhaseName: "implementation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Action != ActionRetryWithHint {
		t.Errorf("action=%q", resp.Action)
	}
	if resp.Confidence != "high" {
		t.Errorf("confidence=%q", resp.Confidence)
	}
	if !strings.Contains(resp.Detail, "v2 API") {
		t.Errorf("detail=%q", resp.Detail)
	}
	if mr.calls != 1 {
		t.Errorf("calls=%d", mr.calls)
	}
}

func TestConsultSessionLimitFallback(t *testing.T) {
	mr := &mockAdvisorRunner{
		output: []string{"ACTION: retry_with_hint", "DETAIL: hint"},
	}
	a := New(Config{
		Enabled:            true,
		Runtime:            "mock",
		Model:              "test",
		MaxCallsPerSession: 2,
	}, mr, t.TempDir())

	for i := 0; i < 2; i++ {
		_, _ = a.Consult(context.Background(), fmt.Sprintf("task-%d", i), Request{})
	}
	if mr.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", mr.calls)
	}

	resp, _ := a.Consult(context.Background(), "task-3", Request{})
	if resp.Action != ActionEscalateToHuman {
		t.Errorf("should fallback after session limit, got %q", resp.Action)
	}
	if mr.calls != 2 {
		t.Error("should not call runner after limit")
	}
}

func TestConsultTaskLimitFallback(t *testing.T) {
	mr := &mockAdvisorRunner{
		output: []string{"ACTION: skip_phase", "DETAIL: skip"},
	}
	a := New(Config{
		Enabled:         true,
		Runtime:         "mock",
		Model:           "test",
		MaxCallsPerTask: 1,
	}, mr, t.TempDir())

	_, _ = a.Consult(context.Background(), "task-1", Request{})
	resp, _ := a.Consult(context.Background(), "task-1", Request{})
	if resp.Action != ActionEscalateToHuman {
		t.Errorf("should fallback after task limit, got %q", resp.Action)
	}
	if mr.calls != 1 {
		t.Error("should not call runner after task limit")
	}
}

func TestConsultRunnerErrorFallback(t *testing.T) {
	mr := &mockAdvisorRunner{
		err: fmt.Errorf("connection refused"),
	}
	a := New(Config{Enabled: true, Runtime: "mock", Model: "test"}, mr, t.TempDir())

	resp, err := a.Consult(context.Background(), "task-1", Request{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Action != ActionEscalateToHuman {
		t.Errorf("should fallback on error, got %q", resp.Action)
	}
}

func TestConsultNilRunnerFallback(t *testing.T) {
	a := New(Config{Enabled: true}, nil, t.TempDir())
	resp, _ := a.Consult(context.Background(), "task-1", Request{})
	if resp.Action != ActionEscalateToHuman {
		t.Errorf("nil runner should fallback, got %q", resp.Action)
	}
}

func TestParseAdvisorOutputUnknownAction(t *testing.T) {
	resp := parseAdvisorOutput([]string{
		"ACTION: do_magic",
		"DETAIL: something",
	})
	if resp.Action != ActionEscalateToHuman {
		t.Errorf("unknown action should default to escalate, got %q", resp.Action)
	}
}

func TestParseAdvisorOutputSkipPhase(t *testing.T) {
	resp := parseAdvisorOutput([]string{
		"ACTION: skip_phase",
		"CONFIDENCE: medium",
		"DETAIL: Config-only change needs no tests",
	})
	if resp.Action != ActionSkipPhase {
		t.Errorf("action=%q", resp.Action)
	}
	if resp.Confidence != "medium" {
		t.Errorf("confidence=%q", resp.Confidence)
	}
}

func TestParseAdvisorOutputModifyConstraints(t *testing.T) {
	resp := parseAdvisorOutput([]string{
		"ACTION: modify_constraints",
		"CONFIDENCE: low",
		"DETAIL: Remove the backwards-compat constraint",
	})
	if resp.Action != ActionModifyConstraints {
		t.Errorf("action=%q", resp.Action)
	}
}

func TestBuildAdvisorPromptContainsContext(t *testing.T) {
	prompt := buildAdvisorPrompt(Request{
		Spec:         "Add auth middleware",
		Scratchpad:   "Tried JWT, failed on expiry check",
		Phase:        8,
		PhaseName:    "implementation",
		Role:         "developer",
		L1Attempts:   3,
		L2Cycles:     1,
		LoopDetected: true,
		FilesChanged: []string{"auth.go"},
	})

	checks := []string{
		"Add auth middleware",
		"JWT",
		"Phase: 8",
		"implementation",
		"L1 attempts used: 3",
		"Loop detected: true",
		"auth.go",
		"ACTION:",
	}
	for _, c := range checks {
		if !strings.Contains(prompt, c) {
			t.Errorf("prompt missing %q", c)
		}
	}
}

func TestSessionCallsCounter(t *testing.T) {
	mr := &mockAdvisorRunner{output: []string{"ACTION: retry_with_hint", "DETAIL: x"}}
	a := New(Config{Enabled: true, Runtime: "m", Model: "t"}, mr, t.TempDir())

	if a.SessionCalls() != 0 {
		t.Error("initial calls should be 0")
	}
	_, _ = a.Consult(context.Background(), "t1", Request{})
	if a.SessionCalls() != 1 {
		t.Errorf("calls=%d after 1 consult", a.SessionCalls())
	}
}

func TestFallbackWritesContextFile(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{Enabled: false}, nil, dir)

	req := Request{
		Spec:      "test spec",
		Phase:     10,
		PhaseName: "domain_compliance",
	}
	_, _ = a.Consult(context.Background(), "task-42", req)

	data, err := os.ReadFile(filepath.Join(dir, "task-42", "advisor-context.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "test spec") {
		t.Error("context file missing spec")
	}
	if !strings.Contains(content, "domain_compliance") {
		t.Error("context file missing phase name")
	}
}
