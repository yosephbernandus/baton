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
