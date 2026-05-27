package phase

import (
	"strings"

	"github.com/yosephbernandus/baton/internal/session"
)

type CompactionGate struct {
	threshold    float64
	budgetTokens int
	gatePhases   map[int]bool
}

func NewCompactionGate(threshold float64, budget int, phases []int) *CompactionGate {
	if threshold <= 0 {
		threshold = 0.85
	}
	if budget <= 0 {
		budget = 180000
	}
	gp := make(map[int]bool)
	for _, p := range phases {
		gp[p] = true
	}
	if len(gp) == 0 {
		gp = map[int]bool{3: true, 8: true, 13: true}
	}
	return &CompactionGate{
		threshold:    threshold,
		budgetTokens: budget,
		gatePhases:   gp,
	}
}

func (g *CompactionGate) IsGatePhase(phaseID int) bool {
	return g.gatePhases[phaseID]
}

func (g *CompactionGate) ShouldCompact(tokenEstimate int) bool {
	return float64(tokenEstimate) > g.threshold*float64(g.budgetTokens)
}

func (g *CompactionGate) TokenLimit() int {
	return int(g.threshold * float64(g.budgetTokens))
}

// EstimateTokens approximates token count from text.
// Uses 1.3 tokens per word, tuned for code-heavy content.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	words := len(strings.Fields(text))
	return int(float64(words) * 1.3)
}

// CompactRecords strips verbose notes and errors from phase records,
// keeping only essential state for prompt construction.
func CompactRecords(records []session.PhaseRecord) []session.PhaseRecord {
	compacted := make([]session.PhaseRecord, len(records))
	for i, r := range records {
		compacted[i] = session.PhaseRecord{
			ID:           r.ID,
			Name:         r.Name,
			Status:       r.Status,
			FilesChanged: r.FilesChanged,
			Attempts:     r.Attempts,
			CompletedAt:  r.CompletedAt,
		}
		if len(r.Notes) > 0 {
			note := r.Notes[0]
			if len(note) > 100 {
				note = note[:100] + "..."
			}
			compacted[i].Notes = []string{note}
		}
	}
	return compacted
}
