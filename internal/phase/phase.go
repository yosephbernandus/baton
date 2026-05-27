package phase

const (
	ComplexityTrivial = "TRIVIAL"
	ComplexitySmall   = "SMALL"
	ComplexityMedium  = "MEDIUM"
	ComplexityLarge   = "LARGE"
)

const (
	RoleLead      = "lead"
	RoleDeveloper = "developer"
	RoleReviewer  = "reviewer"
	RoleTestLead  = "test_lead"
	RoleTester    = "tester"
)

type Phase struct {
	ID               int
	Name             string
	Role             string
	CompletionSignal string
	SkipFor          []string
}

func DefaultPhases() []Phase {
	return []Phase{
		{1, "setup", RoleLead, "setup", nil},
		{2, "triage", RoleLead, "triage", []string{ComplexityTrivial}},
		{3, "discovery", RoleLead, "discovery", []string{ComplexityTrivial}},
		{4, "skill_discovery", RoleLead, "skill_discovery", []string{ComplexityTrivial}},
		{5, "complexity", RoleLead, "complexity", []string{ComplexityTrivial, ComplexitySmall, ComplexityMedium}},
		{6, "brainstorming", RoleLead, "brainstorming", []string{ComplexityTrivial, ComplexitySmall}},
		{7, "architecture", RoleLead, "architecture", []string{ComplexityTrivial, ComplexitySmall}},
		{8, "implementation", RoleDeveloper, "implementation", nil},
		{9, "design_verification", RoleReviewer, "design_verification", []string{ComplexityTrivial, ComplexitySmall}},
		{10, "domain_compliance", RoleReviewer, "domain_compliance", []string{ComplexityTrivial}},
		{11, "code_quality", RoleReviewer, "code_quality", []string{ComplexityTrivial, ComplexitySmall}},
		{12, "test_planning", RoleTestLead, "test_planning", []string{ComplexityTrivial}},
		{13, "testing", RoleTester, "testing", []string{ComplexityTrivial}},
		{14, "coverage_verification", RoleTester, "coverage_verification", []string{ComplexityTrivial}},
		{15, "test_quality", RoleReviewer, "test_quality", []string{ComplexityTrivial}},
		{16, "completion", RoleLead, "completion", nil},
	}
}

func ActivePhases(phases []Phase, complexity string) []Phase {
	var active []Phase
	for _, p := range phases {
		if shouldSkip(p, complexity) {
			continue
		}
		active = append(active, p)
	}
	return active
}

func SkippedPhaseIDs(phases []Phase, complexity string) []int {
	var skipped []int
	for _, p := range phases {
		if shouldSkip(p, complexity) {
			skipped = append(skipped, p.ID)
		}
	}
	return skipped
}

func shouldSkip(p Phase, complexity string) bool {
	for _, c := range p.SkipFor {
		if c == complexity {
			return true
		}
	}
	return false
}

const (
	L2StartPhase = 8  // implementation
	L2EndPhase   = 15 // test_quality (inclusive)
	L3StartPhase = 6  // brainstorming — rethink approach
)

func IsVerificationPhase(phaseID int) bool {
	return (phaseID >= 9 && phaseID <= 11) || (phaseID >= 13 && phaseID <= 15)
}

func ValidComplexity(c string) bool {
	switch c {
	case ComplexityTrivial, ComplexitySmall, ComplexityMedium, ComplexityLarge:
		return true
	}
	return false
}
