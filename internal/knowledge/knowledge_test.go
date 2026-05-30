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

func TestDetectLanguages(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)
	writeGoFile(t, dir, "handler.go", `package main`)
	writeFile(t, filepath.Join(dir, "style.css"), `body {}`)

	langs := DetectLanguages(dir)
	if len(langs) == 0 {
		t.Fatal("expected at least 1 language")
	}
	if langs[0].Name != "go" {
		t.Errorf("expected go as primary language, got %s", langs[0].Name)
	}
	if langs[0].FileCount != 2 {
		t.Errorf("expected 2 go files, got %d", langs[0].FileCount)
	}
}

func TestDetectLanguagesSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)
	writeFile(t, filepath.Join(dir, "vendor", "lib.go"), `package lib`)
	writeFile(t, filepath.Join(dir, "node_modules", "index.js"), `module.exports = {}`)

	langs := DetectLanguages(dir)
	goCount := 0
	for _, l := range langs {
		if l.Name == "go" {
			goCount = l.FileCount
		}
	}
	if goCount != 1 {
		t.Errorf("vendor .go should be excluded, expected 1, got %d", goCount)
	}
}

func TestDetectLanguagesEmpty(t *testing.T) {
	dir := t.TempDir()
	langs := DetectLanguages(dir)
	if len(langs) != 0 {
		t.Errorf("expected 0 languages for empty dir, got %d", len(langs))
	}
}

func TestDetectLanguage(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)
	if got := DetectLanguage(dir); got != "go" {
		t.Errorf("expected go, got %s", got)
	}

	emptyDir := t.TempDir()
	if got := DetectLanguage(emptyDir); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestTopDomain(t *testing.T) {
	signals := []DomainSignal{
		{Domain: "api", Score: 5.0},
		{Domain: "auth", Score: 3.0},
	}
	if got := TopDomain(signals); got != "api" {
		t.Errorf("expected api, got %s", got)
	}

	if got := TopDomain(nil); got != "" {
		t.Errorf("expected empty for nil, got %s", got)
	}
}

func TestSymbolKindName(t *testing.T) {
	tests := []struct {
		kind int
		want string
	}{
		{SymbolKindClass, "class"},
		{SymbolKindMethod, "method"},
		{SymbolKindField, "field"},
		{SymbolKindInterface, "interface"},
		{SymbolKindFunction, "function"},
		{SymbolKindVariable, "variable"},
		{SymbolKindConstant, "constant"},
		{SymbolKindStruct, "struct"},
		{999, "unknown"},
	}
	for _, tt := range tests {
		got := SymbolKindName(tt.kind)
		if got != tt.want {
			t.Errorf("SymbolKindName(%d) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestHashDir(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)
	writeGoFile(t, dir, "util.go", `package main`)

	h1 := hashDir(dir)
	if len(h1) != 16 {
		t.Errorf("expected 16 char hash, got %d: %s", len(h1), h1)
	}

	// Same content → same hash
	h2 := hashDir(dir)
	if h1 != h2 {
		t.Error("same dir should produce same hash")
	}

	// Modify file → different hash
	writeGoFile(t, dir, "main.go", `package main
func NewFunc() {}`)
	h3 := hashDir(dir)
	if h1 == h3 {
		t.Error("modified file should change hash")
	}
}

func TestHashDirIgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)

	h1 := hashDir(dir)

	writeGoFile(t, dir, "main_test.go", `package main`)
	h2 := hashDir(dir)

	if h1 != h2 {
		t.Error("test files should not affect hash")
	}
}

func TestSlugifyPackage(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"example.com/app/api", "example-com-app-api"},
		{"main", "main"},
		{"a/b.c/d", "a-b-c-d"},
	}
	for _, tt := range tests {
		got := slugifyPackage(tt.input)
		if got != tt.want {
			t.Errorf("slugifyPackage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildIndex(t *testing.T) {
	facts := []*PackageFact{
		{
			Package: "example.com/app",
			Path:    "/tmp/app",
			Imports: []string{"fmt"},
			Functions: []FunctionFact{
				{Name: "Main", Exported: true},
				{Name: "helper", Exported: false},
			},
			Types: []TypeFact{
				{Name: "Config", Exported: true},
			},
		},
	}
	graph := BuildGraph(facts)
	index := buildIndex(graph)

	if len(index) != 1 {
		t.Fatalf("expected 1 index entry, got %d", len(index))
	}
	if index[0].Exports != 2 {
		t.Errorf("expected 2 exports (Main + Config), got %d", index[0].Exports)
	}
	if index[0].ImportN != 1 {
		t.Errorf("expected 1 import, got %d", index[0].ImportN)
	}
}

func TestFileToPackage(t *testing.T) {
	tests := []struct {
		file, mod, want string
	}{
		{"api/handler.go", "example.com/app", "example.com/app/api"},
		{"main.go", "example.com/app", "example.com/app"},
		{"main.go", "", ""},
		{"internal/config/config.go", "example.com/app", "example.com/app/internal/config"},
		{"internal/config/config.go", "", "internal/config"},
	}
	for _, tt := range tests {
		got := fileToPackage(tt.file, tt.mod)
		if got != tt.want {
			t.Errorf("fileToPackage(%q, %q) = %q, want %q", tt.file, tt.mod, got, tt.want)
		}
	}
}

func TestLoadHealth(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main
func Hello() {}
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module test\n\ngo 1.22\n")

	result, err := Compile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, result.Graph, result.Health); err != nil {
		t.Fatal(err)
	}

	health, err := LoadHealth(dir)
	if err != nil {
		t.Fatal(err)
	}
	if health.PackageCount != 1 {
		t.Errorf("expected 1 package, got %d", health.PackageCount)
	}
	if health.FunctionCount != 1 {
		t.Errorf("expected 1 function, got %d", health.FunctionCount)
	}
}

func TestLoadHealthMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadHealth(dir)
	if err == nil {
		t.Error("expected error for missing health file")
	}
}

func TestModuleRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module test\n")

	subDir := filepath.Join(dir, "internal", "config")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := moduleRoot(subDir, "test")
	if got != dir {
		t.Errorf("expected %s, got %s", dir, got)
	}
}

func TestHasGoFiles(t *testing.T) {
	dir := t.TempDir()
	if hasGoFiles(dir) {
		t.Error("empty dir should have no go files")
	}

	writeGoFile(t, dir, "main.go", `package main`)
	if !hasGoFiles(dir) {
		t.Error("should detect go files")
	}
}

func TestFindGoPackages(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)
	if err := os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config`)

	pkgs, err := findGoPackages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Errorf("expected 2 packages, got %d: %v", len(pkgs), pkgs)
	}
}

func TestFindGoPackagesSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main`)
	writeFile(t, filepath.Join(dir, "vendor", "lib", "lib.go"), `package lib`)

	pkgs, err := findGoPackages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Errorf("expected 1 package (vendor excluded), got %d", len(pkgs))
	}
}

func TestReadModulePath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/myapp\n\ngo 1.22\n")

	mod, err := readModulePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mod != "example.com/myapp" {
		t.Errorf("expected example.com/myapp, got %s", mod)
	}
}

func TestReadModulePathMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := readModulePath(dir)
	if err == nil {
		t.Error("expected error for missing go.mod")
	}
}

func TestFindPackageByPath(t *testing.T) {
	graph := BuildGraph([]*PackageFact{
		{Package: "example.com/app", Path: "/tmp/app"},
		{Package: "example.com/lib", Path: "/tmp/lib"},
	})
	if got := findPackageByPath(graph, "/tmp/app"); got != "example.com/app" {
		t.Errorf("expected example.com/app, got %s", got)
	}
	if got := findPackageByPath(graph, "/nonexistent"); got != "" {
		t.Errorf("expected empty for missing path, got %s", got)
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
