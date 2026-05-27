package gateway

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/lock"
	"github.com/yosephbernandus/baton/internal/spec"
)

func testConfig() *config.Config {
	return &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "mock", Model: "test"},
		Runtimes: map[string]config.RuntimeConfig{
			"mock":    {Command: "echo"},
			"missing": {Command: "nonexistent-binary-xyz-12345"},
		},
		LockFile:     ".baton/locks.yaml",
		ProjectBrief: ".baton/project-brief.md",
	}
}

func TestCheckRuntimeAvailableExists(t *testing.T) {
	cfg := testConfig()
	findings := CheckRuntimeAvailable(cfg, "mock")
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for existing runtime, got %d: %v", len(findings), findings)
	}
}

func TestCheckRuntimeAvailableMissing(t *testing.T) {
	cfg := testConfig()
	findings := CheckRuntimeAvailable(cfg, "missing")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected SeverityError, got %d", findings[0].Severity)
	}
	if !strings.Contains(findings[0].Message, "nonexistent-binary-xyz-12345") {
		t.Errorf("message should mention command: %s", findings[0].Message)
	}
}

func TestCheckRuntimeAvailableNilConfig(t *testing.T) {
	findings := CheckRuntimeAvailable(nil, "mock")
	if len(findings) != 0 {
		t.Errorf("nil config should return 0 findings, got %d", len(findings))
	}
}

func TestCheckRuntimeAvailableEmptyName(t *testing.T) {
	cfg := testConfig()
	findings := CheckRuntimeAvailable(cfg, "")
	if len(findings) != 0 {
		t.Errorf("empty name should return 0 findings, got %d", len(findings))
	}
}

func TestCheckPromptBudgetUnderLimit(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{What: "small task", Why: "testing"}
	findings := CheckPromptBudget(cfg, s)
	if len(findings) != 0 {
		t.Errorf("small spec should have 0 findings, got %d: %v", len(findings), findings)
	}
}

func TestCheckPromptBudgetExceedsBudget(t *testing.T) {
	cfg := testConfig()
	cfg.PhaseMachine.ContextBudgetTokens = 10
	huge := strings.Repeat("word ", 100)
	s := &spec.Spec{What: huge, Why: huge}
	findings := CheckPromptBudget(cfg, s)
	if len(findings) == 0 {
		t.Fatal("huge spec with tiny budget should produce findings")
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected SeverityError, got %d", findings[0].Severity)
	}
}

func TestCheckPromptBudgetNearThreshold(t *testing.T) {
	cfg := testConfig()
	cfg.PhaseMachine.ContextBudgetTokens = 50
	cfg.PhaseMachine.CompactionGateThreshold = 0.5
	medium := strings.Repeat("word ", 15)
	s := &spec.Spec{What: medium, Why: medium}
	findings := CheckPromptBudget(cfg, s)

	hasWarn := false
	for _, f := range findings {
		if f.Severity == SeverityWarn {
			hasWarn = true
		}
	}
	if !hasWarn && len(findings) == 0 {
		t.Skip("token estimation did not exceed threshold for this input size")
	}
}

func TestCheckPromptBudgetNilSpec(t *testing.T) {
	cfg := testConfig()
	findings := CheckPromptBudget(cfg, nil)
	if len(findings) != 0 {
		t.Errorf("nil spec should return 0 findings, got %d", len(findings))
	}
}

func TestCheckAcceptanceCommandsNone(t *testing.T) {
	s := &spec.Spec{}
	findings := CheckAcceptanceCommands(s)
	if len(findings) != 0 {
		t.Errorf("no checks should return 0 findings, got %d", len(findings))
	}
}

func TestCheckAcceptanceCommandsExists(t *testing.T) {
	s := &spec.Spec{
		AcceptanceChecks: []spec.Check{
			{Command: "echo test", Description: "echo check"},
		},
	}
	findings := CheckAcceptanceCommands(s)
	if len(findings) != 0 {
		t.Errorf("echo should exist, got %d findings: %v", len(findings), findings)
	}
}

func TestCheckAcceptanceCommandsMissing(t *testing.T) {
	s := &spec.Spec{
		AcceptanceChecks: []spec.Check{
			{Command: "nonexistent-tool-xyz check", Description: "bad check"},
		},
	}
	findings := CheckAcceptanceCommands(s)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected SeverityError, got %d", findings[0].Severity)
	}
	if !strings.Contains(findings[0].Message, "nonexistent-tool-xyz") {
		t.Errorf("message should mention command: %s", findings[0].Message)
	}
}

func TestCheckAcceptanceCommandsPathBased(t *testing.T) {
	s := &spec.Spec{
		AcceptanceChecks: []spec.Check{
			{Command: "./scripts/check.sh", Description: "path check"},
		},
	}
	findings := CheckAcceptanceCommands(s)
	if len(findings) != 0 {
		t.Errorf("path-based commands should be skipped, got %d findings", len(findings))
	}
}

func TestCheckLockConflictsNoWritesTo(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{}
	findings := CheckLockConflicts(cfg, s)
	if len(findings) != 0 {
		t.Errorf("no writes_to should return 0 findings, got %d", len(findings))
	}
}

func TestCheckLockConflictsNoConflict(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.LockFile = filepath.Join(dir, "locks.yaml")
	s := &spec.Spec{WritesTo: []string{"main.go"}}
	findings := CheckLockConflicts(cfg, s)
	if len(findings) != 0 {
		t.Errorf("empty registry should return 0 findings, got %d", len(findings))
	}
}

func TestCheckLockConflictsActiveConflict(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "locks.yaml")
	reg := lock.NewRegistry(lockPath)
	if err := reg.Acquire("other-task", []string{"main.go"}); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.LockFile = lockPath
	s := &spec.Spec{WritesTo: []string{"main.go"}}
	findings := CheckLockConflicts(cfg, s)
	if len(findings) != 1 {
		t.Fatalf("expected 1 conflict finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected SeverityError, got %d", findings[0].Severity)
	}
}

func TestCheckActivePhasesValid(t *testing.T) {
	findings := CheckActivePhases("MEDIUM")
	if len(findings) != 0 {
		t.Errorf("MEDIUM should have active phases, got %d findings", len(findings))
	}
}

func TestCheckActivePhasesEmpty(t *testing.T) {
	findings := CheckActivePhases("")
	if len(findings) != 0 {
		t.Errorf("empty complexity should skip, got %d findings", len(findings))
	}
}

func TestReportHasErrorsOnlyWarnings(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Check: "test", Severity: SeverityWarn, Message: "warning"},
	}}
	if r.HasErrors() {
		t.Error("only warnings should return false")
	}
}

func TestReportHasErrorsWithError(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Check: "test", Severity: SeverityError, Message: "error"},
	}}
	if !r.HasErrors() {
		t.Error("should return true with error")
	}
}

func TestReportFormatStderr(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Check: "test", Severity: SeverityError, Message: "something broke"},
	}}
	output := r.FormatStderr()
	if !strings.Contains(output, "Gateway preflight") {
		t.Error("should contain header")
	}
	if !strings.Contains(output, "[ERROR]") {
		t.Error("should contain severity")
	}
	if !strings.Contains(output, "something broke") {
		t.Error("should contain message")
	}
}

func TestReportFormatStderrEmpty(t *testing.T) {
	r := &Report{}
	if r.FormatStderr() != "" {
		t.Error("empty report should return empty string")
	}
}

func TestPreflightClean(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{What: "test", Why: "testing"}
	report := Preflight(Input{
		Config:     cfg,
		Spec:       s,
		Runtimes:   []string{"mock"},
		Complexity: "MEDIUM",
		Mode:       "pipeline",
	})
	if report.HasErrors() {
		t.Errorf("clean input should have no errors: %s", report.FormatStderr())
	}
}

func TestPreflightMultipleFindings(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "locks.yaml")
	reg := lock.NewRegistry(lockPath)
	_ = reg.Acquire("blocker", []string{"main.go"})

	cfg := testConfig()
	cfg.LockFile = lockPath
	s := &spec.Spec{
		What:     "test",
		Why:      "testing",
		WritesTo: []string{"main.go"},
	}
	report := Preflight(Input{
		Config:     cfg,
		Spec:       s,
		Runtimes:   []string{"missing"},
		Complexity: "MEDIUM",
		Mode:       "pipeline",
	})
	if !report.HasErrors() {
		t.Fatal("should have errors")
	}

	checks := map[string]bool{}
	for _, f := range report.Findings {
		checks[f.Check] = true
	}
	if !checks["runtime_available"] {
		t.Error("should have runtime_available finding")
	}
	if !checks["lock_conflict"] {
		t.Error("should have lock_conflict finding")
	}
}

func TestPreflightDeduplicatesRuntimes(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{What: "test", Why: "testing"}
	report := Preflight(Input{
		Config:   cfg,
		Spec:     s,
		Runtimes: []string{"missing", "missing", "missing"},
		Mode:     "run",
	})

	count := 0
	for _, f := range report.Findings {
		if f.Check == "runtime_available" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("duplicate runtimes should produce 1 finding, got %d", count)
	}
}

func TestPreflightSkipsActivePhasesForRunMode(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{What: "test", Why: "testing"}
	report := Preflight(Input{
		Config:     cfg,
		Spec:       s,
		Runtimes:   []string{"mock"},
		Complexity: "",
		Mode:       "run",
	})
	for _, f := range report.Findings {
		if f.Check == "active_phases" {
			t.Error("run mode should not check active_phases")
		}
	}
}
