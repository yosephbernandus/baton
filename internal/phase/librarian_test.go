package phase

import (
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/session"
)

func TestScoreRecordsAffinityDominates(t *testing.T) {
	arch := session.PhaseRecord{ID: 7, Name: "architecture", Status: "completed", Notes: []string{"designed API"}}
	setup := session.PhaseRecord{ID: 1, Name: "setup", Status: "completed"}
	implPhase := Phase{ID: 8, Name: "implementation", Role: RoleDeveloper}

	scored := ScoreRecords([]session.PhaseRecord{setup, arch}, implPhase, nil, false, false)
	if len(scored) != 2 {
		t.Fatalf("expected 2 scored records, got %d", len(scored))
	}
	if scored[0].Record.ID != 7 {
		t.Errorf("architecture should rank first, got phase %d", scored[0].Record.ID)
	}
	if scored[0].Score <= scored[1].Score {
		t.Errorf("architecture score (%.3f) should be > setup score (%.3f)", scored[0].Score, scored[1].Score)
	}
	if scored[0].Score < 0.3 {
		t.Errorf("architecture score=%.3f, expected > 0.3 for high-affinity phase", scored[0].Score)
	}
}

func TestScoreRecordsRoleAffinity(t *testing.T) {
	testerRecord := session.PhaseRecord{ID: 13, Name: "testing", Status: "completed", Notes: []string{"tests pass"}}
	leadRecord := session.PhaseRecord{ID: 1, Name: "setup", Status: "completed", Notes: []string{"env ready"}}
	coveragePhase := Phase{ID: 14, Name: "coverage_verification", Role: RoleTester}

	scored := ScoreRecords([]session.PhaseRecord{leadRecord, testerRecord}, coveragePhase, nil, false, false)

	var testerScore, leadScore float64
	for _, s := range scored {
		if s.Record.ID == 13 {
			testerScore = s.Score
		}
		if s.Record.ID == 1 {
			leadScore = s.Score
		}
	}
	if testerScore <= leadScore {
		t.Errorf("same-role record (tester) score=%.3f should be > lead score=%.3f", testerScore, leadScore)
	}
}

func TestScoreRecordsDistanceDecay(t *testing.T) {
	near := session.PhaseRecord{ID: 7, Name: "architecture", Status: "completed"}
	far := session.PhaseRecord{ID: 1, Name: "setup", Status: "completed"}
	// Use phase 10 which has no special affinity for 7 or 1
	phase10 := Phase{ID: 10, Name: "domain_compliance", Role: RoleReviewer}

	scored := ScoreRecords([]session.PhaseRecord{far, near}, phase10, nil, false, false)

	var nearScore, farScore float64
	for _, s := range scored {
		if s.Record.ID == 7 {
			nearScore = s.Score
		}
		if s.Record.ID == 1 {
			farScore = s.Score
		}
	}
	if nearScore <= farScore {
		t.Errorf("near phase score=%.3f should be > far phase score=%.3f (distance decay)", nearScore, farScore)
	}
}

func TestScoreRecordsFileOverlap(t *testing.T) {
	withOverlap := session.PhaseRecord{ID: 8, Name: "implementation", Status: "completed", FilesChanged: []string{"main.go", "config.go"}}
	noOverlap := session.PhaseRecord{ID: 7, Name: "architecture", Status: "completed"}
	dirty := map[int][]string{8: {"main.go", "util.go"}}
	// Use phase 12 which has some affinity for 8 but not huge
	phase12 := Phase{ID: 12, Name: "test_planning", Role: RoleTestLead}

	scoredWith := ScoreRecords([]session.PhaseRecord{withOverlap}, phase12, dirty, false, false)
	scoredWithout := ScoreRecords([]session.PhaseRecord{withOverlap}, phase12, nil, false, false)

	if scoredWith[0].Score <= scoredWithout[0].Score {
		t.Errorf("file overlap score=%.3f should be > no-overlap score=%.3f", scoredWith[0].Score, scoredWithout[0].Score)
	}

	_ = noOverlap
}

func TestScoreRecordsErrorBoostL2(t *testing.T) {
	errorRecord := session.PhaseRecord{
		ID: 10, Name: "domain_compliance", Status: "failed",
		Errors: []string{"naming violation"}, Notes: []string{"tried fix"},
	}
	phase13 := Phase{ID: 13, Name: "testing", Role: RoleTester}

	scoredNoRetry := ScoreRecords([]session.PhaseRecord{errorRecord}, phase13, nil, false, false)
	scoredL2 := ScoreRecords([]session.PhaseRecord{errorRecord}, phase13, nil, true, false)

	if scoredL2[0].Score <= scoredNoRetry[0].Score {
		t.Errorf("L2 error score=%.3f should be > normal score=%.3f", scoredL2[0].Score, scoredNoRetry[0].Score)
	}
	boost := scoredL2[0].Score - scoredNoRetry[0].Score
	if boost < 0.1 {
		t.Errorf("error boost=%.3f, expected >= 0.1", boost)
	}
}

func TestAssignTiersBudgetEnforcement(t *testing.T) {
	var records []session.PhaseRecord
	for i := 1; i <= 10; i++ {
		records = append(records, session.PhaseRecord{
			ID: i, Name: "phase", Status: "completed",
			Notes: []string{"a long note with enough words to consume tokens in the budget"},
		})
	}
	phase16 := Phase{ID: 16, Name: "completion", Role: RoleLead}
	scored := ScoreRecords(records, phase16, nil, false, false)

	tinyBudget := PhaseBudget{
		MaxRecordTokens:   50,
		FullTierCutoff:    0.3,
		SummaryTierCutoff: 0.2,
		MinimalTierCutoff: 0.05,
	}
	scored = AssignTiers(scored, tinyBudget)

	fullCount := 0
	for _, s := range scored {
		if s.Tier == TierFull {
			fullCount++
		}
	}
	if fullCount >= 10 {
		t.Errorf("with tiny budget, not all 10 records should be TierFull (got %d)", fullCount)
	}
}

func TestAssignTiersCutoffLevels(t *testing.T) {
	budget := PhaseBudget{
		MaxRecordTokens:   100000,
		FullTierCutoff:    0.7,
		SummaryTierCutoff: 0.4,
		MinimalTierCutoff: 0.2,
	}

	scored := []ScoredRecord{
		{Score: 0.8, Record: session.PhaseRecord{ID: 1}},
		{Score: 0.5, Record: session.PhaseRecord{ID: 2}},
		{Score: 0.3, Record: session.PhaseRecord{ID: 3}},
		{Score: 0.1, Record: session.PhaseRecord{ID: 4}},
	}

	scored = AssignTiers(scored, budget)

	if scored[0].Tier != TierFull {
		t.Errorf("score 0.8 should be TierFull, got %d", scored[0].Tier)
	}
	if scored[1].Tier != TierSummary {
		t.Errorf("score 0.5 should be TierSummary, got %d", scored[1].Tier)
	}
	if scored[2].Tier != TierMinimal {
		t.Errorf("score 0.3 should be TierMinimal, got %d", scored[2].Tier)
	}
	if scored[3].Tier != TierOmit {
		t.Errorf("score 0.1 should be TierOmit, got %d", scored[3].Tier)
	}
}

func TestAssignTiersBudgetZero(t *testing.T) {
	budget := PhaseBudget{MaxRecordTokens: 0}
	scored := []ScoredRecord{
		{Score: 0.9, Record: session.PhaseRecord{ID: 1}},
		{Score: 0.5, Record: session.PhaseRecord{ID: 2}},
	}
	scored = AssignTiers(scored, budget)

	for _, s := range scored {
		if s.Tier != TierMinimal {
			t.Errorf("budget=0 should make all TierMinimal, got tier %d for phase %d", s.Tier, s.Record.ID)
		}
	}
}

func TestEstimateRecordTokensTiers(t *testing.T) {
	r := session.PhaseRecord{
		ID: 8, Name: "implementation", Status: "completed",
		Notes:        []string{"implemented the feature with proper error handling"},
		Errors:       []string{"build failed: missing import"},
		FilesChanged: []string{"main.go", "config.go"},
		FailReason:   "build error",
	}

	full := EstimateRecordTokens(r, TierFull)
	summary := EstimateRecordTokens(r, TierSummary)
	minimal := EstimateRecordTokens(r, TierMinimal)
	omit := EstimateRecordTokens(r, TierOmit)

	if full <= summary {
		t.Errorf("TierFull tokens (%d) should be > TierSummary (%d)", full, summary)
	}
	if summary <= minimal {
		t.Errorf("TierSummary tokens (%d) should be > TierMinimal (%d)", summary, minimal)
	}
	if omit != 0 {
		t.Errorf("TierOmit tokens should be 0, got %d", omit)
	}
}

func TestPhaseGroupOf(t *testing.T) {
	tests := []struct {
		id    int
		group PhaseGroup
	}{
		{1, GroupPlanning}, {7, GroupPlanning},
		{8, GroupImplementation},
		{9, GroupVerification}, {11, GroupVerification},
		{12, GroupTesting}, {15, GroupTesting},
		{16, GroupCompletion},
	}
	for _, tt := range tests {
		if got := PhaseGroupOf(tt.id); got != tt.group {
			t.Errorf("PhaseGroupOf(%d)=%d, want %d", tt.id, got, tt.group)
		}
	}
}

func TestResolveBudgetDefault(t *testing.T) {
	b := ResolveBudget(8, nil)
	if b.MaxRecordTokens != 20000 {
		t.Errorf("implementation budget=%d, want 20000", b.MaxRecordTokens)
	}
}

func TestResolveBudgetOverride(t *testing.T) {
	overrides := map[string]int{"implementation": 50000}
	b := ResolveBudget(8, overrides)
	if b.MaxRecordTokens != 50000 {
		t.Errorf("overridden implementation budget=%d, want 50000", b.MaxRecordTokens)
	}
}

func TestFileOverlapRatio(t *testing.T) {
	dirty := map[int][]string{8: {"main.go", "config.go", "util.go"}}
	ratio := fileOverlapRatio([]string{"main.go", "other.go"}, dirty, 10)
	if ratio != 0.5 {
		t.Errorf("overlap ratio=%.2f, want 0.50 (1 of 2 files overlap)", ratio)
	}
}

func TestFileOverlapRatioEmpty(t *testing.T) {
	if r := fileOverlapRatio([]string{"main.go"}, nil, 10); r != 0.0 {
		t.Errorf("nil dirty should return 0, got %.2f", r)
	}
	if r := fileOverlapRatio([]string{"main.go"}, map[int][]string{}, 10); r != 0.0 {
		t.Errorf("empty dirty should return 0, got %.2f", r)
	}
}

func TestRoleGroupMatch(t *testing.T) {
	if !roleGroupMatch(RoleLead, RoleReviewer) {
		t.Error("lead and reviewer should match (analysis group)")
	}
	if !roleGroupMatch(RoleDeveloper, RoleTester) {
		t.Error("developer and tester should match (execution group)")
	}
	if roleGroupMatch(RoleLead, RoleDeveloper) {
		t.Error("lead and developer should not match (different groups)")
	}
}

func TestRenderScoredRecordsTiers(t *testing.T) {
	records := []session.PhaseRecord{
		{ID: 7, Name: "architecture", Status: "completed", Notes: []string{"designed API"}, Errors: []string{"warning: unused import"}, FailReason: ""},
		{ID: 1, Name: "setup", Status: "completed", Notes: []string{"env ready"}, FilesChanged: []string{"main.go"}},
		{ID: 3, Name: "discovery", Status: "completed"},
	}
	implPhase := Phase{ID: 8, Name: "implementation", Role: RoleDeveloper}

	scored := ScoreRecords(records, implPhase, nil, false, false)
	budget := PhaseBudget{
		MaxRecordTokens:   100000,
		FullTierCutoff:    0.3,
		SummaryTierCutoff: 0.15,
		MinimalTierCutoff: 0.05,
	}
	scored = AssignTiers(scored, budget)

	var b strings.Builder
	renderScoredRecords(&b, scored)
	output := b.String()

	if !strings.Contains(output, "[PRIOR PHASE CONTEXT]") {
		t.Error("missing [PRIOR PHASE CONTEXT] header")
	}
	if !strings.Contains(output, "architecture") {
		t.Error("should contain architecture phase")
	}
}

func TestSmartCompactPreservesErrorsDuringL2(t *testing.T) {
	records := []session.PhaseRecord{
		{ID: 1, Name: "setup", Status: "completed"},
		{ID: 8, Name: "implementation", Status: "completed", Notes: []string{"wrote code"}},
		{ID: 9, Name: "design_verification", Status: "failed", Errors: []string{"interface mismatch"}},
	}
	testingPhase := Phase{ID: 13, Name: "testing", Role: RoleTester}

	compacted := SmartCompact(records, testingPhase, nil, true, false, nil)

	var foundError bool
	for _, r := range compacted {
		if r.ID == 9 && len(r.Errors) > 0 {
			foundError = true
		}
	}
	if !foundError {
		t.Error("verification phase error should be preserved during L2")
	}
}

func TestSmartCompactVsDestructiveCompact(t *testing.T) {
	records := []session.PhaseRecord{
		{ID: 7, Name: "architecture", Status: "completed", Notes: []string{"designed full API with detailed interface contracts"}, Errors: []string{"lint warning"}},
		{ID: 8, Name: "implementation", Status: "completed", Notes: []string{"implemented feature"}},
	}
	implPhase := Phase{ID: 9, Name: "design_verification", Role: RoleReviewer}

	destructive := CompactRecords(records)
	smart := SmartCompact(records, implPhase, nil, false, false, nil)

	archNoteLen := 0
	for _, r := range destructive {
		if r.ID == 7 && len(r.Notes) > 0 {
			archNoteLen = len(r.Notes[0])
		}
	}
	if archNoteLen > 103 {
		t.Errorf("CompactRecords should truncate notes, got len=%d", archNoteLen)
	}

	var smartArchHasNote bool
	for _, r := range smart {
		if r.ID == 7 && len(r.Notes) > 0 && len(r.Notes[0]) > 0 {
			smartArchHasNote = true
		}
	}
	if !smartArchHasNote {
		t.Error("SmartCompact should preserve high-relevance notes for high-affinity phase")
	}
}

func TestBuildPhasePromptLibrarianEnabled(t *testing.T) {
	records := []session.PhaseRecord{
		{ID: 7, Name: "architecture", Status: "completed", Notes: []string{"designed API"}},
		{ID: 1, Name: "setup", Status: "completed"},
	}
	implPhase := Phase{ID: 8, Name: "implementation", Role: RoleDeveloper, CompletionSignal: "implementation"}

	promptWithLib := BuildPhasePrompt("base", implPhase, "medium", 16, records, "", nil, false, false, true, nil)
	promptWithout := BuildPhasePrompt("base", implPhase, "medium", 16, records, "", nil, false, false, false, nil)

	if !strings.Contains(promptWithLib, "[PRIOR PHASE CONTEXT]") {
		t.Error("librarian prompt missing phase context")
	}
	if !strings.Contains(promptWithout, "[PRIOR PHASE CONTEXT]") {
		t.Error("non-librarian prompt missing phase context")
	}
	if promptWithLib == promptWithout {
		t.Error("librarian-enabled and disabled prompts should differ (scoring reorders records)")
	}
}

func TestBuildPhasePromptLibrarianReordersByRelevance(t *testing.T) {
	records := []session.PhaseRecord{
		{ID: 1, Name: "setup", Status: "completed", Notes: []string{"env ready"}},
		{ID: 7, Name: "architecture", Status: "completed", Notes: []string{"designed API with interfaces"}},
		{ID: 8, Name: "implementation", Status: "completed", Notes: []string{"wrote code with full impl"}},
	}
	implPhase := Phase{ID: 9, Name: "design_verification", Role: RoleReviewer, CompletionSignal: "design_verification"}

	promptLib := BuildPhasePrompt("base", implPhase, "medium", 16, records, "", nil, false, false, true, nil)
	promptFlat := BuildPhasePrompt("base", implPhase, "medium", 16, records, "", nil, false, false, false, nil)

	archIdxLib := strings.Index(promptLib, "architecture")
	setupIdxLib := strings.Index(promptLib, "setup")
	archIdxFlat := strings.Index(promptFlat, "architecture")
	setupIdxFlat := strings.Index(promptFlat, "setup")

	if archIdxLib <= 0 || setupIdxLib <= 0 {
		t.Fatal("both phases should appear in librarian prompt")
	}

	if archIdxFlat > setupIdxFlat {
		t.Skip("flat already has architecture after setup — can't verify reorder")
	}

	if archIdxLib > setupIdxLib {
		t.Error("librarian should rank architecture before setup for design_verification phase")
	}
}
