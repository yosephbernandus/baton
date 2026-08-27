package phase

import (
	"slices"
	"testing"
)

// Complexity skips phases, and skipped phases take their roles with them.
// Reporting a capability gap for a role that cannot run is noise at best, and at
// worst tells someone to fix something the run will never reach.
func TestTrivialRunsOnlyLeadAndDeveloper(t *testing.T) {
	roles := ActiveRoles(ComplexityTrivial)
	want := []string{"developer", "lead"}
	if !slices.Equal(roles, want) {
		t.Errorf("roles=%v, want %v — TRIVIAL runs phases 1, 8 and 16 only", roles, want)
	}
}

func TestLargerComplexitiesReachEveryRole(t *testing.T) {
	want := []string{"developer", "lead", "reviewer", "test_lead", "tester"}
	for _, c := range []string{ComplexitySmall, ComplexityMedium, ComplexityLarge} {
		if roles := ActiveRoles(c); !slices.Equal(roles, want) {
			t.Errorf("%s roles=%v, want %v", c, roles, want)
		}
	}
}

// Sorted, so a report that iterates roles reads the same across runs.
func TestActiveRolesAreSorted(t *testing.T) {
	roles := ActiveRoles(ComplexityLarge)
	if !slices.IsSorted(roles) {
		t.Errorf("roles=%v, want sorted", roles)
	}
}

// Every active role is one a phase actually declares, so a caller can look up
// its tool boundary without a missing-key case.
func TestActiveRolesComeFromActivePhases(t *testing.T) {
	for _, c := range []string{ComplexityTrivial, ComplexitySmall, ComplexityMedium, ComplexityLarge} {
		roles := ActiveRoles(c)
		for _, ph := range ActivePhases(DefaultPhases(), c) {
			if ph.Role != "" && !slices.Contains(roles, ph.Role) {
				t.Errorf("%s: phase %d declares role %q, missing from %v", c, ph.ID, ph.Role, roles)
			}
		}
	}
}

// An unrecognised complexity skips nothing, so every role is active. That is the
// safe direction — running a phase that could have been skipped costs tokens,
// while skipping one that should have run loses a check — and callers validate
// complexity before reaching here anyway. Pinned so the fail-open stays
// deliberate.
func TestUnknownComplexityRunsEveryRole(t *testing.T) {
	roles := ActiveRoles("NONSENSE")
	want := []string{"developer", "lead", "reviewer", "test_lead", "tester"}
	if !slices.Equal(roles, want) {
		t.Errorf("roles=%v, want %v: an unknown complexity must skip nothing", roles, want)
	}
}
