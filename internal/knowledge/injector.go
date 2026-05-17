package knowledge

import (
	"fmt"
	"strings"
)

const DefaultTokenBudget = 3000

func Inject(graph *Graph, files []string, modulePath string, budget int) string {
	if budget <= 0 {
		budget = DefaultTokenBudget
	}

	facts := graph.Query(files, modulePath, 2)
	if len(facts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Project Knowledge (AST-derived, verified)\n\n")

	tokens := 0
	for _, fact := range facts {
		section := formatPackageFact(fact)
		sectionTokens := estimateTokens(section)
		if tokens+sectionTokens > budget {
			break
		}
		b.WriteString(section)
		b.WriteString("\n")
		tokens += sectionTokens
	}

	return b.String()
}

func formatPackageFact(fact *PackageFact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n", fact.Package)

	if len(fact.Imports) > 0 {
		internal := filterInternal(fact.Imports)
		if len(internal) > 0 {
			fmt.Fprintf(&b, "Imports: %s\n", strings.Join(internal, ", "))
		}
	}

	if len(fact.ImportedBy) > 0 {
		var refs []string
		for _, ref := range fact.ImportedBy {
			refs = append(refs, ref.Package)
		}
		fmt.Fprintf(&b, "Used by: %s\n", strings.Join(refs, ", "))
	}

	exportedFuncs := filterExported(fact.Functions)
	if len(exportedFuncs) > 0 {
		b.WriteString("Functions:\n")
		for _, fn := range exportedFuncs {
			fmt.Fprintf(&b, "  - %s(%s)", fn.Name, formatParams(fn.Params))
			if fn.Returns != "" {
				fmt.Fprintf(&b, " → %s", fn.Returns)
			}
			b.WriteString("\n")
		}
	}

	exportedTypes := filterExportedTypes(fact.Types)
	if len(exportedTypes) > 0 {
		b.WriteString("Types:\n")
		for _, t := range exportedTypes {
			fmt.Fprintf(&b, "  - %s (%s)", t.Name, t.Kind)
			if len(t.Fields) > 0 && len(t.Fields) <= 5 {
				var fields []string
				for _, f := range t.Fields {
					fields = append(fields, f.Name+" "+f.Type)
				}
				fmt.Fprintf(&b, " {%s}", strings.Join(fields, ", "))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func formatParams(params []ParamFact) string {
	var parts []string
	for _, p := range params {
		if p.Name != "" {
			parts = append(parts, p.Name+" "+p.Type)
		} else {
			parts = append(parts, p.Type)
		}
	}
	return strings.Join(parts, ", ")
}

func filterInternal(imports []string) []string {
	var internal []string
	for _, imp := range imports {
		if !strings.Contains(imp, ".") {
			continue
		}
		parts := strings.Split(imp, "/")
		internal = append(internal, parts[len(parts)-1])
	}
	return internal
}

func filterExported(fns []FunctionFact) []FunctionFact {
	var exported []FunctionFact
	for _, fn := range fns {
		if fn.Exported {
			exported = append(exported, fn)
		}
	}
	return exported
}

func filterExportedTypes(types []TypeFact) []TypeFact {
	var exported []TypeFact
	for _, t := range types {
		if t.Exported {
			exported = append(exported, t)
		}
	}
	return exported
}

func estimateTokens(s string) int {
	return len(s) / 4
}
