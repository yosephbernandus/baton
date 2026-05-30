package cmd

import (
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/decisions"
	"github.com/yosephbernandus/baton/internal/knowledge"
	"github.com/yosephbernandus/baton/internal/phase"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello world", 5, "hello..."},
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"", 5, ""},
		{"abc", 0, "..."},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestExitError(t *testing.T) {
	err := exitError(3, "spec error: %s", "missing field")
	if err.Code != 3 {
		t.Errorf("expected code 3, got %d", err.Code)
	}
	if err.Message != "spec error: missing field" {
		t.Errorf("expected formatted message, got %q", err.Message)
	}
	if err.Error() != "spec error: missing field" {
		t.Errorf("Error() should return message, got %q", err.Error())
	}
}

func TestExitErrorEmptyMessage(t *testing.T) {
	err := exitError(10, "")
	if err.Code != 10 {
		t.Errorf("expected code 10, got %d", err.Code)
	}
	if err.Message != "" {
		t.Errorf("expected empty message, got %q", err.Message)
	}
}

func TestFindNextPhaseID(t *testing.T) {
	active := phase.ActivePhases(phase.DefaultPhases(), phase.ComplexityMedium)

	// After phase 0 (nothing completed), should return first active
	next := findNextPhaseID(active, 0)
	if next != active[0].ID {
		t.Errorf("expected first phase %d, got %d", active[0].ID, next)
	}

	// After last phase completed
	lastID := active[len(active)-1].ID
	next = findNextPhaseID(active, lastID)
	if next != 0 {
		t.Errorf("expected 0 when all done, got %d", next)
	}

	// After middle phase
	midIdx := len(active) / 2
	next = findNextPhaseID(active, active[midIdx].ID)
	if next != active[midIdx+1].ID {
		t.Errorf("expected phase %d, got %d", active[midIdx+1].ID, next)
	}
}

func TestResolveComplexity(t *testing.T) {
	tests := []struct {
		flag, spec, config, want string
	}{
		{"LARGE", "MEDIUM", "SMALL", "LARGE"},
		{"", "MEDIUM", "SMALL", "MEDIUM"},
		{"", "", "SMALL", "SMALL"},
		{"", "", "", phase.ComplexityMedium},
	}
	for _, tt := range tests {
		got := resolveComplexity(tt.flag, tt.spec, tt.config)
		if got != tt.want {
			t.Errorf("resolveComplexity(%q, %q, %q) = %q, want %q",
				tt.flag, tt.spec, tt.config, got, tt.want)
		}
	}
}

func TestPhaseNames(t *testing.T) {
	phases := []phase.Phase{
		{ID: 1, Name: "setup"},
		{ID: 8, Name: "implementation"},
		{ID: 16, Name: "completion"},
	}
	got := phaseNames(phases)
	if !strings.Contains(got, "1:setup") {
		t.Errorf("expected 1:setup in output, got %s", got)
	}
	if !strings.Contains(got, "8:implementation") {
		t.Errorf("expected 8:implementation in output, got %s", got)
	}
	if !strings.Contains(got, "16:completion") {
		t.Errorf("expected 16:completion in output, got %s", got)
	}
}

func TestPhaseNamesEmpty(t *testing.T) {
	got := phaseNames(nil)
	if got != "" {
		t.Errorf("expected empty string for nil phases, got %q", got)
	}
}

func TestSearchDecisions(t *testing.T) {
	records := []decisions.Record{
		{Question: "Which database to use?", Answer: "PostgreSQL", Reason: "team standard"},
		{Question: "Auth method?", Answer: "OAuth2", Reason: "security"},
		{Question: "Cache strategy?", Answer: "Redis", Reason: "performance"},
	}

	// Match by question
	matches := searchDecisions(records, "database")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'database', got %d", len(matches))
	}
	if matches[0].Answer != "PostgreSQL" {
		t.Errorf("expected PostgreSQL, got %s", matches[0].Answer)
	}

	// Match by answer content
	matches = searchDecisions(records, "oauth2")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'oauth2', got %d", len(matches))
	}

	// No match
	matches = searchDecisions(records, "kubernetes")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}

	// Empty query
	matches = searchDecisions(records, "")
	if matches != nil {
		t.Errorf("expected nil for empty query, got %v", matches)
	}
}

func TestValueOr(t *testing.T) {
	if got := valueOr("value", "fallback"); got != "value" {
		t.Errorf("expected value, got %s", got)
	}
	if got := valueOr("", "fallback"); got != "fallback" {
		t.Errorf("expected fallback, got %s", got)
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a, b, c", []string{"a", "b", "c"}},
		{"one", []string{"one"}},
		{"  x ,  y  ", []string{"x", "y"}},
		{",,,", nil},
		{"", nil},
	}
	for _, tt := range tests {
		got := splitAndTrim(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitAndTrim(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitAndTrim(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"a\nb\nc", 3},
		{"single", 1},
		{"", 0},
		{"a\n\nb", 3}, // empty lines preserved
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitLines(%q) len = %d, want %d: %v", tt.input, len(got), tt.want, got)
		}
	}
}

func TestLspInstallHint(t *testing.T) {
	tests := []struct {
		lang string
		lsp  string
		want string
	}{
		{"python", "pyright-langserver", "pip install pyright"},
		{"typescript", "typescript-language-server", "npm i -g typescript-language-server"},
		{"rust", "rust-analyzer", "rustup component add rust-analyzer"},
		{"ruby", "ruby-lsp", "gem install ruby-lsp"},
		{"cpp", "clangd", "brew install llvm"},
		{"unknown", "some-lsp", "install some-lsp"},
	}
	for _, tt := range tests {
		got := lspInstallHint(knowledge.DetectedLang{Name: tt.lang, LSP: tt.lsp})
		if !strings.Contains(got, tt.want) {
			t.Errorf("lspInstallHint(%s) = %q, want to contain %q", tt.lang, got, tt.want)
		}
	}
}
