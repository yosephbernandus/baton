package knowledge

import (
	"testing"
)

func TestInferDomains_APIPackage(t *testing.T) {
	graph := &Graph{
		Packages: map[string]*PackageFact{
			"example.com/app/api": {
				Package: "example.com/app/api",
				Path:    "/app/api",
				Imports: []string{"net/http", "encoding/json"},
				Functions: []FunctionFact{
					{Name: "HandleGetUser", Exported: true},
					{Name: "HandleCreateUser", Exported: true},
				},
				Types: []TypeFact{
					{Name: "Handler", Kind: "struct", Exported: true},
				},
			},
		},
	}

	signals := InferDomains(graph, []string{"api/handler.go"}, "example.com/app")
	if len(signals) == 0 {
		t.Fatal("expected domain signals, got none")
	}

	top := TopDomain(signals)
	if top != "api" {
		t.Errorf("expected top domain 'api', got %q", top)
	}
}

func TestInferDomains_DatabasePackage(t *testing.T) {
	graph := &Graph{
		Packages: map[string]*PackageFact{
			"example.com/app/store": {
				Package: "example.com/app/store",
				Path:    "/app/store",
				Imports: []string{"database/sql", "github.com/lib/pgx"},
				Types: []TypeFact{
					{Name: "UserRepository", Kind: "struct", Exported: true},
					{Name: "UserStore", Kind: "interface", Exported: true},
				},
			},
		},
	}

	signals := InferDomains(graph, []string{"store/user.go"}, "example.com/app")
	top := TopDomain(signals)
	if top != "database" {
		t.Errorf("expected 'database', got %q", top)
	}
}

func TestInferDomains_FallbackEmpty(t *testing.T) {
	signals := InferDomains(nil, []string{"foo.go"}, "")
	if len(signals) != 0 {
		t.Errorf("expected no signals for nil graph, got %d", len(signals))
	}
}

func TestInferDomains_MultipleSignals(t *testing.T) {
	graph := &Graph{
		Packages: map[string]*PackageFact{
			"app/auth": {
				Package: "app/auth",
				Path:    "/app/auth",
				Imports: []string{"crypto/bcrypt", "github.com/golang-jwt/jwt"},
				Types: []TypeFact{
					{Name: "TokenService", Kind: "struct", Exported: true},
					{Name: "Credential", Kind: "struct", Exported: true},
				},
				Functions: []FunctionFact{
					{Name: "HashPassword", Exported: true},
				},
			},
		},
	}

	signals := InferDomains(graph, []string{"auth/service.go"}, "app")
	top := TopDomain(signals)
	if top != "security" {
		t.Errorf("expected 'security', got %q", top)
	}
}

func TestClassifyImport(t *testing.T) {
	tests := []struct {
		imp    string
		domain string
	}{
		// Go
		{"net/http", "api"},
		{"database/sql", "database"},
		{"crypto/bcrypt", "security"},
		{"testing", "testing"},
		{"github.com/gin-gonic/gin", "api"},
		{"github.com/jmoiron/sqlx", "database"},
		{"encoding/json", "serialization"},
		{"os/exec", "system"},
		// Python
		{"flask", "api"},
		{"fastapi", "api"},
		{"django", "api"},
		{"sqlalchemy", "database"},
		{"psycopg", "database"},
		{"pytest", "testing"},
		{"asyncio", "concurrency"},
		{"boto3", "infra"},
		{"numpy", "ml"},
		{"pandas", "ml"},
		{"torch", "ml"},
		{"langchain", "ml"},
		// TypeScript/JavaScript
		{"express", "api"},
		{"@nestjs/core", "api"},
		{"prisma", "database"},
		{"sequelize", "database"},
		{"drizzle", "database"},
		{"jest", "testing"},
		{"vitest", "testing"},
		{"react", "frontend"},
		{"vue", "frontend"},
		{"next", "frontend"},
		{"zod", "serialization"},
		{"winston", "observability"},
		// Rust
		{"axum", "api"},
		{"actix", "api"},
		{"diesel", "database"},
		{"tokio", "concurrency"},
		{"serde", "serialization"},
		{"clap", "cli"},
		// Unknown
		{"unknown/package", ""},
	}

	for _, tt := range tests {
		got := classifyImport(tt.imp)
		if got != tt.domain {
			t.Errorf("classifyImport(%q) = %q, want %q", tt.imp, got, tt.domain)
		}
	}
}

func TestClassifyType(t *testing.T) {
	tests := []struct {
		name   string
		domain string
	}{
		{"UserHandler", "api"},
		{"AuthMiddleware", "api"},
		{"UserRepository", "database"},
		{"UserStore", "database"},
		{"TokenService", "security"},
		{"AppConfig", "config"},
		{"EventEmitter", "events"},
		{"RandomThing", ""},
	}

	for _, tt := range tests {
		got := classifyType(TypeFact{Name: tt.name})
		if got != tt.domain {
			t.Errorf("classifyType(%q) = %q, want %q", tt.name, got, tt.domain)
		}
	}
}
