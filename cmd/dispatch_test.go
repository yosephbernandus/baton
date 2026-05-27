package cmd

import (
	"testing"

	"github.com/yosephbernandus/baton/internal/phase"
)

func TestResolveDispatchMode(t *testing.T) {
	tests := []struct {
		name       string
		modeFlag   string
		cfgDefault string
		complexity string
		threshold  string
		want       string
	}{
		{"flag overrides everything", "coordinator", "pipeline", "TRIVIAL", "MEDIUM", "coordinator"},
		{"config default non-auto", "", "pipeline", "TRIVIAL", "MEDIUM", "pipeline"},
		{"auto trivial below threshold", "", "auto", "TRIVIAL", "MEDIUM", "run"},
		{"auto small below threshold", "", "auto", "SMALL", "MEDIUM", "run"},
		{"auto medium at threshold", "", "auto", "MEDIUM", "MEDIUM", "pipeline"},
		{"auto large above threshold", "", "auto", "LARGE", "MEDIUM", "pipeline"},
		{"empty config defaults to auto", "", "", "MEDIUM", "", "pipeline"},
		{"empty config trivial", "", "", "TRIVIAL", "", "run"},
		{"threshold small", "", "auto", "SMALL", "SMALL", "pipeline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDispatchMode(tt.modeFlag, tt.cfgDefault, tt.complexity, tt.threshold)
			if got != tt.want {
				t.Errorf("resolveDispatchMode(%q, %q, %q, %q) = %q, want %q",
					tt.modeFlag, tt.cfgDefault, tt.complexity, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestComplexityRankCoverage(t *testing.T) {
	for _, c := range []string{phase.ComplexityTrivial, phase.ComplexitySmall, phase.ComplexityMedium, phase.ComplexityLarge} {
		if _, ok := complexityRank[c]; !ok {
			t.Errorf("missing rank for %s", c)
		}
	}
}
