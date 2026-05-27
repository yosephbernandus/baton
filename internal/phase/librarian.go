package phase

import (
	"sort"

	"github.com/yosephbernandus/baton/internal/session"
)

type PhaseGroup int

const (
	GroupPlanning       PhaseGroup = iota
	GroupImplementation
	GroupVerification
	GroupTesting
	GroupCompletion
)

func PhaseGroupOf(phaseID int) PhaseGroup {
	switch {
	case phaseID >= 1 && phaseID <= 7:
		return GroupPlanning
	case phaseID == 8:
		return GroupImplementation
	case phaseID >= 9 && phaseID <= 11:
		return GroupVerification
	case phaseID >= 12 && phaseID <= 15:
		return GroupTesting
	case phaseID == 16:
		return GroupCompletion
	default:
		return GroupPlanning
	}
}

type RecordTier int

const (
	TierFull    RecordTier = iota
	TierSummary
	TierMinimal
	TierOmit
)

type ScoredRecord struct {
	Record session.PhaseRecord
	Score  float64
	Tier   RecordTier
}

type PhaseBudget struct {
	MaxRecordTokens   int
	FullTierCutoff    float64
	SummaryTierCutoff float64
	MinimalTierCutoff float64
}

type affinityEntry struct {
	sourcePhaseID int
	weight        float64
}

var phaseAffinities = map[int][]affinityEntry{
	8:  {{7, 1.0}, {6, 0.8}, {3, 0.6}, {5, 0.5}},
	9:  {{7, 1.0}, {8, 1.0}, {6, 0.5}},
	10: {{8, 1.0}, {3, 0.7}, {4, 0.6}},
	11: {{8, 1.0}, {7, 0.8}, {6, 0.4}},
	12: {{8, 1.0}, {7, 0.7}, {3, 0.5}},
	13: {{12, 1.0}, {8, 0.9}},
	14: {{13, 1.0}, {8, 0.8}, {12, 0.7}},
	15: {{13, 1.0}, {12, 0.8}, {8, 0.6}},
	16: {{8, 1.0}, {7, 0.8}, {13, 0.8}, {3, 0.5}},
}

var defaultBudgets = map[PhaseGroup]PhaseBudget{
	GroupPlanning: {
		MaxRecordTokens:   5000,
		FullTierCutoff:    0.7,
		SummaryTierCutoff: 0.3,
		MinimalTierCutoff: 0.1,
	},
	GroupImplementation: {
		MaxRecordTokens:   20000,
		FullTierCutoff:    0.6,
		SummaryTierCutoff: 0.3,
		MinimalTierCutoff: 0.1,
	},
	GroupVerification: {
		MaxRecordTokens:   15000,
		FullTierCutoff:    0.6,
		SummaryTierCutoff: 0.25,
		MinimalTierCutoff: 0.1,
	},
	GroupTesting: {
		MaxRecordTokens:   18000,
		FullTierCutoff:    0.6,
		SummaryTierCutoff: 0.3,
		MinimalTierCutoff: 0.1,
	},
	GroupCompletion: {
		MaxRecordTokens:   25000,
		FullTierCutoff:    0.5,
		SummaryTierCutoff: 0.2,
		MinimalTierCutoff: 0.05,
	},
}

func ResolveBudget(phaseID int, configBudgets map[string]int) PhaseBudget {
	group := PhaseGroupOf(phaseID)
	budget := defaultBudgets[group]

	if configBudgets != nil {
		groupName := groupToString(group)
		if override, ok := configBudgets[groupName]; ok {
			budget.MaxRecordTokens = override
		}
	}
	return budget
}

func groupToString(g PhaseGroup) string {
	switch g {
	case GroupPlanning:
		return "planning"
	case GroupImplementation:
		return "implementation"
	case GroupVerification:
		return "verification"
	case GroupTesting:
		return "testing"
	case GroupCompletion:
		return "completion"
	default:
		return "planning"
	}
}

func ScoreRecords(
	records []session.PhaseRecord,
	currentPhase Phase,
	dirtyFiles map[int][]string,
	l2Active, l3Active bool,
) []ScoredRecord {
	affinities := phaseAffinities[currentPhase.ID]
	affinityMap := make(map[int]float64, len(affinities))
	for _, a := range affinities {
		affinityMap[a.sourcePhaseID] = a.weight
	}

	scored := make([]ScoredRecord, len(records))
	for i, r := range records {
		scored[i] = ScoredRecord{
			Record: r,
			Score:  computeScore(r, currentPhase, affinityMap, dirtyFiles, l2Active, l3Active),
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	return scored
}

func computeScore(
	r session.PhaseRecord,
	current Phase,
	affinityMap map[int]float64,
	dirtyFiles map[int][]string,
	l2Active, l3Active bool,
) float64 {
	var score float64

	if w, ok := affinityMap[r.ID]; ok {
		score += w * 0.4
	}

	sourceRole := phaseRole(r.ID)
	if sourceRole == current.Role {
		score += 0.15
	} else if roleGroupMatch(sourceRole, current.Role) {
		score += 0.08
	}

	distance := current.ID - r.ID
	if distance <= 0 {
		distance = 1
	}
	score += 0.15 * (1.0 / (1.0 + 0.3*float64(distance)))

	if len(r.FilesChanged) > 0 && len(dirtyFiles) > 0 {
		score += fileOverlapRatio(r.FilesChanged, dirtyFiles, current.ID) * 0.15
	}

	if len(r.Errors) > 0 {
		score += 0.02
	}
	if len(r.Notes) > 0 {
		score += 0.02
	}
	if r.Attempts > 1 {
		score += 0.01
	}

	if (l2Active || l3Active) && len(r.Errors) > 0 {
		score += 0.1
	}
	if (l2Active || l3Active) && IsVerificationPhase(r.ID) && r.Status != "completed" {
		score += 0.05
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func phaseRole(phaseID int) string {
	for _, p := range DefaultPhases() {
		if p.ID == phaseID {
			return p.Role
		}
	}
	return ""
}

func roleGroupMatch(a, b string) bool {
	analysisRoles := map[string]bool{RoleLead: true, RoleReviewer: true, RoleTestLead: true}
	executionRoles := map[string]bool{RoleDeveloper: true, RoleTester: true}
	return (analysisRoles[a] && analysisRoles[b]) || (executionRoles[a] && executionRoles[b])
}

func fileOverlapRatio(recordFiles []string, dirtyFiles map[int][]string, currentPhaseID int) float64 {
	allDirty := make(map[string]bool)
	for pid, files := range dirtyFiles {
		if pid < currentPhaseID {
			for _, f := range files {
				allDirty[f] = true
			}
		}
	}
	if len(allDirty) == 0 {
		return 0.0
	}
	overlap := 0
	for _, f := range recordFiles {
		if allDirty[f] {
			overlap++
		}
	}
	return float64(overlap) / float64(len(recordFiles))
}

func AssignTiers(scored []ScoredRecord, budget PhaseBudget) []ScoredRecord {
	remaining := budget.MaxRecordTokens
	if remaining <= 0 {
		for i := range scored {
			scored[i].Tier = TierMinimal
		}
		return scored
	}

	for i := range scored {
		s := &scored[i]
		switch {
		case s.Score >= budget.FullTierCutoff && remaining > 0:
			s.Tier = TierFull
			remaining -= EstimateRecordTokens(s.Record, TierFull)
		case s.Score >= budget.SummaryTierCutoff && remaining > 0:
			s.Tier = TierSummary
			remaining -= EstimateRecordTokens(s.Record, TierSummary)
		case s.Score >= budget.MinimalTierCutoff && remaining > 0:
			s.Tier = TierMinimal
			remaining -= EstimateRecordTokens(s.Record, TierMinimal)
		default:
			s.Tier = TierOmit
		}

		if remaining <= 0 {
			for j := i + 1; j < len(scored); j++ {
				if scored[j].Score >= budget.MinimalTierCutoff {
					scored[j].Tier = TierMinimal
				} else {
					scored[j].Tier = TierOmit
				}
			}
			break
		}
	}
	return scored
}

func EstimateRecordTokens(r session.PhaseRecord, tier RecordTier) int {
	switch tier {
	case TierFull:
		tokens := 20
		for _, n := range r.Notes {
			tokens += EstimateTokens(n)
		}
		for _, e := range r.Errors {
			tokens += EstimateTokens(e)
		}
		for _, f := range r.FilesChanged {
			tokens += EstimateTokens(f)
		}
		if r.FailReason != "" {
			tokens += EstimateTokens(r.FailReason)
		}
		return tokens
	case TierSummary:
		tokens := 15
		if len(r.Notes) > 0 {
			note := r.Notes[0]
			if len(note) > 200 {
				note = note[:200]
			}
			tokens += EstimateTokens(note)
		}
		for _, f := range r.FilesChanged {
			tokens += EstimateTokens(f)
		}
		return tokens
	case TierMinimal:
		return 10 + len(r.FilesChanged)*3
	default:
		return 0
	}
}
