package knowledge

import (
	"sort"
	"strings"
)

type DomainSignal struct {
	Domain string
	Score  float64
	Reason string
}

// InferDomains analyzes the knowledge graph around the given files and returns
// ranked domain signals based on package structure, imports, types, and function names.
func InferDomains(graph *Graph, files []string, modulePath string) []DomainSignal {
	if graph == nil || len(files) == 0 {
		return nil
	}

	facts := graph.Query(files, modulePath, 2)
	if len(facts) == 0 {
		return nil
	}

	scores := map[string]float64{}
	reasons := map[string][]string{}

	for _, fact := range facts {
		signals := classifyPackage(fact)
		for _, s := range signals {
			scores[s.Domain] += s.Score
			if s.Reason != "" {
				reasons[s.Domain] = append(reasons[s.Domain], s.Reason)
			}
		}
	}

	var result []DomainSignal
	for domain, score := range scores {
		reason := ""
		if rs, ok := reasons[domain]; ok && len(rs) > 0 {
			seen := map[string]bool{}
			var unique []string
			for _, r := range rs {
				if !seen[r] {
					seen[r] = true
					unique = append(unique, r)
				}
			}
			if len(unique) > 3 {
				unique = unique[:3]
			}
			reason = strings.Join(unique, ", ")
		}
		result = append(result, DomainSignal{
			Domain: domain,
			Score:  score,
			Reason: reason,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// TopDomain returns the highest-scoring domain or empty string.
func TopDomain(signals []DomainSignal) string {
	if len(signals) == 0 {
		return ""
	}
	return signals[0].Domain
}

func classifyPackage(fact *PackageFact) []DomainSignal {
	var signals []DomainSignal

	// Import-based signals
	for _, imp := range fact.Imports {
		if d := classifyImport(imp); d != "" {
			signals = append(signals, DomainSignal{Domain: d, Score: 1.0, Reason: "imports " + lastSegment(imp)})
		}
	}

	// Type-based signals
	for _, t := range fact.Types {
		if d := classifyType(t); d != "" {
			signals = append(signals, DomainSignal{Domain: d, Score: 1.5, Reason: "type " + t.Name})
		}
	}

	// Function-name signals
	for _, fn := range fact.Functions {
		if d := classifyFunction(fn); d != "" {
			signals = append(signals, DomainSignal{Domain: d, Score: 0.5, Reason: "func " + fn.Name})
		}
	}

	// Package path signals
	if d := classifyPath(fact.Package); d != "" {
		signals = append(signals, DomainSignal{Domain: d, Score: 0.8, Reason: "path " + lastSegment(fact.Package)})
	}

	// Connectivity signals (hub packages)
	if len(fact.ImportedBy) > 5 {
		signals = append(signals, DomainSignal{Domain: "core", Score: 1.0, Reason: "high fan-in"})
	}

	return signals
}

func classifyImport(imp string) string {
	low := strings.ToLower(imp)
	patterns := []struct {
		match  string
		domain string
	}{
		{"net/http", "api"},
		{"http", "api"},
		{"gin", "api"},
		{"echo", "api"},
		{"fiber", "api"},
		{"chi", "api"},
		{"mux", "api"},
		{"grpc", "api"},
		{"graphql", "api"},
		{"rest", "api"},

		{"database/sql", "database"},
		{"gorm", "database"},
		{"sqlx", "database"},
		{"ent", "database"},
		{"mongo", "database"},
		{"redis", "database"},
		{"pgx", "database"},
		{"sqlalchemy", "database"},
		{"prisma", "database"},
		{"typeorm", "database"},
		{"sequelize", "database"},
		{"knex", "database"},

		{"crypto", "security"},
		{"oauth", "security"},
		{"jwt", "security"},
		{"bcrypt", "security"},
		{"auth", "security"},
		{"saml", "security"},

		{"testing", "testing"},
		{"testify", "testing"},
		{"jest", "testing"},
		{"pytest", "testing"},
		{"mocha", "testing"},

		{"react", "frontend"},
		{"vue", "frontend"},
		{"angular", "frontend"},
		{"svelte", "frontend"},
		{"next", "frontend"},
		{"html/template", "frontend"},

		{"cobra", "cli"},
		{"flag", "cli"},
		{"argparse", "cli"},
		{"click", "cli"},
		{"commander", "cli"},

		{"terraform", "infra"},
		{"pulumi", "infra"},
		{"aws-sdk", "infra"},
		{"cloud.google", "infra"},
		{"azure", "infra"},
		{"docker", "infra"},
		{"kubernetes", "infra"},

		{"encoding/json", "serialization"},
		{"protobuf", "serialization"},
		{"yaml", "serialization"},
		{"msgpack", "serialization"},

		{"sync", "concurrency"},
		{"context", "concurrency"},
		{"channel", "concurrency"},
		{"goroutine", "concurrency"},
		{"asyncio", "concurrency"},

		{"log", "observability"},
		{"zap", "observability"},
		{"slog", "observability"},
		{"prometheus", "observability"},
		{"opentelemetry", "observability"},
		{"datadog", "observability"},

		{"os/exec", "system"},
		{"syscall", "system"},
		{"os", "system"},
		{"subprocess", "system"},
	}

	for _, p := range patterns {
		if strings.Contains(low, p.match) {
			return p.domain
		}
	}
	return ""
}

func classifyType(t TypeFact) string {
	low := strings.ToLower(t.Name)

	patterns := []struct {
		match  string
		domain string
	}{
		{"handler", "api"},
		{"controller", "api"},
		{"middleware", "api"},
		{"router", "api"},
		{"endpoint", "api"},
		{"server", "api"},
		{"request", "api"},
		{"response", "api"},

		{"repository", "database"},
		{"store", "database"},
		{"model", "database"},
		{"migration", "database"},
		{"query", "database"},
		{"dao", "database"},

		{"auth", "security"},
		{"token", "security"},
		{"credential", "security"},
		{"permission", "security"},
		{"session", "security"},

		{"config", "config"},
		{"settings", "config"},
		{"options", "config"},

		{"event", "events"},
		{"emitter", "events"},
		{"listener", "events"},
		{"subscriber", "events"},
		{"publisher", "events"},

		{"component", "frontend"},
		{"widget", "frontend"},
		{"view", "frontend"},
		{"page", "frontend"},

		{"command", "cli"},
		{"flag", "cli"},

		{"pipeline", "pipeline"},
		{"worker", "pipeline"},
		{"job", "pipeline"},
		{"task", "pipeline"},
		{"scheduler", "pipeline"},
	}

	for _, p := range patterns {
		if strings.Contains(low, p.match) {
			return p.domain
		}
	}
	return ""
}

func classifyFunction(fn FunctionFact) string {
	low := strings.ToLower(fn.Name)

	if strings.HasPrefix(low, "handle") || strings.HasPrefix(low, "serve") {
		return "api"
	}
	if strings.HasPrefix(low, "test") {
		return "testing"
	}
	if strings.HasPrefix(low, "migrate") || strings.HasPrefix(low, "seed") {
		return "database"
	}
	if strings.HasPrefix(low, "render") || strings.HasPrefix(low, "draw") {
		return "frontend"
	}
	if strings.HasPrefix(low, "encrypt") || strings.HasPrefix(low, "decrypt") || strings.HasPrefix(low, "hash") {
		return "security"
	}
	if strings.HasPrefix(low, "log") || strings.HasPrefix(low, "trace") || strings.HasPrefix(low, "metric") {
		return "observability"
	}

	return ""
}

func classifyPath(pkg string) string {
	low := strings.ToLower(pkg)
	segments := strings.Split(low, "/")

	pathDomains := map[string]string{
		"api":         "api",
		"handler":     "api",
		"handlers":    "api",
		"controller":  "api",
		"controllers": "api",
		"routes":      "api",
		"middleware":   "api",

		"db":         "database",
		"database":   "database",
		"store":      "database",
		"repo":       "database",
		"repository": "database",
		"migration":  "database",
		"migrations": "database",
		"model":      "database",
		"models":     "database",

		"auth":     "security",
		"security": "security",

		"config":  "config",
		"cfg":     "config",

		"cmd":     "cli",
		"cli":     "cli",

		"test":    "testing",
		"tests":   "testing",

		"ui":         "frontend",
		"frontend":   "frontend",
		"components": "frontend",
		"pages":      "frontend",
		"views":      "frontend",

		"infra":      "infra",
		"terraform":  "infra",
		"deploy":     "infra",

		"worker":    "pipeline",
		"workers":   "pipeline",
		"pipeline":  "pipeline",
		"queue":     "pipeline",
		"jobs":      "pipeline",

		"events":  "events",
		"event":   "events",

		"pkg":      "core",
		"internal": "core",
		"lib":      "core",
		"utils":    "core",
		"common":   "core",
	}

	for _, seg := range segments {
		if d, ok := pathDomains[seg]; ok {
			return d
		}
	}
	return ""
}

func lastSegment(s string) string {
	parts := strings.Split(s, "/")
	return parts[len(parts)-1]
}
