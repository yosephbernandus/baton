package role

import "testing"

func TestVerifyBoundaryReviewerNoChanges(t *testing.T) {
	v := VerifyBoundary("reviewer", nil)
	if len(v) != 0 {
		t.Errorf("no changes = no violations, got %d", len(v))
	}
}

func TestVerifyBoundaryReviewerModifiedFile(t *testing.T) {
	v := VerifyBoundary("reviewer", []string{"internal/config/config.go"})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(v))
	}
	if v[0].Reason != "role is read-only but modified file" {
		t.Errorf("reason=%q", v[0].Reason)
	}
}

func TestVerifyBoundaryLeadModifiedFile(t *testing.T) {
	v := VerifyBoundary("lead", []string{"main.go"})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(v))
	}
}

func TestVerifyBoundaryTestLeadModifiedFile(t *testing.T) {
	v := VerifyBoundary("test_lead", []string{"plan.md"})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(v))
	}
}

func TestVerifyBoundaryDeveloperAllowed(t *testing.T) {
	v := VerifyBoundary("developer", []string{"main.go", "internal/phase/machine.go"})
	if len(v) != 0 {
		t.Errorf("developer can modify anything, got %d violations", len(v))
	}
}

func TestVerifyBoundaryTesterTestFile(t *testing.T) {
	v := VerifyBoundary("tester", []string{"internal/phase/machine_test.go"})
	if len(v) != 0 {
		t.Errorf("tester modifying test file = OK, got %d violations", len(v))
	}
}

func TestVerifyBoundaryTesterProductionFile(t *testing.T) {
	v := VerifyBoundary("tester", []string{"internal/phase/machine.go"})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(v))
	}
	if v[0].Reason != "tester modified non-test file" {
		t.Errorf("reason=%q", v[0].Reason)
	}
}

func TestVerifyBoundaryTesterMixed(t *testing.T) {
	v := VerifyBoundary("tester", []string{
		"internal/phase/machine_test.go",
		"internal/phase/machine.go",
		"test_helper.go",
	})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation (machine.go), got %d", len(v))
	}
	if v[0].File != "internal/phase/machine.go" {
		t.Errorf("wrong file: %s", v[0].File)
	}
}

func TestVerifyBoundaryUnknownRole(t *testing.T) {
	v := VerifyBoundary("unknown", []string{"any.go"})
	if len(v) != 0 {
		t.Errorf("unknown role should return no violations, got %d", len(v))
	}
}

func TestMatchesTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"machine_test.go", true},
		{"internal/phase/machine_test.go", true},
		{"test_helper.go", true},
		{"foo.test.js", true},
		{"foo_test.py", true},
		{"tests/integration.go", true},
		{"__tests__/app.test.tsx", true},
		{"testdata/fixture.json", true},
		{"main.go", false},
		{"internal/config/config.go", false},
		{"testing.go", false}, // file named testing.go is not a test file
	}
	for _, tt := range tests {
		got := matchesTestFile(tt.path)
		if got != tt.want {
			t.Errorf("matchesTestFile(%q)=%v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBoundaryText(t *testing.T) {
	text := BoundaryText("reviewer")
	if text == "" {
		t.Error("reviewer boundary text should not be empty")
	}
	if BoundaryText("unknown") != "" {
		t.Error("unknown role should return empty")
	}
}

func TestAllRolesHaveBoundary(t *testing.T) {
	for name, r := range Roles {
		if r.Boundary == "" {
			t.Errorf("role %q has empty boundary text", name)
		}
	}
}
