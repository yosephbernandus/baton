package routing

import (
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

func testConfig() *config.Config {
	return &config.Config{
		Defaults: config.DefaultsConfig{
			Runtime: "opencode",
			Model:   "kimi",
		},
		Routing: config.RoutingConfig{
			Rules: []config.RoutingRule{
				{
					Match:  map[string]interface{}{"criticality": "high"},
					Action: "escalate",
					Model:  "opus",
					Reason: "high criticality needs review",
				},
				{
					Match:   map[string]interface{}{"domain": []interface{}{"frontend", "css"}},
					Action:  "delegate",
					Runtime: "opencode",
					Model:   "kimi",
					Reason:  "frontend work",
				},
				{
					Match:   map[string]interface{}{"domain": []interface{}{"tests"}},
					Action:  "delegate",
					Runtime: "opencode",
					Model:   "gemini-flash",
					Reason:  "test generation is cheap",
				},
				{
					Match:  map[string]interface{}{"default": true},
					Action: "delegate",
					Model:  "kimi",
					Reason: "default routing",
				},
			},
		},
	}
}

func TestResolve_HighCriticality(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{
		What:        "fix auth",
		Criticality: "high",
	}
	r := Resolve(cfg, s)
	if r.Action != "escalate" {
		t.Errorf("expected escalate, got %s", r.Action)
	}
	if r.Model != "opus" {
		t.Errorf("expected opus, got %s", r.Model)
	}
}

func TestResolve_FrontendDomain(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{
		What:         "add button",
		ContextFiles: []string{"src/components/Button.tsx"},
	}
	r := Resolve(cfg, s)
	if r.Action != "delegate" {
		t.Errorf("expected delegate, got %s", r.Action)
	}
	if r.Model != "kimi" {
		t.Errorf("expected kimi, got %s", r.Model)
	}
	if r.Reason != "frontend work" {
		t.Errorf("expected 'frontend work', got %q", r.Reason)
	}
}

func TestResolve_TestDomain(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{
		What:         "add tests",
		ContextFiles: []string{"internal/config/config_test.go"},
	}
	r := Resolve(cfg, s)
	if r.Model != "gemini-flash" {
		t.Errorf("expected gemini-flash, got %s", r.Model)
	}
}

func TestResolve_Default(t *testing.T) {
	cfg := testConfig()
	s := &spec.Spec{
		What:         "refactor code",
		ContextFiles: []string{"internal/config/config.go"},
	}
	r := Resolve(cfg, s)
	if r.Action != "delegate" {
		t.Errorf("expected delegate, got %s", r.Action)
	}
	if r.Model != "kimi" {
		t.Errorf("expected kimi, got %s", r.Model)
	}
}

func TestResolve_NilSpec(t *testing.T) {
	cfg := testConfig()
	r := Resolve(cfg, nil)
	if r.Runtime != "opencode" {
		t.Errorf("expected opencode, got %s", r.Runtime)
	}
	if r.Model != "kimi" {
		t.Errorf("expected kimi, got %s", r.Model)
	}
}

func TestResolve_NoRules(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Runtime: "aider", Model: "gpt-4o"},
	}
	s := &spec.Spec{What: "test"}
	r := Resolve(cfg, s)
	if r.Runtime != "aider" || r.Model != "gpt-4o" {
		t.Errorf("expected aider/gpt-4o, got %s/%s", r.Runtime, r.Model)
	}
}

func TestInferDomain(t *testing.T) {
	tests := []struct {
		path   string
		domain string
	}{
		{"src/Button.tsx", "frontend"},
		{"styles/main.css", "css"},
		{"config_test.go", "tests"},
		{"deploy/main.tf", "infra"},
		{"internal/config.go", "general"},
	}

	for _, tt := range tests {
		got := inferDomain(tt.path)
		if got != tt.domain {
			t.Errorf("inferDomain(%q) = %q, want %q", tt.path, got, tt.domain)
		}
	}
}
