package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if cfg.ClarifyExit != 10 {
		t.Errorf("expected clarification exit code 10, got %d", cfg.ClarifyExit)
	}
	if cfg.AbsoluteTimeout != "60m" {
		t.Errorf("expected absolute timeout 60m, got %s", cfg.AbsoluteTimeout)
	}
	if cfg.SilenceTimeout != "5m" {
		t.Errorf("expected silence timeout 5m, got %s", cfg.SilenceTimeout)
	}
	if cfg.SilenceWarning != "3m" {
		t.Errorf("expected silence warning 3m, got %s", cfg.SilenceWarning)
	}
	if len(cfg.ClarifyPatterns) == 0 {
		t.Error("expected default clarification patterns")
	}
}

func TestValidateRuntime(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"opencode": {
				Command: "opencode",
				Models:  []string{"kimi", "deepseek"},
			},
		},
	}

	if err := cfg.ValidateRuntime("opencode", "kimi"); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}

	if err := cfg.ValidateRuntime("opencode", "gpt-4o"); err == nil {
		t.Error("expected error for invalid model")
	}

	if err := cfg.ValidateRuntime("aider", "kimi"); err == nil {
		t.Error("expected error for unknown runtime")
	}
}

func TestResolveRuntime(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{Runtime: "opencode", Model: "kimi"},
	}

	r, m := cfg.ResolveRuntime("", "")
	if r != "opencode" || m != "kimi" {
		t.Errorf("expected opencode/kimi defaults, got %s/%s", r, m)
	}

	r, m = cfg.ResolveRuntime("aider", "gpt-4o")
	if r != "aider" || m != "gpt-4o" {
		t.Errorf("expected aider/gpt-4o, got %s/%s", r, m)
	}
}

func TestInstructionsFilename(t *testing.T) {
	tests := []struct {
		name     string
		runtime  string
		explicit string
		want     string
	}{
		{"claude-code auto", "claude-code", "", "CLAUDE.md"},
		{"cursor auto", "cursor", "", ".cursorrules"},
		{"windsurf auto", "windsurf", "", ".windsurfrules"},
		{"cline auto", "cline", "", ".clinerules"},
		{"unknown runtime", "something", "", "AGENTS.md"},
		{"empty runtime", "", "", "AGENTS.md"},
		{"explicit override", "claude-code", "CUSTOM.md", "CUSTOM.md"},
		{"explicit beats auto", "cursor", "AGENTS.md", "AGENTS.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Orchestrator: OrchestratorConfig{
					Runtime:          tt.runtime,
					InstructionsFile: tt.explicit,
				},
			}
			got := cfg.InstructionsFilename()
			if got != tt.want {
				t.Errorf("InstructionsFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeAvailableExists(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"test": {Command: "echo"},
		},
	}
	if !cfg.RuntimeAvailable("test") {
		t.Error("echo should be available")
	}
}

func TestRuntimeAvailableMissing(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"bad": {Command: "nonexistent-binary-xyz-12345"},
		},
	}
	if cfg.RuntimeAvailable("bad") {
		t.Error("nonexistent binary should not be available")
	}
}

func TestRuntimeAvailableUnknown(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{},
	}
	if cfg.RuntimeAvailable("ghost") {
		t.Error("unknown runtime should return false")
	}
}

func TestMergeFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agents.yaml")

	content := `
defaults:
  runtime: aider
  model: gpt-4o
clarification_exit_code: 42
gateway:
  strict: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := mergeFromFile(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}

	if cfg.Defaults.Runtime != "aider" {
		t.Errorf("expected runtime aider, got %s", cfg.Defaults.Runtime)
	}
	if cfg.Defaults.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", cfg.Defaults.Model)
	}
	if cfg.ClarifyExit != 42 {
		t.Errorf("expected clarify exit 42, got %d", cfg.ClarifyExit)
	}
	if !cfg.Gateway.Strict {
		t.Error("expected gateway strict true")
	}
	// Default fields should be preserved when not overridden
	if cfg.EventLog != ".baton/events.ndjson" {
		t.Errorf("expected default event log preserved, got %s", cfg.EventLog)
	}
}

func TestMergeFromFileNotFound(t *testing.T) {
	cfg := defaultConfig()
	err := mergeFromFile(cfg, "/nonexistent/path/agents.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestMergeFromFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(cfgPath, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	err := mergeFromFile(cfg, cfgPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestMergeFromFilePhaseMachine(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agents.yaml")

	content := `
phase_machine:
  enabled: true
  max_l1_retries: 5
  max_l3_cycles: 2
  compaction_gate_threshold: 0.9
  context_budget_tokens: 200000
  l3_escalation_runtime: claude-code
  l3_escalation_model: opus
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := mergeFromFile(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}

	if !cfg.PhaseMachine.Enabled {
		t.Error("expected phase_machine enabled")
	}
	if cfg.PhaseMachine.MaxL1Retries != 5 {
		t.Errorf("expected max_l1_retries 5, got %d", cfg.PhaseMachine.MaxL1Retries)
	}
	if cfg.PhaseMachine.MaxL3Cycles != 2 {
		t.Errorf("expected max_l3_cycles 2, got %d", cfg.PhaseMachine.MaxL3Cycles)
	}
	if cfg.PhaseMachine.CompactionGateThreshold != 0.9 {
		t.Errorf("expected threshold 0.9, got %f", cfg.PhaseMachine.CompactionGateThreshold)
	}
	if cfg.PhaseMachine.ContextBudgetTokens != 200000 {
		t.Errorf("expected budget 200000, got %d", cfg.PhaseMachine.ContextBudgetTokens)
	}
	if cfg.PhaseMachine.L3EscalationRuntime != "claude-code" {
		t.Errorf("expected l3 runtime claude-code, got %s", cfg.PhaseMachine.L3EscalationRuntime)
	}
}

func TestMergeFromFileRuntimes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agents.yaml")

	content := `
runtimes:
  opencode:
    command: opencode
    model_flag: "--model"
    models:
      - kimi
      - deepseek
    rate_limit:
      patterns:
        - "rate limit"
        - "429"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := mergeFromFile(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}

	rt, ok := cfg.Runtimes["opencode"]
	if !ok {
		t.Fatal("expected opencode runtime")
	}
	if rt.Command != "opencode" {
		t.Errorf("expected command opencode, got %s", rt.Command)
	}
	if len(rt.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(rt.Models))
	}
	if rt.RateLimit == nil {
		t.Fatal("expected rate_limit config")
	}
	if len(rt.RateLimit.Patterns) != 2 {
		t.Errorf("expected 2 rate limit patterns, got %d", len(rt.RateLimit.Patterns))
	}
}

func TestResolveRuntimePartialOverride(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{Runtime: "opencode", Model: "kimi"},
	}

	r, m := cfg.ResolveRuntime("aider", "")
	if r != "aider" {
		t.Errorf("expected aider, got %s", r)
	}
	if m != "kimi" {
		t.Errorf("expected default model kimi, got %s", m)
	}

	r, m = cfg.ResolveRuntime("", "opus")
	if r != "opencode" {
		t.Errorf("expected default runtime opencode, got %s", r)
	}
	if m != "opus" {
		t.Errorf("expected opus, got %s", m)
	}
}
