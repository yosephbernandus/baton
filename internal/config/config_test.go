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

func TestLoadConfigFromPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agents.yaml")

	content := []byte(`
orchestrator:
  runtime: claude-code
  model: sonnet
runtimes:
  opencode:
    command: "opencode"
    model_flag: "-m"
    prompt_flag: "-p"
    models:
      - kimi
      - deepseek
defaults:
  runtime: opencode
  model: kimi
`)
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromPath(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Orchestrator.Runtime != "claude-code" {
		t.Errorf("expected orchestrator runtime claude-code, got %s", cfg.Orchestrator.Runtime)
	}
	if cfg.Orchestrator.Model != "sonnet" {
		t.Errorf("expected orchestrator model sonnet, got %s", cfg.Orchestrator.Model)
	}
	if len(cfg.Runtimes) != 1 {
		t.Errorf("expected 1 runtime, got %d", len(cfg.Runtimes))
	}
	rt := cfg.Runtimes["opencode"]
	if rt.Command != "opencode" {
		t.Errorf("expected command opencode, got %s", rt.Command)
	}
	if len(rt.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(rt.Models))
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
