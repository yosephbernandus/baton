package knowledge

import "time"

type PackageFact struct {
	Package    string         `yaml:"package"`
	Path       string         `yaml:"path"`
	Imports    []string       `yaml:"imports"`
	ImportedBy []ImportRef    `yaml:"imported_by,omitempty"`
	Functions  []FunctionFact `yaml:"functions,omitempty"`
	Types      []TypeFact     `yaml:"types,omitempty"`
	CompiledAt time.Time      `yaml:"compiled_at"`
	SourceHash string         `yaml:"source_hash"`
}

type ImportRef struct {
	Package string `yaml:"package"`
	File    string `yaml:"file"`
	Line    int    `yaml:"line"`
}

type FunctionFact struct {
	Name     string      `yaml:"name"`
	Params   []ParamFact `yaml:"params,omitempty"`
	Returns  string      `yaml:"returns,omitempty"`
	File     string      `yaml:"file"`
	Line     int         `yaml:"line"`
	Exported bool        `yaml:"exported"`
}

type ParamFact struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type TypeFact struct {
	Name     string      `yaml:"name"`
	Kind     string      `yaml:"kind"` // struct, interface, alias
	Fields   []FieldFact `yaml:"fields,omitempty"`
	Methods  []string    `yaml:"methods,omitempty"`
	File     string      `yaml:"file"`
	Line     int         `yaml:"line"`
	Exported bool        `yaml:"exported"`
}

type FieldFact struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type Edge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
	Type string `yaml:"type"` // imports, implements
}

type Graph struct {
	Packages map[string]*PackageFact `yaml:"packages"`
	Edges    []Edge                  `yaml:"edges"`
}

type IndexEntry struct {
	Package  string `yaml:"package"`
	Path     string `yaml:"path"`
	Summary  string `yaml:"summary"`
	Exports  int    `yaml:"exports"`
	ImportN  int    `yaml:"imports"`
}

type Health struct {
	CompiledAt   time.Time `yaml:"compiled_at"`
	PackageCount int       `yaml:"package_count"`
	FunctionCount int      `yaml:"function_count"`
	TypeCount    int       `yaml:"type_count"`
	StaleCount   int       `yaml:"stale_count"`
}

type SoftClaim struct {
	Claim        string `yaml:"claim"`
	Package      string `yaml:"package"`
	Verified     bool   `yaml:"verified"`
	Verification string `yaml:"verification"`
	Confidence   string `yaml:"confidence"` // verified, unverified, rejected
	VerifiedAt   string `yaml:"verified_at,omitempty"`
}
