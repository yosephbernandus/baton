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
// Uses static patterns + learned cache. Call LearnUnclassified after to teach unknowns to LLM.
func InferDomains(graph *Graph, files []string, modulePath string) []DomainSignal {
	return InferDomainsWithLearned(graph, files, modulePath, nil)
}

// InferDomainsWithLearned is like InferDomains but also checks the learned cache.
func InferDomainsWithLearned(graph *Graph, files []string, modulePath string, learned *LearnedDomains) []DomainSignal {
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
		signals := classifyPackageWithLearned(fact, learned)
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

func classifyPackageWithLearned(fact *PackageFact, learned *LearnedDomains) []DomainSignal {
	var signals []DomainSignal

	for _, imp := range fact.Imports {
		if d := classifyImport(imp); d != "" {
			signals = append(signals, DomainSignal{Domain: d, Score: 1.0, Reason: "imports " + lastSegment(imp)})
		} else if learned != nil {
			if d := learned.ClassifyImport(imp); d != "" {
				signals = append(signals, DomainSignal{Domain: d, Score: 0.8, Reason: "learned: " + lastSegment(imp)})
			}
		}
	}

	for _, t := range fact.Types {
		if d := classifyType(t); d != "" {
			signals = append(signals, DomainSignal{Domain: d, Score: 1.5, Reason: "type " + t.Name})
		} else if learned != nil {
			if d := learned.ClassifyType(t.Name); d != "" {
				signals = append(signals, DomainSignal{Domain: d, Score: 1.2, Reason: "learned type: " + t.Name})
			}
		}
	}

	for _, fn := range fact.Functions {
		if d := classifyFunction(fn); d != "" {
			signals = append(signals, DomainSignal{Domain: d, Score: 0.5, Reason: "func " + fn.Name})
		}
	}

	if d := classifyPath(fact.Package); d != "" {
		signals = append(signals, DomainSignal{Domain: d, Score: 0.8, Reason: "path " + lastSegment(fact.Package)})
	}

	if len(fact.ImportedBy) > 5 {
		signals = append(signals, DomainSignal{Domain: "core", Score: 1.0, Reason: "high fan-in"})
	}

	return signals
}

// CollectUnclassified returns imports and type names that neither static patterns
// nor learned cache could classify.
func CollectUnclassified(graph *Graph, files []string, modulePath string, learned *LearnedDomains) (unknownImports []string, unknownTypes []string) {
	if graph == nil {
		return nil, nil
	}

	facts := graph.Query(files, modulePath, 2)
	seenImports := map[string]bool{}
	seenTypes := map[string]bool{}

	for _, fact := range facts {
		for _, imp := range fact.Imports {
			if seenImports[imp] {
				continue
			}
			seenImports[imp] = true
			if classifyImport(imp) != "" {
				continue
			}
			if learned != nil && learned.ClassifyImport(imp) != "" {
				continue
			}
			unknownImports = append(unknownImports, imp)
		}

		for _, t := range fact.Types {
			if seenTypes[t.Name] {
				continue
			}
			seenTypes[t.Name] = true
			if classifyType(t) != "" {
				continue
			}
			if learned != nil && learned.ClassifyType(t.Name) != "" {
				continue
			}
			unknownTypes = append(unknownTypes, t.Name)
		}
	}

	return unknownImports, unknownTypes
}

func classifyImport(imp string) string {
	low := strings.ToLower(imp)
	patterns := []struct {
		match  string
		domain string
	}{
		// API / Web frameworks (Go, Python, JS/TS, Rust, Ruby, Java)
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
		{"flask", "api"},
		{"django", "api"},
		{"fastapi", "api"},
		{"starlette", "api"},
		{"sanic", "api"},
		{"express", "api"},
		{"koa", "api"},
		{"nestjs", "api"},
		{"hono", "api"},
		{"axum", "api"},
		{"actix", "api"},
		{"warp", "api"},
		{"rocket", "api"},
		{"rails", "api"},
		{"sinatra", "api"},
		{"spring", "api"},

		// Database / ORM (multi-language)
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
		{"drizzle", "database"},
		{"peewee", "database"},
		{"tortoise", "database"},
		{"diesel", "database"},
		{"sea-orm", "database"},
		{"activerecord", "database"},
		{"mongoose", "database"},
		{"psycopg", "database"},
		{"hibernate", "database"},
		{"mybatis", "database"},

		// Security / Auth
		{"crypto", "security"},
		{"oauth", "security"},
		{"jwt", "security"},
		{"bcrypt", "security"},
		{"auth", "security"},
		{"saml", "security"},
		{"passport", "security"},
		{"argon2", "security"},
		{"hashlib", "security"},

		// Testing (multi-language)
		{"testing", "testing"},
		{"testify", "testing"},
		{"jest", "testing"},
		{"pytest", "testing"},
		{"mocha", "testing"},
		{"vitest", "testing"},
		{"unittest", "testing"},
		{"rspec", "testing"},
		{"minitest", "testing"},
		{"junit", "testing"},
		{"playwright", "testing"},
		{"cypress", "testing"},
		{"selenium", "testing"},

		// Frontend (JS/TS)
		{"react", "frontend"},
		{"vue", "frontend"},
		{"angular", "frontend"},
		{"svelte", "frontend"},
		{"next", "frontend"},
		{"nuxt", "frontend"},
		{"remix", "frontend"},
		{"html/template", "frontend"},
		{"tailwind", "frontend"},
		{"styled-components", "frontend"},
		{"emotion", "frontend"},

		// CLI
		{"cobra", "cli"},
		{"flag", "cli"},
		{"argparse", "cli"},
		{"click", "cli"},
		{"commander", "cli"},
		{"clap", "cli"},
		{"typer", "cli"},
		{"oclif", "cli"},

		// Infrastructure / Cloud
		{"terraform", "infra"},
		{"pulumi", "infra"},
		{"aws-sdk", "infra"},
		{"cloud.google", "infra"},
		{"azure", "infra"},
		{"docker", "infra"},
		{"kubernetes", "infra"},
		{"boto3", "infra"},
		{"@aws-sdk", "infra"},

		// Serialization
		{"encoding/json", "serialization"},
		{"protobuf", "serialization"},
		{"yaml", "serialization"},
		{"msgpack", "serialization"},
		{"pydantic", "serialization"},
		{"serde", "serialization"},
		{"marshmallow", "serialization"},
		{"zod", "serialization"},

		// Concurrency / Async
		{"sync", "concurrency"},
		{"context", "concurrency"},
		{"channel", "concurrency"},
		{"goroutine", "concurrency"},
		{"asyncio", "concurrency"},
		{"tokio", "concurrency"},
		{"rayon", "concurrency"},
		{"celery", "concurrency"},
		{"bullmq", "concurrency"},

		// Observability / Logging
		{"log", "observability"},
		{"zap", "observability"},
		{"slog", "observability"},
		{"prometheus", "observability"},
		{"opentelemetry", "observability"},
		{"datadog", "observability"},
		{"logging", "observability"},
		{"loguru", "observability"},
		{"winston", "observability"},
		{"pino", "observability"},
		{"tracing", "observability"},
		{"sentry", "observability"},

		// System / OS
		{"os/exec", "system"},
		{"syscall", "system"},
		{"os", "system"},
		{"subprocess", "system"},
		{"child_process", "system"},
		{"std::process", "system"},

		// ML / Data
		{"numpy", "ml"},
		{"pandas", "ml"},
		{"torch", "ml"},
		{"tensorflow", "ml"},
		{"sklearn", "ml"},
		{"transformers", "ml"},
		{"langchain", "ml"},
		{"openai", "ml"},
		{"anthropic", "ml"},
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
