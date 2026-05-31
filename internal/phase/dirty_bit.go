package phase

import (
	gitpkg "github.com/yosephbernandus/baton/internal/git"
)

func isDirtyBitSkippable(phaseID int) bool {
	return phaseID >= 9 && phaseID <= 15
}

func (p *Pipeline) shouldSkipDirtyBit(phaseID int, dirtyFiles map[int][]string, l2Cycles, l3Cycles int) bool {
	if p.cfg.PhaseMachine.DirtyBitSkipEnabled != nil && !*p.cfg.PhaseMachine.DirtyBitSkipEnabled {
		return false
	}
	if l2Cycles > 0 || l3Cycles > 0 {
		return false
	}
	if !isDirtyBitSkippable(phaseID) {
		return false
	}
	if HasUpstreamChanges(phaseID, dirtyFiles) {
		return false
	}
	if checkUncommittedChanges() {
		return false
	}
	return true
}

var checkUncommittedChanges = defaultCheckUncommittedChanges

func defaultCheckUncommittedChanges() bool {
	snap, err := gitpkg.TakeSnapshot()
	if err != nil || snap == nil {
		return false
	}
	return len(snap.Modified) > 0 || len(snap.Untracked) > 0
}

// HasUpstreamChanges returns true if any phase before phaseID produced file changes.
func HasUpstreamChanges(phaseID int, dirtyFiles map[int][]string) bool {
	for pid, files := range dirtyFiles {
		if pid < phaseID && len(files) > 0 {
			return true
		}
	}
	return false
}
