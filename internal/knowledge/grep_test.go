package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepFindsPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func Hello() string {
	return "hello world"
}

func Goodbye() string {
	return "goodbye"
}
`)
	writeFile(t, filepath.Join(dir, "util.go"), `package main

func helper() {
	_ = Hello()
}
`)

	results, err := Grep("Hello", GrepOpts{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	var hasDef, hasUsage bool
	for _, r := range results {
		if r.Kind == "definition" {
			hasDef = true
		}
		if r.Kind == "usage" {
			hasUsage = true
		}
	}

	if !hasDef {
		t.Error("expected at least one definition match")
	}
	if !hasUsage {
		t.Error("expected at least one usage match")
	}
}

func TestGrepNoResults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func Run() {}
`)

	results, err := Grep("NonExistentSymbol", GrepOpts{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestGrepFilePattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func Target() {}
`)
	writeFile(t, filepath.Join(dir, "script.py"), `def Target(): pass
`)

	results, err := Grep("Target", GrepOpts{Dir: dir, FilePattern: "*.go"})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if !strings.HasSuffix(r.File, ".go") {
			t.Errorf("expected only .go files, got %s", r.File)
		}
	}
}

func TestGrepIgnoresDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func Target() {}
`)
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "vendor", "dep.go"), `package dep
func Target() {}
`)

	results, err := Grep("Target", GrepOpts{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if strings.Contains(r.File, "vendor") {
			t.Errorf("should not match in vendor dir: %s", r.File)
		}
	}
}

func TestClassifyMatch(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"func Hello() string {", "definition"},
		{"type User struct {", "definition"},
		{"const MaxRetries = 3", "definition"},
		{"var globalState = map[string]int{}", "definition"},
		{"def process(data):", "definition"},
		{"class UserService:", "definition"},
		{"  result := Hello()", "usage"},
		{"  // func comment", "usage"},
	}

	for _, tt := range tests {
		got := classifyMatch(tt.line)
		if got != tt.want {
			t.Errorf("classifyMatch(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFormatGrepJSON(t *testing.T) {
	results := []GrepResult{
		{File: "main.go", Line: 5, Content: "func Hello()", Kind: "definition"},
	}
	out := FormatGrepJSON(results)
	if !strings.Contains(out, `"file": "main.go"`) {
		t.Errorf("expected JSON output, got: %s", out)
	}
}

func TestFormatGrepText(t *testing.T) {
	results := []GrepResult{
		{File: "main.go", Line: 5, Content: "func Hello()", Kind: "definition"},
		{File: "util.go", Line: 10, Content: "Hello()", Kind: "usage"},
	}
	out := FormatGrepText(results)
	if !strings.Contains(out, "[def]") {
		t.Error("expected [def] tag in text output")
	}
	if !strings.Contains(out, "main.go:5") {
		t.Error("expected file:line in text output")
	}
}

func TestFormatGrepTextEmpty(t *testing.T) {
	out := FormatGrepText(nil)
	if !strings.Contains(out, "No matches") {
		t.Errorf("expected 'No matches' message, got: %s", out)
	}
}
