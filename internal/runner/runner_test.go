package runner

import "testing"

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
