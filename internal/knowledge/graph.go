package knowledge

import "path/filepath"

func BuildGraph(facts []*PackageFact) *Graph {
	g := &Graph{
		Packages: make(map[string]*PackageFact),
	}

	for _, f := range facts {
		g.Packages[f.Package] = f
	}

	// Build import edges and imported_by reverse references
	for _, f := range facts {
		for _, imp := range f.Imports {
			if target, ok := g.Packages[imp]; ok {
				g.Edges = append(g.Edges, Edge{
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

	return g
}

func (g *Graph) Query(files []string, modulePath string, maxHops int) []*PackageFact {
	if maxHops <= 0 {
		maxHops = 2
	}

	// Map files to packages
	seedPkgs := map[string]bool{}
	for _, f := range files {
		pkg := fileToPackage(f, modulePath)
		if pkg != "" {
			seedPkgs[pkg] = true
		}
	}

	// BFS traversal up to maxHops
	visited := map[string]bool{}
	queue := make([]string, 0, len(seedPkgs))
	depths := map[string]int{}

	for pkg := range seedPkgs {
		queue = append(queue, pkg)
		depths[pkg] = 0
		visited[pkg] = true
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		depth := depths[current]

		if depth >= maxHops {
			continue
		}

		// Follow edges in both directions
		for _, edge := range g.Edges {
			var neighbor string
			if edge.From == current {
				neighbor = edge.To
			} else if edge.To == current {
				neighbor = edge.From
			}
			if neighbor != "" && !visited[neighbor] {
				visited[neighbor] = true
				depths[neighbor] = depth + 1
				queue = append(queue, neighbor)
			}
		}
	}

	var results []*PackageFact
	for pkg := range visited {
		if fact, ok := g.Packages[pkg]; ok {
			results = append(results, fact)
		}
	}

	return results
}

func fileToPackage(file, modulePath string) string {
	dir := filepath.Dir(file)
	if dir == "." {
		if modulePath == "" {
			return ""
		}
		return modulePath
	}
	if modulePath == "" {
		return dir
	}
	return modulePath + "/" + dir
}

// StalePackages returns packages whose source hash differs from stored hash.
func (g *Graph) StalePackages() []string {
	var stale []string
	for _, pkg := range g.Packages {
		current := hashDir(pkg.Path)
		if current != pkg.SourceHash {
			stale = append(stale, pkg.Package)
		}
	}
	return stale
}
