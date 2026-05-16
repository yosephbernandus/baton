package phase

import (
	"fmt"
	"math"
	"testing"
)

func TestSimilarityIdentical(t *testing.T) {
	a := []string{"line1", "line2", "line3"}
	if s := Similarity(a, a); s != 1.0 {
		t.Errorf("identical: got %f, want 1.0", s)
	}
}

func TestSimilarityDisjoint(t *testing.T) {
	a := []string{"a", "b", "c"}
	b := []string{"x", "y", "z"}
	if s := Similarity(a, b); s != 0.0 {
		t.Errorf("disjoint: got %f, want 0.0", s)
	}
}

func TestSimilarityPartial(t *testing.T) {
	a := []string{"a", "b", "c", "d"}
	b := []string{"a", "b", "x", "y"}
	// intersection=2 (a,b), union=6 (a,b,c,d,x,y) → 2/6 ≈ 0.333
	s := Similarity(a, b)
	if math.Abs(s-1.0/3.0) > 0.01 {
		t.Errorf("partial: got %f, want ~0.333", s)
	}
}

func TestSimilarityEmpty(t *testing.T) {
	if s := Similarity(nil, nil); s != 1.0 {
		t.Errorf("both empty: got %f, want 1.0", s)
	}
	if s := Similarity([]string{"a"}, nil); s != 0.0 {
		t.Errorf("one empty: got %f, want 0.0", s)
	}
}

func TestSimilarityDuplicateLines(t *testing.T) {
	a := []string{"a", "a", "b"}
	b := []string{"a", "b", "b"}
	// Sets: {a,b} vs {a,b} → 1.0
	if s := Similarity(a, b); s != 1.0 {
		t.Errorf("duplicates: got %f, want 1.0", s)
	}
}

func TestLoopDetectorNotStuckTooFew(t *testing.T) {
	ld := NewLoopDetector(3, 0.9, 50)
	ld.Record([]string{"error: build failed"})
	ld.Record([]string{"error: build failed"})
	if ld.IsStuck() {
		t.Error("should not be stuck with only 2 records (window=3)")
	}
}

func TestLoopDetectorStuck(t *testing.T) {
	ld := NewLoopDetector(3, 0.9, 50)
	output := []string{
		"starting build",
		"compiling main.go",
		"error: undefined reference to foo",
		"build failed",
	}
	ld.Record(output)
	ld.Record(output)
	ld.Record(output)
	if !ld.IsStuck() {
		t.Error("should be stuck with 3 identical outputs")
	}
}

func TestLoopDetectorNotStuckDifferent(t *testing.T) {
	ld := NewLoopDetector(3, 0.9, 50)
	for i := 0; i < 3; i++ {
		ld.Record([]string{
			fmt.Sprintf("attempt %d", i),
			fmt.Sprintf("trying approach %d", i),
			"some common line",
		})
	}
	if ld.IsStuck() {
		t.Error("should not be stuck with different outputs")
	}
}

func TestLoopDetectorWindow(t *testing.T) {
	ld := NewLoopDetector(3, 0.9, 50)
	// First 2 are different
	ld.Record([]string{"different output 1"})
	ld.Record([]string{"different output 2"})
	// Next 3 are identical
	same := []string{"error: same thing"}
	ld.Record(same)
	ld.Record(same)
	ld.Record(same)
	if !ld.IsStuck() {
		t.Error("should be stuck — last 3 in window are identical")
	}
}

func TestLoopDetectorTailTruncation(t *testing.T) {
	ld := NewLoopDetector(3, 0.9, 5)
	// 10 lines, but tailLines=5 so only last 5 compared
	outputA := []string{"1", "2", "3", "4", "5", "a", "b", "c", "d", "e"}
	outputB := []string{"x", "y", "z", "w", "v", "a", "b", "c", "d", "e"}
	ld.Record(outputA)
	ld.Record(outputB)
	ld.Record(outputA)
	// Last 5 lines are identical across all 3 → stuck
	if !ld.IsStuck() {
		t.Error("should be stuck — tail 5 lines are identical")
	}
}

func TestLoopDetectorThreshold(t *testing.T) {
	ld := NewLoopDetector(3, 0.5, 50)
	// With 0.5 threshold, partially similar outputs should trigger
	base := []string{"a", "b", "c", "d"}
	ld.Record(base)
	ld.Record([]string{"a", "b", "c", "x"}) // 3/5 = 0.6
	ld.Record([]string{"a", "b", "c", "y"}) // similar
	if !ld.IsStuck() {
		t.Error("should be stuck at 0.5 threshold with >60% similarity")
	}
}

func TestNewLoopDetectorDefaults(t *testing.T) {
	ld := NewLoopDetector(0, 0, 0)
	if ld.window != 3 {
		t.Errorf("window=%d, want 3", ld.window)
	}
	if ld.threshold != 0.9 {
		t.Errorf("threshold=%f, want 0.9", ld.threshold)
	}
	if ld.tailLines != 50 {
		t.Errorf("tailLines=%d, want 50", ld.tailLines)
	}
}
