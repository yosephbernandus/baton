package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Update(projectDir string, changedFiles []string) error {
	graph, err := Load(projectDir)
	if err != nil {
		return fmt.Errorf("loading knowledge: %w (run 'baton knowledge compile' first)", err)
	}

	// Map changed files to affected packages
	affectedDirs := map[string]bool{}
	for _, f := range changedFiles {
		absPath := f
		if !filepath.IsAbs(f) {
			absPath = filepath.Join(projectDir, f)
		}
		dir := filepath.Dir(absPath)
		affectedDirs[dir] = true
	}

	if len(affectedDirs) == 0 {
		return nil
	}

	// Determine which languages are affected
	langs := DetectLanguages(projectDir)
	goAvailable := false
	for _, l := range langs {
		if l.Name == "go" {
			goAvailable = true
		}
	}

	updated := 0
	for dir := range affectedDirs {
		pkg := findPackageByPath(graph, dir)
		if pkg == "" {
			continue
		}

		// Re-parse the affected package
		var newFact *PackageFact
		if goAvailable && hasGoFiles(dir) {
			modPath := readModPathSafe(projectDir)
			fact, err := ParsePackage(dir, modPath)
			if err != nil {
				continue
			}
			newFact = fact
		} else {
			// For non-Go, check if LSP available
			for _, l := range langs {
				if l.Available && dirHasLangFiles(dir, l.Name) {
					client, err := NewLSPClient(l.LSP, lspArgs(l), projectDir)
					if err != nil {
						continue
					}
					files, _ := findSourceFilesInDir(dir, l.Name)
					fact := &PackageFact{
						Package:    relativePackage(projectDir, dir),
						Path:       dir,
						CompiledAt: time.Now().UTC(),
						SourceHash: hashDir(dir),
					}
					for _, f := range files {
						client.OpenFile(f)
						symbols, err := client.DocumentSymbols(f)
						client.CloseFile(f)
						if err != nil {
							continue
						}
						extractLSPSymbols(fact, symbols, f, projectDir)
					}
					client.Shutdown()
					if len(fact.Functions) > 0 || len(fact.Types) > 0 {
						newFact = fact
					}
					break
				}
			}
		}

		if newFact != nil {
			graph.Packages[pkg] = newFact
			updated++
		}
	}

	if updated == 0 {
		return nil
	}

	// Rebuild edges
	graph.Edges = nil
	for _, f := range graph.Packages {
		f.ImportedBy = nil
	}
	for _, f := range graph.Packages {
		for _, imp := range f.Imports {
			if target, ok := graph.Packages[imp]; ok {
				graph.Edges = append(graph.Edges, Edge{
					From: f.Package,
					To:   imp,
					Type: "imports",
				})
				target.ImportedBy = append(target.ImportedBy, ImportRef{
					Package: f.Package,
				})
			}
		}
	}

	// Recompute health
	funcCount := 0
	typeCount := 0
	for _, f := range graph.Packages {
		funcCount += len(f.Functions)
		typeCount += len(f.Types)
	}

	health := Health{
		CompiledAt:    time.Now().UTC(),
		PackageCount:  len(graph.Packages),
		FunctionCount: funcCount,
		TypeCount:     typeCount,
	}

	fmt.Fprintf(os.Stderr, "knowledge updated: %d packages refreshed\n", updated)
	return Save(projectDir, graph, health)
}

func findPackageByPath(graph *Graph, dir string) string {
	for pkg, fact := range graph.Packages {
		if fact.Path == dir {
			return pkg
		}
	}
	return ""
}

func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func dirHasLangFiles(dir, lang string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if entry, ok := langExtensions[ext]; ok && entry.lang == lang {
			return true
		}
	}
	return false
}

func findSourceFilesInDir(dir, lang string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if entry, ok := langExtensions[ext]; ok && entry.lang == lang {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

func readModPathSafe(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
