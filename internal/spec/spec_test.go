package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "test.yaml")

	content := []byte(`
spec:
  what: "Implement migration"
  why: "Needed for auto-expiry"
  constraints:
    - "Use v2 schema"
    - "Do NOT touch routes/"
  context_files:
    - go.mod
  acceptance_criteria:
    - "go build passes"
  criticality: medium
`)
	if err := os.WriteFile(specPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}

	if s.What != "Implement migration" {
		t.Errorf("expected 'Implement migration', got %q", s.What)
	}
	if s.Why != "Needed for auto-expiry" {
		t.Errorf("expected 'Needed for auto-expiry', got %q", s.Why)
	}
	if len(s.Constraints) != 2 {
		t.Errorf("expected 2 constraints, got %d", len(s.Constraints))
	}
	if s.Criticality != "medium" {
		t.Errorf("expected criticality medium, got %s", s.Criticality)
	}
}

func TestValidate_Valid(t *testing.T) {
	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "test.go")
	os.WriteFile(ctxFile, []byte("package main"), 0o644)

	s := &Spec{
		What:               "Do something",
		Why:                "Because reasons",
		Constraints:        []string{},
		ContextFiles:       []string{ctxFile},
		AcceptanceCriteria: []string{"it works"},
	}

	errs := Validate(s)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_MissingFields(t *testing.T) {
	s := &Spec{}
	errs := Validate(s)

	fields := map[string]bool{}
	for _, e := range errs {
		fields[e.Field] = true
	}

	for _, required := range []string{"what", "why", "constraints", "context_files", "acceptance_criteria"} {
		if !fields[required] {
			t.Errorf("expected validation error for %q", required)
		}
	}
}

func TestValidate_BadCriticality(t *testing.T) {
	s := &Spec{
		What:               "x",
		Why:                "y",
		Constraints:        []string{},
		ContextFiles:       []string{"go.mod"},
		AcceptanceCriteria: []string{"z"},
		Criticality:        "extreme",
	}

	errs := Validate(s)
	found := false
	for _, e := range errs {
		if e.Field == "criticality" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for invalid criticality")
	}
}

func TestValidate_ContextFileNotExist(t *testing.T) {
	s := &Spec{
		What:               "x",
		Why:                "y",
		Constraints:        []string{},
		ContextFiles:       []string{"/nonexistent/file.go"},
		AcceptanceCriteria: []string{"z"},
	}

	errs := Validate(s)
	found := false
	for _, e := range errs {
		if e.Field == "context_files" && strings.Contains(e.Message, "not found") {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for missing context file")
	}
}

func TestBuildPrompt(t *testing.T) {
	s := &Spec{
		What:               "Build feature X",
		Why:                "Users need it",
		Constraints:        []string{"Don't touch auth"},
		AcceptanceCriteria: []string{"Tests pass"},
		Decisions: []Decision{
			{Question: "v1 or v2?", Answer: "v2", Reason: "TTL needed", DecidedBy: "human"},
		},
		Examples: []Example{
			{Description: "Pattern to follow", Code: "func Foo() {}"},
		},
	}

	prompt := BuildPrompt(s, "Project: TestProject\nLanguage: Go")

	checks := []string{
		"[PROJECT CONTEXT]",
		"Project: TestProject",
		"[TASK]",
		"Build feature X",
		"[WHY THIS MATTERS]",
		"Users need it",
		"[CONSTRAINTS]",
		"Don't touch auth",
		"[ACCEPTANCE CRITERIA]",
		"Tests pass",
		"[DECISIONS ALREADY MADE]",
		"v1 or v2?",
		"v2",
		"[EXAMPLES]",
		"func Foo() {}",
		"CLARIFICATION_NEEDED",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

func TestBuildPrompt_NoBrief(t *testing.T) {
	s := &Spec{
		What:               "Do thing",
		Why:                "Reason",
		Constraints:        []string{},
		AcceptanceCriteria: []string{"Done"},
	}

	prompt := BuildPrompt(s, "")
	if strings.Contains(prompt, "[PROJECT CONTEXT]") {
		t.Error("should not include PROJECT CONTEXT when brief is empty")
	}
}
