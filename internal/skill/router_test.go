package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContextEmptyDomain(t *testing.T) {
	r := NewRouter("", nil)
	got, err := r.LoadContext("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestLoadContextMissingDir(t *testing.T) {
	r := NewRouter("/nonexistent/skills", nil)
	got, err := r.LoadContext("backend")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty for missing dir, got %q", got)
	}
}

func TestLoadContextReadsFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "backend")
	_ = os.MkdirAll(skillDir, 0o755)

	_ = os.WriteFile(filepath.Join(skillDir, "conventions.md"), []byte("Use Go error wrapping"), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, "patterns.txt"), []byte("Repository pattern preferred"), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, "ignored.json"), []byte(`{"not":"loaded"}`), 0o644)

	r := NewRouter(dir, nil)
	got, err := r.LoadContext("backend")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Use Go error wrapping") {
		t.Error("missing conventions.md content")
	}
	if !strings.Contains(got, "Repository pattern preferred") {
		t.Error("missing patterns.txt content")
	}
	if strings.Contains(got, "not") {
		t.Error("should not load .json files")
	}
}

func TestLoadContextWithDomainMap(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "backend")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "api.md"), []byte("REST conventions"), 0o644)

	r := NewRouter(dir, map[string]string{"go": "backend"})
	got, err := r.LoadContext("go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "REST conventions") {
		t.Error("domain mapping not applied")
	}
}

func TestLoadContextSkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "empty")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "blank.md"), []byte("   \n  "), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, "real.md"), []byte("content here"), 0o644)

	r := NewRouter(dir, nil)
	got, err := r.LoadContext("empty")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "blank.md") {
		t.Error("should skip empty files")
	}
	if !strings.Contains(got, "content here") {
		t.Error("should include non-empty files")
	}
}

func TestLoadContextSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "mixed")
	subDir := filepath.Join(skillDir, "nested")
	_ = os.MkdirAll(subDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "top.md"), []byte("top level"), 0o644)
	_ = os.WriteFile(filepath.Join(subDir, "deep.md"), []byte("nested level"), 0o644)

	r := NewRouter(dir, nil)
	got, err := r.LoadContext("mixed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "top level") {
		t.Error("should include top-level files")
	}
	if strings.Contains(got, "nested level") {
		t.Error("should not recurse into subdirs")
	}
}

func TestLoadContextYamlFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "infra")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "rules.yaml"), []byte("naming: kebab-case"), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, "env.yml"), []byte("env: production"), 0o644)

	r := NewRouter(dir, nil)
	got, err := r.LoadContext("infra")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "kebab-case") {
		t.Error("should load .yaml files")
	}
	if !strings.Contains(got, "production") {
		t.Error("should load .yml files")
	}
}

func TestInferDomainFromExtensions(t *testing.T) {
	tests := []struct {
		files []string
		want  string
	}{
		{[]string{"cmd/main.go", "internal/runner/runner.go"}, "go"},
		{[]string{"src/App.tsx", "src/index.tsx"}, "react"},
		{[]string{"infra/main.tf", "infra/vars.tf"}, "terraform"},
		{[]string{"models.py", "views.py"}, "python"},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		got := InferDomain(tt.files)
		if got != tt.want {
			t.Errorf("InferDomain(%v)=%q, want %q", tt.files, got, tt.want)
		}
	}
}

func TestInferDomainDirectoryHint(t *testing.T) {
	got := InferDomain([]string{"migrations/001_init.sql", "migrations/002_users.sql"})
	if got != "sql" {
		t.Errorf("InferDomain(migrations/*.sql)=%q, want sql", got)
	}
}

func TestNewRouterDefaults(t *testing.T) {
	r := NewRouter("", nil)
	if r.SkillDir != ".baton/skills" {
		t.Errorf("default skill dir=%q", r.SkillDir)
	}
}
