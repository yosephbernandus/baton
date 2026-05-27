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

func SmartCompact(
	records []session.PhaseRecord,
	currentPhase Phase,
	dirtyFiles map[int][]string,
	l2Active, l3Active bool,
	configBudgets map[string]int,
) []session.PhaseRecord {
	scored := ScoreRecords(records, currentPhase, dirtyFiles, l2Active, l3Active)
	budget := ResolveBudget(currentPhase.ID, configBudgets)
	scored = AssignTiers(scored, budget)

	compacted := make([]session.PhaseRecord, 0, len(records))
	for _, sr := range scored {
		r := sr.Record
		switch sr.Tier {
		case TierFull:
			compacted = append(compacted, r)
		case TierSummary:
			compacted = append(compacted, session.PhaseRecord{
				ID:           r.ID,
				Name:         r.Name,
				Status:       r.Status,
				Notes:        truncateNotes(r.Notes, 1, 200),
				Errors:       truncateNotes(r.Errors, 1, 150),
				FilesChanged: r.FilesChanged,
				Attempts:     r.Attempts,
				CompletedAt:  r.CompletedAt,
			})
		case TierMinimal:
			cr := session.PhaseRecord{
				ID:           r.ID,
				Name:         r.Name,
				Status:       r.Status,
				FilesChanged: r.FilesChanged,
				Attempts:     r.Attempts,
				CompletedAt:  r.CompletedAt,
			}
			if (l2Active || l3Active) && IsVerificationPhase(r.ID) && len(r.Errors) > 0 {
				cr.Errors = truncateNotes(r.Errors, 1, 200)
			}
			compacted = append(compacted, cr)
		case TierOmit:
			cr := session.PhaseRecord{
				ID:     r.ID,
				Name:   r.Name,
				Status: r.Status,
			}
			if (l2Active || l3Active) && IsVerificationPhase(r.ID) && len(r.Errors) > 0 {
				cr.Errors = truncateNotes(r.Errors, 1, 200)
			}
			compacted = append(compacted, cr)
		}
	}
	return compacted
}

func truncateNotes(notes []string, maxCount, maxLen int) []string {
	if len(notes) == 0 {
		return nil
	}
	result := make([]string, 0, maxCount)
	for i, n := range notes {
		if i >= maxCount {
			break
		}
		if len(n) > maxLen {
			n = n[:maxLen] + "..."
		}
		result = append(result, n)
	}
	return result
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
