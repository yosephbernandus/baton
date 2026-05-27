package phase

func isDirtyBitSkippable(phaseID int) bool {
	return phaseID >= 9 && phaseID <= 15
}

func (p *Pipeline) shouldSkipDirtyBit(phaseID int, dirtyFiles map[int][]string, l2Cycles int) bool {
	if p.cfg.PhaseMachine.DirtyBitSkipEnabled != nil && !*p.cfg.PhaseMachine.DirtyBitSkipEnabled {
		return false
	}
	// During L2, no changes means the fix didn't take — don't skip, let verification catch it
	if l2Cycles > 0 {
		return false
	}
	if !isDirtyBitSkippable(phaseID) {
		return false
	}
	return !HasUpstreamChanges(phaseID, dirtyFiles)
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
