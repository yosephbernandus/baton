package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLearnedDomains_ClassifyImport(t *testing.T) {
	ld := &LearnedDomains{
		Imports: map[string]string{
			"custom-orm":    "database",
			"my-framework":  "api",
			"internal-auth": "security",
		},
		Types: map[string]string{},
	}

	tests := []struct {
		imp    string
		domain string
	}{
		{"custom-orm", "database"},
		{"my-framework", "api"},
		{"internal-auth", "security"},
		{"github.com/org/custom-orm", "database"},
		{"unknown-lib", ""},
	}

	for _, tt := range tests {
		got := ld.ClassifyImport(tt.imp)
		if got != tt.domain {
			t.Errorf("ClassifyImport(%q) = %q, want %q", tt.imp, got, tt.domain)
		}
	}
}

func TestLearnedDomains_ClassifyType(t *testing.T) {
	ld := &LearnedDomains{
		Imports: map[string]string{},
		Types: map[string]string{
			"customwidget": "frontend",
			"datapipeline": "pipeline",
		},
	}

	if got := ld.ClassifyType("CustomWidget"); got != "frontend" {
		t.Errorf("ClassifyType(CustomWidget) = %q, want frontend", got)
	}
	if got := ld.ClassifyType("Unknown"); got != "" {
		t.Errorf("ClassifyType(Unknown) = %q, want empty", got)
	}
}

func TestSaveLoadLearnedDomains(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "project")
	_ = os.MkdirAll(filepath.Join(projectDir, ".baton", "knowledge"), 0o755)

	ld := &LearnedDomains{
		Imports: map[string]string{
			"fastapi": "api",
			"celery":  "concurrency",
		},
		Types: map[string]string{
			"myhandler": "api",
		},
	}

	// Save to project only (skip global for test)
	path := filepath.Join(projectDir, ".baton", "knowledge", learnedDomainsFile)
	data, err := os.ReadFile(path)
	if err == nil {
		t.Fatalf("file should not exist yet, got: %s", data)
	}

	err = SaveLearnedDomains(projectDir, ld)
	if err != nil {
		t.Fatalf("SaveLearnedDomains: %v", err)
	}

	// Load back
	loaded := LoadLearnedDomains(projectDir)
	if loaded.Imports["fastapi"] != "api" {
		t.Errorf("expected fastapi=api, got %q", loaded.Imports["fastapi"])
	}
	if loaded.Imports["celery"] != "concurrency" {
		t.Errorf("expected celery=concurrency, got %q", loaded.Imports["celery"])
	}
	if loaded.Types["myhandler"] != "api" {
		t.Errorf("expected myhandler=api, got %q", loaded.Types["myhandler"])
	}
}

func TestMergeLearned(t *testing.T) {
	dst := &LearnedDomains{
		Imports: map[string]string{"a": "api"},
		Types:   map[string]string{"x": "frontend"},
	}
	src := &LearnedDomains{
		Imports: map[string]string{"b": "database", "a": "cli"},
		Types:   map[string]string{"y": "testing"},
	}

	mergeLearned(dst, src)

	if dst.Imports["a"] != "cli" {
		t.Errorf("expected src to override: a=%q", dst.Imports["a"])
	}
	if dst.Imports["b"] != "database" {
		t.Errorf("expected b=database, got %q", dst.Imports["b"])
	}
	if dst.Types["x"] != "frontend" {
		t.Errorf("expected x=frontend preserved, got %q", dst.Types["x"])
	}
	if dst.Types["y"] != "testing" {
		t.Errorf("expected y=testing, got %q", dst.Types["y"])
	}
}

func TestParseLearnOutput(t *testing.T) {
	output := `Here is the classification:
{"imports": {"some-lib": "api", "other-lib": "database"}, "types": {"MyWidget": "frontend"}}
`
	ld, err := parseLearnOutput(output)
	if err != nil {
		t.Fatalf("parseLearnOutput: %v", err)
	}
	if ld.Imports["some-lib"] != "api" {
		t.Errorf("expected some-lib=api, got %q", ld.Imports["some-lib"])
	}
	if ld.Imports["other-lib"] != "database" {
		t.Errorf("expected other-lib=database, got %q", ld.Imports["other-lib"])
	}
	if ld.Types["mywidget"] != "frontend" {
		t.Errorf("expected mywidget=frontend, got %q", ld.Types["mywidget"])
	}
}

func TestParseLearnOutput_InvalidDomain(t *testing.T) {
	output := `{"imports": {"lib": "nonsense"}, "types": {}}`
	ld, err := parseLearnOutput(output)
	if err != nil {
		t.Fatalf("parseLearnOutput: %v", err)
	}
	if _, ok := ld.Imports["lib"]; ok {
		t.Error("expected invalid domain to be filtered out")
	}
}

func TestCollectUnclassified(t *testing.T) {
	graph := &Graph{
		Packages: map[string]*PackageFact{
			"app/api": {
				Package: "app/api",
				Imports: []string{"net/http", "some-unknown-lib", "another-unknown"},
				Types: []TypeFact{
					{Name: "Handler"},
					{Name: "WeirdCustomThing"},
				},
			},
		},
	}

	learned := &LearnedDomains{
		Imports: map[string]string{"some-unknown-lib": "api"},
		Types:   map[string]string{},
	}

	imports, types := CollectUnclassified(graph, []string{"api/main.go"}, "app", learned)

	// net/http → static hit, some-unknown-lib → learned hit, another-unknown → unknown
	if len(imports) != 1 || imports[0] != "another-unknown" {
		t.Errorf("expected [another-unknown], got %v", imports)
	}

	// Handler → static hit, WeirdCustomThing → unknown
	if len(types) != 1 || types[0] != "WeirdCustomThing" {
		t.Errorf("expected [WeirdCustomThing], got %v", types)
	}
}
