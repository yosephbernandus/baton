package routing

import (
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/decisions"
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

func TestAnalyzeClarification_Empty(t *testing.T) {
	v := AnalyzeClarification(ClarifyContext{})
	if v.Confidence != "none" {
		t.Errorf("expected none confidence, got %s", v.Confidence)
	}
	if v.CanAutoAnswer {
		t.Error("should not auto-answer empty clarification")
	}
}

func TestAnalyzeClarification_SpecDecisionMatch(t *testing.T) {
	v := AnalyzeClarification(ClarifyContext{
		Clarification: "which database should I use for storage?",
		Spec: &spec.Spec{
			Decisions: []spec.Decision{
				{Question: "which database for storage", Answer: "PostgreSQL", Reason: "team standard"},
			},
		},
	})
	if !v.CanAutoAnswer {
		t.Fatal("should auto-answer from spec decision")
	}
	if v.Answer != "PostgreSQL" {
		t.Errorf("expected PostgreSQL, got %s", v.Answer)
	}
	if v.Source != "spec decisions" {
		t.Errorf("expected source 'spec decisions', got %s", v.Source)
	}
	if v.Confidence != "high" {
		t.Errorf("expected high confidence, got %s", v.Confidence)
	}
}

func TestAnalyzeClarification_DecisionsYamlMatch(t *testing.T) {
	v := AnalyzeClarification(ClarifyContext{
		Clarification: "what authentication method should we use?",
		Decisions: []decisions.Record{
			{Question: "authentication method", Answer: "OAuth2", Reason: "security policy", DecidedBy: "architect"},
		},
	})
	if !v.CanAutoAnswer {
		t.Fatal("should auto-answer from decisions.yaml")
	}
	if v.Answer != "OAuth2" {
		t.Errorf("expected OAuth2, got %s", v.Answer)
	}
	if v.Confidence != "medium" {
		t.Errorf("expected medium confidence, got %s", v.Confidence)
	}
}

func TestAnalyzeClarification_SpecTakesPrecedence(t *testing.T) {
	v := AnalyzeClarification(ClarifyContext{
		Clarification: "which database for storage?",
		Spec: &spec.Spec{
			Decisions: []spec.Decision{
				{Question: "which database for storage", Answer: "PostgreSQL", Reason: "from spec"},
			},
		},
		Decisions: []decisions.Record{
			{Question: "which database for storage", Answer: "MySQL", Reason: "from yaml"},
		},
	})
	if v.Answer != "PostgreSQL" {
		t.Errorf("spec should take precedence, got %s", v.Answer)
	}
	if v.Source != "spec decisions" {
		t.Errorf("expected spec decisions source, got %s", v.Source)
	}
}

func TestAnalyzeClarification_BriefPartialMatch(t *testing.T) {
	v := AnalyzeClarification(ClarifyContext{
		Clarification: "what logging framework should we use?",
		ProjectBrief:  "We use structured logging with zap framework throughout the project.",
	})
	if v.CanAutoAnswer {
		t.Error("brief match should not auto-answer")
	}
	if v.Confidence != "low" {
		t.Errorf("expected low confidence for brief match, got %s", v.Confidence)
	}
	if v.Source != "project brief (partial match)" {
		t.Errorf("expected brief source, got %s", v.Source)
	}
}

func TestAnalyzeClarification_NoMatch(t *testing.T) {
	v := AnalyzeClarification(ClarifyContext{
		Clarification: "completely unrelated question about quantum physics",
		Spec:          &spec.Spec{},
		ProjectBrief:  "This is a web application for managing tasks.",
	})
	if v.CanAutoAnswer {
		t.Error("should not auto-answer unrelated question")
	}
	if v.Confidence != "none" {
		t.Errorf("expected none confidence, got %s", v.Confidence)
	}
}

func TestExtractKeywords(t *testing.T) {
	words := extractKeywords("which database should we use for the storage layer?")
	if len(words) == 0 {
		t.Fatal("expected keywords extracted")
	}

	// Stop words should be filtered
	for _, w := range words {
		if w == "the" || w == "we" || w == "for" || w == "which" {
			t.Errorf("stop word %q should be filtered", w)
		}
	}

	// Short words (<=2 chars) should be filtered
	for _, w := range words {
		if len(w) <= 2 {
			t.Errorf("short word %q should be filtered", w)
		}
	}
}

func TestExtractKeywordsEmpty(t *testing.T) {
	words := extractKeywords("")
	if len(words) != 0 {
		t.Errorf("expected 0 keywords from empty string, got %d", len(words))
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("database storage layer", "which database for storage") {
		t.Error("should match overlapping keywords")
	}
	if containsAny("quantum physics", "database storage") {
		t.Error("should not match unrelated content")
	}
}

func TestToStringSlice(t *testing.T) {
	// []interface{}
	s, ok := toStringSlice([]interface{}{"a", "b"})
	if !ok || len(s) != 2 {
		t.Errorf("expected [a b], got %v (ok=%v)", s, ok)
	}

	// []string
	s, ok = toStringSlice([]string{"x", "y"})
	if !ok || len(s) != 2 {
		t.Errorf("expected [x y], got %v (ok=%v)", s, ok)
	}

	// string
	s, ok = toStringSlice("single")
	if !ok || len(s) != 1 || s[0] != "single" {
		t.Errorf("expected [single], got %v (ok=%v)", s, ok)
	}

	// unsupported type
	_, ok = toStringSlice(42)
	if ok {
		t.Error("int should not convert")
	}
}

func TestMatchDomain(t *testing.T) {
	s := &spec.Spec{ContextFiles: []string{"src/Button.tsx"}}
	if !matchDomain([]interface{}{"frontend", "css"}, s) {
		t.Error("should match frontend for .tsx file")
	}
	if matchDomain([]interface{}{"infra", "k8s"}, s) {
		t.Error("should not match infra for .tsx file")
	}
}

func TestMatchDomainNoContextFiles(t *testing.T) {
	s := &spec.Spec{}
	if matchDomain([]interface{}{"frontend"}, s) {
		t.Error("should not match without context files")
	}
}

func TestExtractDomains(t *testing.T) {
	s := &spec.Spec{ContextFiles: []string{"src/App.tsx", "main_test.go", "deploy/main.tf"}}
	domains := extractDomains(s)
	if len(domains) != 3 {
		t.Fatalf("expected 3 domains, got %d: %v", len(domains), domains)
	}
}

func TestResolveField(t *testing.T) {
	if resolveField("value", "fallback") != "value" {
		t.Error("should return value when non-empty")
	}
	if resolveField("", "fallback") != "fallback" {
		t.Error("should return fallback when empty")
	}
}
