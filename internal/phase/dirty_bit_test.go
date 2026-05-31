package phase

import (
	"testing"
)

func TestIsDirtyBitSkippable(t *testing.T) {
	for _, id := range []int{9, 10, 11, 12, 13, 14, 15} {
		if !isDirtyBitSkippable(id) {
			t.Errorf("phase %d should be dirty-bit skippable", id)
		}
	}
	for _, id := range []int{1, 2, 3, 4, 5, 6, 7, 8, 16} {
		if isDirtyBitSkippable(id) {
			t.Errorf("phase %d should NOT be dirty-bit skippable", id)
		}
	}
}

func TestHasUpstreamChanges(t *testing.T) {
	dirty := map[int][]string{
		8: {"main.go", "config.go"},
	}
	if !HasUpstreamChanges(9, dirty) {
		t.Error("phase 9 should see upstream changes from phase 8")
	}
	if HasUpstreamChanges(8, dirty) {
		t.Error("phase 8 should not see its own changes as upstream")
	}
	if HasUpstreamChanges(7, dirty) {
		t.Error("phase 7 should not see phase 8 as upstream")
	}
}

func TestHasUpstreamChangesEmpty(t *testing.T) {
	if HasUpstreamChanges(9, map[int][]string{}) {
		t.Error("no upstream changes when dirty map is empty")
	}
	if HasUpstreamChanges(9, nil) {
		t.Error("no upstream changes when dirty map is nil")
	}
}

func TestHasUpstreamChangesEmptyFiles(t *testing.T) {
	dirty := map[int][]string{
		8: {},
	}
	if HasUpstreamChanges(9, dirty) {
		t.Error("empty file list should not count as upstream changes")
	}
}

func init() {
	checkUncommittedChanges = func() bool { return false }
}

func TestShouldSkipDirtyBitDisabled(t *testing.T) {
	disabled := false
	cfg := testConfig(0)
	cfg.PhaseMachine.DirtyBitSkipEnabled = &disabled
	p := NewPipeline(cfg, &mockRunner{}, nil, nil, testSpec(), "test", PipelineConfig{})

	if p.shouldSkipDirtyBit(9, map[int][]string{}, 0, 0) {
		t.Error("should not skip when disabled")
	}
}

func TestShouldSkipDirtyBitDuringL2(t *testing.T) {
	cfg := testConfig(0)
	p := NewPipeline(cfg, &mockRunner{}, nil, nil, testSpec(), "test", PipelineConfig{})

	if p.shouldSkipDirtyBit(9, map[int][]string{}, 1, 0) {
		t.Error("should not skip during L2 cycles")
	}
}

func TestShouldSkipDirtyBitDuringL3(t *testing.T) {
	cfg := testConfig(0)
	p := NewPipeline(cfg, &mockRunner{}, nil, nil, testSpec(), "test", PipelineConfig{})

	if p.shouldSkipDirtyBit(9, map[int][]string{}, 0, 1) {
		t.Error("should not skip during L3 cycles")
	}
}

func TestShouldSkipDirtyBitNoChanges(t *testing.T) {
	cfg := testConfig(0)
	p := NewPipeline(cfg, &mockRunner{}, nil, nil, testSpec(), "test", PipelineConfig{})

	if !p.shouldSkipDirtyBit(9, map[int][]string{}, 0, 0) {
		t.Error("should skip verification when no upstream changes")
	}
	if !p.shouldSkipDirtyBit(12, map[int][]string{}, 0, 0) {
		t.Error("should skip test_planning when no upstream changes")
	}
	if !p.shouldSkipDirtyBit(15, map[int][]string{}, 0, 0) {
		t.Error("should skip test_quality when no upstream changes")
	}
}

func TestShouldSkipDirtyBitWithChanges(t *testing.T) {
	cfg := testConfig(0)
	p := NewPipeline(cfg, &mockRunner{}, nil, nil, testSpec(), "test", PipelineConfig{})

	dirty := map[int][]string{8: {"main.go"}}
	if p.shouldSkipDirtyBit(9, dirty, 0, 0) {
		t.Error("should not skip when upstream has changes")
	}
}

func TestShouldSkipDirtyBitNonSkippablePhase(t *testing.T) {
	cfg := testConfig(0)
	p := NewPipeline(cfg, &mockRunner{}, nil, nil, testSpec(), "test", PipelineConfig{})

	if p.shouldSkipDirtyBit(8, map[int][]string{}, 0, 0) {
		t.Error("implementation phase should never be dirty-bit skipped")
	}
	if p.shouldSkipDirtyBit(16, map[int][]string{}, 0, 0) {
		t.Error("completion phase should never be dirty-bit skipped")
	}
}
