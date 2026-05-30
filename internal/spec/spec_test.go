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
	_ = os.WriteFile(ctxFile, []byte("package main"), 0o644)

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

func TestInferComplexity(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want string
	}{
		{"nil spec", nil, ""},
		{"empty spec", &Spec{}, "TRIVIAL"},
		{"single file", &Spec{
			WritesTo:     []string{"main.go"},
			ContextFiles: []string{"main.go"},
		}, "TRIVIAL"},
		{"small task", &Spec{
			WritesTo:           []string{"a.go", "b.go", "c.go"},
			ContextFiles:       []string{"pkg/a.go", "cmd/b.go"},
			AcceptanceCriteria: []string{"compiles", "tests pass"},
		}, "SMALL"},
		{"medium task", &Spec{
			WritesTo:           []string{"a.go", "b.go", "c.go", "d.go"},
			ContextFiles:       []string{"x/a.go", "y/b.go", "z/c.go"},
			AcceptanceCriteria: []string{"1", "2", "3"},
			AcceptanceChecks:   []Check{{Command: "go test"}},
		}, "MEDIUM"},
		{"large task", &Spec{
			WritesTo:           []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			ContextFiles:       []string{"w/1", "x/2", "y/3", "z/4"},
			AcceptanceCriteria: []string{"1", "2", "3", "4", "5", "6"},
			AcceptanceChecks:   []Check{{Command: "t1"}, {Command: "t2"}, {Command: "t3"}},
		}, "LARGE"},
		{"domain boost", &Spec{
			WritesTo:           []string{"a.go", "b.go", "c.go"},
			ContextFiles:       []string{"x/a.go", "y/b.go"},
			AcceptanceCriteria: []string{"1", "2", "3"},
			Domain:             "security",
		}, "MEDIUM"},
		{"many constraints boost", &Spec{
			WritesTo:     []string{"a.go", "b.go"},
			ContextFiles: []string{"x/a.go", "y/b.go"},
			Constraints:  []string{"c1", "c2", "c3", "c4", "c5"},
		}, "SMALL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferComplexity(tt.spec)
			if got != tt.want {
				t.Errorf("InferComplexity() = %q, want %q", got, tt.want)
			}
		})
	}
}
