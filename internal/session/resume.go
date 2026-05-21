package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"syscall"

	gitpkg "github.com/yosephbernandus/baton/internal/git"
	"github.com/yosephbernandus/baton/internal/spec"
)

type ResumeDecision struct {
	Action     string // "resume", "fresh", "error"
	Reason     string
	StartPhase int
}

const maxPhaseResumeAttempts = 3

func SpecCoreHash(s *spec.Spec) string {
	h := sha256.New()
	h.Write([]byte(s.What))
	h.Write([]byte(s.Why))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func CheckResumable(sessionPath string, s *spec.Spec) (*ResumeDecision, *Manifest) {
	m, err := Load(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ResumeDecision{Action: "fresh", Reason: "no existing session"}, nil
		}
		return &ResumeDecision{Action: "fresh", Reason: fmt.Sprintf("session load error: %v", err)}, nil
	}

	if !m.IsResumable() {
		return &ResumeDecision{Action: "fresh", Reason: fmt.Sprintf("previous session %s", m.Status)}, m
	}

	if m.WorkerPID > 0 {
		if err := syscall.Kill(m.WorkerPID, 0); err == nil {
			return &ResumeDecision{Action: "error", Reason: fmt.Sprintf("pipeline still running (PID %d)", m.WorkerPID)}, m
		}
	}

	currentHash := SpecCoreHash(s)
	if m.SpecCoreHash != "" && m.SpecCoreHash != currentHash {
		return &ResumeDecision{Action: "fresh", Reason: "task changed (what/why modified)"}, m
	}

	if m.GitHead != "" {
		currentHead, err := gitpkg.HeadHash()
		if err == nil && currentHead != m.GitHead {
			externalFiles, err := gitpkg.DiffBetween(m.GitHead, currentHead)
			if err == nil && len(externalFiles) > 0 {
				ownFiles := make(map[string]bool)
				for _, f := range m.PipelineFiles {
					ownFiles[f] = true
				}

				specFiles := make(map[string]bool)
				for _, f := range s.ContextFiles {
					specFiles[f] = true
				}
				for _, f := range s.WritesTo {
					specFiles[f] = true
				}

				for _, f := range externalFiles {
					if ownFiles[f] {
						return &ResumeDecision{Action: "fresh", Reason: fmt.Sprintf("pipeline file %s overwritten externally", f)}, m
					}
				}

				for _, f := range externalFiles {
					if specFiles[f] {
						return &ResumeDecision{Action: "fresh", Reason: fmt.Sprintf("conflict detected: %s changed externally", f)}, m
					}
				}
			}
		}
	}

	lastCompleted := m.LastCompletedPhase()
	nextPhaseID := m.Pipeline.CurrentPhase
	if containsInt(m.Pipeline.PhasesCompleted, nextPhaseID) {
		nextPhaseID = lastCompleted + 1
	}
	if m.PhaseResumeAttempts != nil {
		if m.PhaseResumeAttempts[nextPhaseID] >= maxPhaseResumeAttempts {
			return &ResumeDecision{
				Action: "fresh",
				Reason: fmt.Sprintf("phase %d stuck across %d resumes", nextPhaseID, m.PhaseResumeAttempts[nextPhaseID]),
			}, m
		}
	}

	return &ResumeDecision{
		Action:     "resume",
		Reason:     fmt.Sprintf("resuming from phase %d (resume #%d)", lastCompleted, m.ResumeCount+1),
		StartPhase: lastCompleted,
	}, m
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}
