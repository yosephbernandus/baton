package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const KnowledgeDir = ".baton/knowledge"

func Save(baseDir string, graph *Graph, health Health) error {
	kDir := filepath.Join(baseDir, KnowledgeDir)
	pkgDir := filepath.Join(kDir, "packages")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return fmt.Errorf("creating knowledge dir: %w", err)
	}

	// Save per-package facts
	for _, pkg := range graph.Packages {
		filename := slugifyPackage(pkg.Package) + ".yaml"
		path := filepath.Join(pkgDir, filename)
		if err := writeYAML(path, pkg); err != nil {
			return fmt.Errorf("saving package %s: %w", pkg.Package, err)
		}
	}

	// Save graph edges
	graphData := struct {
		Edges []Edge `yaml:"edges"`
	}{Edges: graph.Edges}
	if err := writeYAML(filepath.Join(kDir, "graph.yaml"), graphData); err != nil {
		return fmt.Errorf("saving graph: %w", err)
	}

	// Save index
	index := buildIndex(graph)
	if err := writeYAML(filepath.Join(kDir, "index.yaml"), index); err != nil {
		return fmt.Errorf("saving index: %w", err)
	}

	// Save health
	if err := writeYAML(filepath.Join(kDir, "health.yaml"), health); err != nil {
		return fmt.Errorf("saving health: %w", err)
	}

	return nil
}

func Load(baseDir string) (*Graph, error) {
	kDir := filepath.Join(baseDir, KnowledgeDir)
	pkgDir := filepath.Join(kDir, "packages")

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("reading knowledge packages: %w", err)
	}

	var facts []*PackageFact
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			continue
		}
		var fact PackageFact
		if err := yaml.Unmarshal(data, &fact); err != nil {
			continue
		}
		facts = append(facts, &fact)
	}

	graph := BuildGraph(facts)
	return graph, nil
}

func LoadHealth(baseDir string) (*Health, error) {
	path := filepath.Join(baseDir, KnowledgeDir, "health.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var h Health
	if err := yaml.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func buildIndex(graph *Graph) []IndexEntry {
	var entries []IndexEntry
	for _, pkg := range graph.Packages {
		exportCount := 0
		for _, f := range pkg.Functions {
			if f.Exported {
				exportCount++
			}
		}
		for _, t := range pkg.Types {
			if t.Exported {
				exportCount++
			}
		}

		summary := fmt.Sprintf("%d functions, %d types", len(pkg.Functions), len(pkg.Types))
		entries = append(entries, IndexEntry{
			Package: pkg.Package,
			Path:    pkg.Path,
			Summary: summary,
			Exports: exportCount,
			ImportN: len(pkg.Imports),
		})
	}
	return entries
}

func slugifyPackage(pkg string) string {
	s := strings.ReplaceAll(pkg, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

func writeYAML(path string, v interface{}) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
