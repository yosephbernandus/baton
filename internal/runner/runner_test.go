package runner

import (
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
)

func TestBuildToolRestrictionFlagsCommaSeparated(t *testing.T) {
	rt := &config.RuntimeConfig{
		ToolRestriction: &config.ToolRestriction{
			Flag:   "--allowedTools",
			Format: "comma-separated",
		},
	}
	got := BuildToolRestrictionFlags(rt, []string{"Read", "Grep", "Glob"})
	if len(got) != 2 {
		t.Fatalf("expected 2 elements, got %d: %v", len(got), got)
	}
	if got[0] != "--allowedTools" {
		t.Errorf("flag=%q", got[0])
	}
	if got[1] != "Read,Grep,Glob" {
		t.Errorf("value=%q", got[1])
	}
}

func TestBuildToolRestrictionFlagsRepeat(t *testing.T) {
	rt := &config.RuntimeConfig{
		ToolRestriction: &config.ToolRestriction{
			Flag:   "--tool",
			Format: "repeat",
		},
	}
	got := BuildToolRestrictionFlags(rt, []string{"Read", "Bash"})
	if len(got) != 4 {
		t.Fatalf("expected 4 elements, got %d: %v", len(got), got)
	}
	if got[0] != "--tool" || got[1] != "Read" || got[2] != "--tool" || got[3] != "Bash" {
		t.Errorf("got %v", got)
	}
}

func TestBuildToolRestrictionFlagsNilRestriction(t *testing.T) {
	rt := &config.RuntimeConfig{}
	got := BuildToolRestrictionFlags(rt, []string{"Read"})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestBuildToolRestrictionFlagsNoTools(t *testing.T) {
	rt := &config.RuntimeConfig{
		ToolRestriction: &config.ToolRestriction{
			Flag:   "--allowedTools",
			Format: "comma-separated",
		},
	}
	got := BuildToolRestrictionFlags(rt, nil)
	if got != nil {
		t.Errorf("expected nil for empty tools, got %v", got)
	}
}

func TestBuildToolRestrictionFlagsEmptyFlag(t *testing.T) {
	rt := &config.RuntimeConfig{
		ToolRestriction: &config.ToolRestriction{
			Flag:   "",
			Format: "comma-separated",
		},
	}
	got := BuildToolRestrictionFlags(rt, []string{"Read"})
	if got != nil {
		t.Errorf("expected nil for empty flag, got %v", got)
	}
}

func TestBuildArgsStdinModeSkipsPrompt(t *testing.T) {
	rt := &config.RuntimeConfig{
		Command:    "claude",
		ModelFlag:  "--model",
		PromptFlag: "-p",
		PromptMode: "stdin",
		ExtraFlags: []string{"--dangerously-skip-permissions"},
	}
	args := buildArgs(rt, "sonnet", "do something", nil)
	for _, a := range args {
		if a == "do something" {
			t.Fatal("prompt should not appear in args with stdin mode")
		}
		if a == "-p" {
			t.Fatal("-p flag should not appear with stdin mode")
		}
	}
	if args[0] != "--model" || args[1] != "sonnet" {
		t.Errorf("expected model flag, got %v", args)
	}
}

func TestBuildArgsStdinModeSkipsPositionalPrompt(t *testing.T) {
	rt := &config.RuntimeConfig{
		Command:    "opencode",
		ModelFlag:  "--model",
		Positional: []string{"run", "{{prompt}}"},
		PromptMode: "stdin",
	}
	args := buildArgs(rt, "gpt-4", "my prompt", nil)
	if len(args) != 3 {
		t.Fatalf("expected 3 args (run, --model, gpt-4), got %d: %v", len(args), args)
	}
	if args[0] != "run" {
		t.Errorf("expected 'run', got %q", args[0])
	}
	for _, a := range args {
		if a == "my prompt" {
			t.Fatal("prompt should not appear in positional args with stdin mode")
		}
	}
}

func TestBuildArgsNormalModeIncludesPrompt(t *testing.T) {
	rt := &config.RuntimeConfig{
		Command:    "claude",
		PromptFlag: "-p",
	}
	args := buildArgs(rt, "", "do something", nil)
	found := false
	for _, a := range args {
		if a == "do something" {
			found = true
		}
	}
	if !found {
		t.Fatal("prompt should appear in args without stdin mode")
	}
}

func TestExtractClarification(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"CLARIFICATION_NEEDED: which schema?", "which schema?"},
		{"some output CLARIFICATION_NEEDED: what version?", "what version?"},
		{"normal output line", ""},
		{"CLARIFICATION_NEEDED:", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractClarification(tt.line)
		if got != tt.want {
			t.Errorf("extractClarification(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
