package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CompileResult struct {
	Graph        *Graph
	Health       Health
	PackageCount int
}

func Compile(projectDir string) (*CompileResult, error) {
	modulePath, err := readModulePath(projectDir)
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	dirs, err := findGoPackages(projectDir)
	if err != nil {
		return nil, fmt.Errorf("finding packages: %w", err)
	}

	var facts []*PackageFact
	for _, dir := range dirs {
		fact, err := ParsePackage(dir, modulePath)
		if err != nil {
			continue
		}
		facts = append(facts, fact)
	}

	graph := BuildGraph(facts)

	funcCount := 0
	typeCount := 0
	for _, f := range facts {
		funcCount += len(f.Functions)
		typeCount += len(f.Types)
	}

	result := &CompileResult{
		Graph:        graph,
		PackageCount: len(facts),
		Health: Health{
			CompiledAt:    time.Now().UTC(),
			PackageCount:  len(facts),
			FunctionCount: funcCount,
			TypeCount:     typeCount,
		},
	}

	return result, nil
}

func findGoPackages(root string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == ".git" || base == ".baton" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		return nil
	})

	return dirs, err
}

func readModulePath(projectDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module path not found in go.mod")
}
