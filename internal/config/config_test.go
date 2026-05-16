package config

import (
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
