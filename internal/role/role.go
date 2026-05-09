package role

import (
	"path/filepath"
	"strings"
)

type Role struct {
	Name       string
	AllowedOps []string
	DeniedOps  []string
	FileScope  string // glob pattern for allowed file paths (empty = all files)
	Boundary   string // enforcement text injected into prompt
}

var Roles = map[string]Role{
	"lead": {
		Name:       "lead",
		AllowedOps: []string{"Read", "Grep", "Glob", "Bash(verify-only)"},
		DeniedOps:  []string{"Edit", "Write", "Delete"},
		Boundary: "You MUST NOT edit, write, create, or delete any files.\n" +
			"You MUST NOT run commands that modify state (build artifacts, install packages, etc.).\n" +
			"You may read files, search code, and run verification commands (go vet, grep, etc.).",
	},
	"developer": {
		Name:       "developer",
		AllowedOps: []string{"Read", "Edit", "Write", "Bash"},
		Boundary:   "You have full access to read, write, and execute. Focus on correctness and quality.",
	},
	"reviewer": {
		Name:       "reviewer",
		AllowedOps: []string{"Read", "Grep", "Glob", "Bash(verify-only)"},
		DeniedOps:  []string{"Edit", "Write", "Delete"},
		Boundary: "You MUST NOT edit, write, create, or delete any files.\n" +
			"You MUST NOT run commands that modify state.\n" +
			"Analyze the code and report findings only.",
	},
	"test_lead": {
		Name:       "test_lead",
		AllowedOps: []string{"Read", "Grep", "Glob"},
		DeniedOps:  []string{"Edit", "Write", "Bash"},
		Boundary: "You MUST NOT edit, write, or create any files.\n" +
			"You MUST NOT run any commands.\n" +
			"Plan the test strategy and identify what needs testing.",
	},
	"tester": {
		Name:       "tester",
		AllowedOps: []string{"Read", "Edit", "Write", "Bash(test-only)"},
		DeniedOps:  []string{"Edit(production)", "Write(production)"},
		FileScope:  "*_test.go",
		Boundary: "You may only edit and create test files (files ending in _test.go, test_*, *_test.*).\n" +
			"You MUST NOT modify production code.\n" +
			"You may run test commands (go test, pytest, npm test, etc.).",
	},
}

type Violation struct {
	Role         string
	File         string
	Reason       string
}

func VerifyBoundary(roleName string, filesChanged []string) []Violation {
	r, ok := Roles[roleName]
	if !ok {
		return nil
	}

	var violations []Violation

	// Roles with no write access should not change any files
	if containsAny(r.DeniedOps, "Edit", "Write") {
		for _, f := range filesChanged {
			violations = append(violations, Violation{
				Role:   roleName,
				File:   f,
				Reason: "role is read-only but modified file",
			})
		}
		return violations
	}

	// Tester: check file scope
	if r.FileScope != "" {
		for _, f := range filesChanged {
			if !matchesTestFile(f) {
				violations = append(violations, Violation{
					Role:   roleName,
					File:   f,
					Reason: "tester modified non-test file",
				})
			}
		}
	}

	return violations
}

func matchesTestFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.HasPrefix(base, "test_") {
		return true
	}
	if strings.Contains(base, "_test.") {
		return true
	}
	if strings.Contains(base, ".test.") {
		return true
	}
	dir := filepath.Dir(path)
	parts := strings.Split(dir, string(filepath.Separator))
	for _, p := range parts {
		if p == "test" || p == "tests" || p == "testdata" || p == "__tests__" {
			return true
		}
	}
	return false
}

func containsAny(ops []string, targets ...string) bool {
	for _, op := range ops {
		for _, t := range targets {
			if op == t {
				return true
			}
		}
	}
	return false
}

func AllowedTools(roleName string) []string {
	switch roleName {
	case "lead":
		return []string{"Read", "Grep", "Glob", "Bash"}
	case "developer":
		return nil // no restriction
	case "reviewer":
		return []string{"Read", "Grep", "Glob", "Bash"}
	case "test_lead":
		return []string{"Read", "Grep", "Glob"}
	case "tester":
		return []string{"Read", "Edit", "Write", "Bash"}
	default:
		return nil
	}
}

func BoundaryText(roleName string) string {
	r, ok := Roles[roleName]
	if !ok {
		return ""
	}
	return r.Boundary
}
