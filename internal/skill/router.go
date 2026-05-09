package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Router struct {
	SkillDir  string
	DomainMap map[string]string
}

func NewRouter(skillDir string, domainMap map[string]string) *Router {
	if skillDir == "" {
		skillDir = ".baton/skills"
	}
	return &Router{SkillDir: skillDir, DomainMap: domainMap}
}

func (r *Router) LoadContext(domain string) (string, error) {
	if domain == "" {
		return "", nil
	}

	mapped := domain
	if r.DomainMap != nil {
		if m, ok := r.DomainMap[domain]; ok {
			mapped = m
		}
	}

	dir := filepath.Join(r.SkillDir, mapped)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading skill dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".md" || ext == ".txt" || ext == ".yaml" || ext == ".yml" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "--- %s ---\n", f)
		b.WriteString(content)
		b.WriteString("\n\n")
	}

	return b.String(), nil
}

func InferDomain(contextFiles []string) string {
	ext := make(map[string]int)
	dir := make(map[string]int)

	for _, f := range contextFiles {
		e := strings.TrimPrefix(filepath.Ext(f), ".")
		if e != "" {
			ext[e]++
		}
		parts := strings.Split(filepath.Dir(f), string(filepath.Separator))
		for _, p := range parts {
			if p != "" && p != "." {
				dir[p]++
			}
		}
	}

	extToDomain := map[string]string{
		"go":    "go",
		"py":    "python",
		"js":    "javascript",
		"ts":    "typescript",
		"tsx":   "react",
		"jsx":   "react",
		"sql":   "sql",
		"tf":    "terraform",
		"hcl":   "terraform",
		"rs":    "rust",
		"rb":    "ruby",
		"java":  "java",
		"kt":    "kotlin",
		"swift": "swift",
	}

	dirToDomain := map[string]string{
		"terraform":  "terraform",
		"infra":      "infra",
		"frontend":   "frontend",
		"backend":    "backend",
		"api":        "backend",
		"cmd":        "go",
		"internal":   "go",
		"pkg":        "go",
		"src":        "frontend",
		"components": "react",
		"migrations": "sql",
		"tests":      "testing",
		"test":       "testing",
	}

	best := ""
	bestCount := 0

	for e, count := range ext {
		if d, ok := extToDomain[e]; ok && count > bestCount {
			best = d
			bestCount = count
		}
	}

	for d, count := range dir {
		if domain, ok := dirToDomain[d]; ok && count > bestCount {
			best = domain
			bestCount = count
		}
	}

	return best
}
