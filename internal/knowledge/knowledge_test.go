package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePackage(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

import "fmt"

type User struct {
	ID   int
	Name string
}

func Hello(name string) string {
	return fmt.Sprintf("hello %s", name)
}

func internal() {}
`)

	fact, err := ParsePackage(dir, "example.com/test")
	if err != nil {
		t.Fatal(err)
	}

	if fact.Package == "" {
		t.Error("expected non-empty package")
	}

	if len(fact.Imports) != 1 || fact.Imports[0] != "fmt" {
		t.Errorf("expected [fmt], got %v", fact.Imports)
	}

	if len(fact.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(fact.Functions))
	}

	var hello FunctionFact
	for _, f := range fact.Functions {
		if f.Name == "Hello" {
			hello = f
		}
	}
	if !hello.Exported {
		t.Error("Hello should be exported")
	}
	if len(hello.Params) != 1 || hello.Params[0].Name != "name" {
		t.Errorf("expected param 'name', got %v", hello.Params)
	}
	if hello.Returns != "string" {
		t.Errorf("expected return 'string', got %s", hello.Returns)
	}

	if len(fact.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(fact.Types))
	}
	if fact.Types[0].Name != "User" || fact.Types[0].Kind != "struct" {
		t.Errorf("expected User struct, got %s %s", fact.Types[0].Name, fact.Types[0].Kind)
	}
	if len(fact.Types[0].Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(fact.Types[0].Fields))
	}
}

func TestParsePackageInterface(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "svc.go", `package svc

type Storage interface {
	Get(id string) ([]byte, error)
	Put(id string, data []byte) error
	Delete(id string) error
}
`)

	fact, err := ParsePackage(dir, "example.com/test")
	if err != nil {
		t.Fatal(err)
	}

	if len(fact.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(fact.Types))
	}
	if fact.Types[0].Kind != "interface" {
		t.Errorf("expected interface, got %s", fact.Types[0].Kind)
	}
	if len(fact.Types[0].Methods) != 3 {
		t.Errorf("expected 3 methods, got %d", len(fact.Types[0].Methods))
	}
}

func TestBuildGraph(t *testing.T) {
	facts := []*PackageFact{
		{Package: "example.com/app/api", Imports: []string{"example.com/app/auth", "example.com/app/db"}},
		{Package: "example.com/app/auth", Imports: []string{"example.com/app/cache"}},
		{Package: "example.com/app/db", Imports: []string{}},
		{Package: "example.com/app/cache", Imports: []string{}},
	}

	graph := BuildGraph(facts)

	if len(graph.Edges) != 3 {
		t.Errorf("expected 3 edges, got %d", len(graph.Edges))
	}

	// auth should be imported_by api
	auth := graph.Packages["example.com/app/auth"]
	if len(auth.ImportedBy) != 1 || auth.ImportedBy[0].Package != "example.com/app/api" {
		t.Errorf("expected auth imported by api, got %v", auth.ImportedBy)
	}
}

func TestGraphQuery(t *testing.T) {
	facts := []*PackageFact{
		{Package: "example.com/app/api", Path: "/tmp/api", Imports: []string{"example.com/app/auth"}},
		{Package: "example.com/app/auth", Path: "/tmp/auth", Imports: []string{"example.com/app/cache"}},
		{Package: "example.com/app/cache", Path: "/tmp/cache", Imports: []string{}},
		{Package: "example.com/app/unrelated", Path: "/tmp/unrelated", Imports: []string{}},
	}

	graph := BuildGraph(facts)

	results := graph.Query([]string{"api/handler.go"}, "example.com/app", 1)

	found := map[string]bool{}
	for _, r := range results {
		found[r.Package] = true
	}

	if !found["example.com/app/api"] {
		t.Error("expected api in results (seed)")
	}
	if !found["example.com/app/auth"] {
		t.Error("expected auth in results (1 hop)")
	}
	if found["example.com/app/cache"] {
		t.Error("cache should NOT be in results (2 hops, max 1)")
	}
	if found["example.com/app/unrelated"] {
		t.Error("unrelated should NOT be in results")
	}
}

func TestGraphQueryTwoHops(t *testing.T) {
	facts := []*PackageFact{
		{Package: "example.com/app/api", Path: "/tmp/api", Imports: []string{"example.com/app/auth"}},
		{Package: "example.com/app/auth", Path: "/tmp/auth", Imports: []string{"example.com/app/cache"}},
		{Package: "example.com/app/cache", Path: "/tmp/cache", Imports: []string{}},
	}

	graph := BuildGraph(facts)
	results := graph.Query([]string{"api/handler.go"}, "example.com/app", 2)

	found := map[string]bool{}
	for _, r := range results {
		found[r.Package] = true
	}

	if !found["example.com/app/cache"] {
		t.Error("expected cache in results (2 hops)")
	}
}

func TestInject(t *testing.T) {
	facts := []*PackageFact{
		{
			Package: "example.com/app/api",
			Path:    "/tmp/api",
			Imports: []string{"example.com/app/auth"},
			Functions: []FunctionFact{
				{Name: "CreateUser", Exported: true, Params: []ParamFact{{Name: "w", Type: "http.ResponseWriter"}}, Returns: ""},
				{Name: "helper", Exported: false},
			},
			Types: []TypeFact{
				{Name: "Handler", Kind: "struct", Exported: true, Fields: []FieldFact{{Name: "DB", Type: "*sql.DB"}}},
			},
		},
	}

	graph := BuildGraph(facts)
	output := Inject(graph, []string{"api/handler.go"}, "example.com/app", 5000)

	if output == "" {
		t.Fatal("expected non-empty output")
	}
	if !contains(output, "CreateUser") {
		t.Error("expected CreateUser in output")
	}
	if contains(output, "helper") {
		t.Error("unexported helper should not be in output")
	}
	if !contains(output, "Handler") {
		t.Error("expected Handler type in output")
	}
}

func TestCompileAndSave(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal Go project
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.22\n")
	writeGoFile(t, dir, "main.go", `package main

func Main() string { return "hello" }
`)

	result, err := Compile(dir)
	if err != nil {
		t.Fatal(err)
	}

	if result.PackageCount != 1 {
		t.Errorf("expected 1 package, got %d", result.PackageCount)
	}

	if err := Save(dir, result.Graph, result.Health); err != nil {
		t.Fatal(err)
	}

	// Verify files written
	if _, err := os.Stat(filepath.Join(dir, KnowledgeDir, "index.yaml")); err != nil {
		t.Error("index.yaml not created")
	}
	if _, err := os.Stat(filepath.Join(dir, KnowledgeDir, "graph.yaml")); err != nil {
		t.Error("graph.yaml not created")
	}
	if _, err := os.Stat(filepath.Join(dir, KnowledgeDir, "health.yaml")); err != nil {
		t.Error("health.yaml not created")
	}

	// Load back
	graph, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Packages) != 1 {
		t.Errorf("expected 1 package loaded, got %d", len(graph.Packages))
	}
}

func TestVerifyClaim(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "handler.go"), `package api

import "net/http"

func Handle(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
`)

	// Claim: "never uses StatusForbidden" — should pass (absent)
	claim := SoftClaim{
		Claim:   "API returns 401, never 403",
		Package: "api",
	}
	rule := VerifyRule{
		Pattern: "StatusForbidden",
		Expect:  "absent",
		Scope:   "src",
	}

	result := VerifyClaim(dir, claim, rule)
	if !result.Passed {
		t.Errorf("expected claim to pass, got: %s", result.Evidence)
	}
	if result.Claim.Confidence != "verified" {
		t.Errorf("expected confidence verified, got %s", result.Claim.Confidence)
	}

	// Claim: "uses StatusUnauthorized" — should pass (exists)
	rule2 := VerifyRule{
		Pattern: "StatusUnauthorized",
		Expect:  "exists",
		Scope:   "src",
	}
	result2 := VerifyClaim(dir, claim, rule2)
	if !result2.Passed {
		t.Errorf("expected exists check to pass")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func writeGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
