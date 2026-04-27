package git

import (
	"os/exec"
	"strings"
)

type Snapshot struct {
	Modified  []string
	Untracked []string
}

func IsRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func TakeSnapshot() (*Snapshot, error) {
	if !IsRepo() {
		return nil, nil
	}

	modified, err := diffNameOnly()
	if err != nil {
		return nil, err
	}

	untracked, err := untrackedFiles()
	if err != nil {
		return nil, err
	}

	return &Snapshot{Modified: modified, Untracked: untracked}, nil
}

func DetectChanges(before, after *Snapshot) []string {
	if before == nil || after == nil {
		return nil
	}

	beforeSet := make(map[string]bool)
	for _, f := range before.Modified {
		beforeSet[f] = true
	}
	for _, f := range before.Untracked {
		beforeSet[f] = true
	}

	var changed []string
	seen := make(map[string]bool)

	for _, f := range after.Modified {
		if !beforeSet[f] && !seen[f] {
			changed = append(changed, f)
			seen[f] = true
		}
	}
	for _, f := range after.Untracked {
		if !beforeSet[f] && !seen[f] {
			changed = append(changed, f)
			seen[f] = true
		}
	}

	return changed
}

func diffNameOnly() ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitLines(string(out)), nil
}

func untrackedFiles() ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitLines(string(out)), nil
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
