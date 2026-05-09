package phase

import (
	"testing"
)

func TestDefaultPhases(t *testing.T) {
	phases := DefaultPhases()
	if len(phases) != 16 {
		t.Fatalf("expected 16 phases, got %d", len(phases))
	}
	if phases[0].ID != 1 || phases[0].Name != "setup" {
		t.Errorf("first phase: got %d:%s, want 1:setup", phases[0].ID, phases[0].Name)
	}
	if phases[15].ID != 16 || phases[15].Name != "completion" {
		t.Errorf("last phase: got %d:%s, want 16:completion", phases[15].ID, phases[15].Name)
	}
}

func TestActivePhasesTrivial(t *testing.T) {
	active := ActivePhases(DefaultPhases(), ComplexityTrivial)
	ids := phaseIDs(active)
	expected := []int{1, 8, 16}
	if !equalInts(ids, expected) {
		t.Errorf("TRIVIAL active phases: got %v, want %v", ids, expected)
	}
}

func TestActivePhasesSmall(t *testing.T) {
	active := ActivePhases(DefaultPhases(), ComplexitySmall)
	ids := phaseIDs(active)
	expected := []int{1, 2, 3, 4, 8, 10, 12, 13, 14, 15, 16}
	if !equalInts(ids, expected) {
		t.Errorf("SMALL active phases: got %v, want %v", ids, expected)
	}
}

func TestActivePhasesMedium(t *testing.T) {
	active := ActivePhases(DefaultPhases(), ComplexityMedium)
	ids := phaseIDs(active)
	// MEDIUM skips only phase 5 (complexity)
	expected := []int{1, 2, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	if !equalInts(ids, expected) {
		t.Errorf("MEDIUM active phases: got %v, want %v", ids, expected)
	}
}

func TestActivePhasesLarge(t *testing.T) {
	active := ActivePhases(DefaultPhases(), ComplexityLarge)
	if len(active) != 16 {
		t.Errorf("LARGE: expected 16 active phases, got %d", len(active))
	}
}

func TestSkippedPhaseIDs(t *testing.T) {
	skipped := SkippedPhaseIDs(DefaultPhases(), ComplexityTrivial)
	expected := []int{2, 3, 4, 5, 6, 7, 9, 10, 11, 12, 13, 14, 15}
	if !equalInts(skipped, expected) {
		t.Errorf("TRIVIAL skipped: got %v, want %v", skipped, expected)
	}
}

func TestValidComplexity(t *testing.T) {
	for _, c := range []string{"TRIVIAL", "SMALL", "MEDIUM", "LARGE"} {
		if !ValidComplexity(c) {
			t.Errorf("expected %q to be valid", c)
		}
	}
	for _, c := range []string{"trivial", "huge", "", "XL"} {
		if ValidComplexity(c) {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func phaseIDs(phases []Phase) []int {
	ids := make([]int, len(phases))
	for i, p := range phases {
		ids[i] = p.ID
	}
	return ids
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
