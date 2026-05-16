package spec

import (
	"strings"
	"testing"
)

func TestGenerateTemplateHasAllFields(t *testing.T) {
	out := GenerateTemplate("add JWT authentication")

	required := []string{
		"spec:",
		"what:",
		"add JWT authentication",
		"why:",
		"constraints:",
		"context_files:",
		"acceptance_criteria:",
	}

	for _, r := range required {
		if !strings.Contains(out, r) {
			t.Errorf("missing %q in output:\n%s", r, out)
		}
	}
}

func TestGenerateFromInputsFilledIn(t *testing.T) {
	out := GenerateFromInputs(
		"add JWT auth",
		"API has no authentication",
		[]string{"don't modify routes", "use stdlib"},
		[]string{"internal/api/router.go"},
		[]string{"protected routes require JWT"},
		TemplateOpts{Complexity: "MEDIUM", Criticality: "high"},
	)

	checks := []string{
		"add JWT auth",
		"API has no authentication",
		"don't modify routes",
		"use stdlib",
		"internal/api/router.go",
		"protected routes require JWT",
		"estimated_complexity: MEDIUM",
		"criticality: high",
	}

	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("missing %q in output:\n%s", c, out)
		}
	}

	if strings.Contains(out, "TODO") {
		t.Error("fully filled spec should not contain TODO")
	}
}

func TestGenerateTemplateHasTODOs(t *testing.T) {
	out := GenerateTemplate("something")
	count := strings.Count(out, "TODO")
	if count < 3 {
		t.Errorf("expected at least 3 TODO placeholders, got %d", count)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"add JWT auth", "add-jwt-auth"},
		{"Fix bug #123", "fix-bug-123"},
		{"  spaces  everywhere  ", "spaces-everywhere"},
		{"UPPER CASE", "upper-case"},
		{"special!@#chars$%^&*()", "special-chars"},
		{"", ""},
	}

	for _, tt := range tests {
		got := Slugify(tt.input)
		if got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSlugifyTruncatesLong(t *testing.T) {
	long := strings.Repeat("a very long description ", 10)
	slug := Slugify(long)
	if len(slug) > 60 {
		t.Errorf("slug too long: %d chars", len(slug))
	}
}
