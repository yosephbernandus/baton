package knowledge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type VerifyRule struct {
	Pattern string // grep pattern to verify
	Expect  string // "exists" or "absent"
	Scope   string // file or directory to search in
}

type VerificationResult struct {
	Claim    SoftClaim
	Passed   bool
	Evidence string
}

func VerifyClaim(projectDir string, claim SoftClaim, rule VerifyRule) VerificationResult {
	scope := filepath.Join(projectDir, rule.Scope)

	var output []byte
	var err error
	info, statErr := os.Stat(scope)
	if statErr != nil {
		return VerificationResult{
			Claim:    claim,
			Passed:   false,
			Evidence: fmt.Sprintf("scope not found: %s", rule.Scope),
		}
	}

	if info.IsDir() {
		output, err = exec.Command("grep", "-r", "--include=*.go", "-l", rule.Pattern, scope).Output()
	} else {
		output, err = exec.Command("grep", "-l", rule.Pattern, scope).Output()
	}

	found := err == nil && len(strings.TrimSpace(string(output))) > 0

	var passed bool
	var evidence string

	switch rule.Expect {
	case "exists":
		passed = found
		if found {
			files := strings.TrimSpace(string(output))
			evidence = fmt.Sprintf("pattern %q found in: %s", rule.Pattern, firstN(files, 3))
		} else {
			evidence = fmt.Sprintf("pattern %q not found in %s", rule.Pattern, rule.Scope)
		}
	case "absent":
		passed = !found
		if !found {
			evidence = fmt.Sprintf("pattern %q absent from %s (confirmed)", rule.Pattern, rule.Scope)
		} else {
			files := strings.TrimSpace(string(output))
			evidence = fmt.Sprintf("pattern %q found in: %s (claim rejected)", rule.Pattern, firstN(files, 3))
		}
	default:
		passed = false
		evidence = fmt.Sprintf("unknown expect value: %s", rule.Expect)
	}

	claim.Verified = passed
	claim.Verification = evidence
	if passed {
		claim.Confidence = "verified"
		claim.VerifiedAt = time.Now().UTC().Format("2006-01-02")
	} else {
		claim.Confidence = "rejected"
	}

	return VerificationResult{
		Claim:    claim,
		Passed:   passed,
		Evidence: evidence,
	}
}

func firstN(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf(" (+%d more)", len(lines)-n)
}
