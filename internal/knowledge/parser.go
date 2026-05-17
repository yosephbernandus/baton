package knowledge

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func ParsePackage(dir string, modulePath string) (*PackageFact, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", dir, err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages in %s", dir)
	}

	var pkgFiles map[string]*ast.File
	for _, p := range pkgs {
		pkgFiles = p.Files
		break
	}

	relPath, _ := filepath.Rel(moduleRoot(dir, modulePath), dir)
	if relPath == "." {
		relPath = modulePath
	} else {
		relPath = modulePath + "/" + relPath
	}

	fact := &PackageFact{
		Package:    relPath,
		Path:       dir,
		CompiledAt: time.Now().UTC(),
		SourceHash: hashDir(dir),
	}

	imports := map[string]bool{}
	for filename, file := range pkgFiles {
		base := filepath.Base(filename)

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			imports[path] = true
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				fn := extractFunction(d, base, fset)
				fact.Functions = append(fact.Functions, fn)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						tf := extractType(ts, base, fset)
						fact.Types = append(fact.Types, tf)
					}
				}
			}
		}
	}

	for imp := range imports {
		fact.Imports = append(fact.Imports, imp)
	}
	sort.Strings(fact.Imports)
	sort.Slice(fact.Functions, func(i, j int) bool { return fact.Functions[i].Name < fact.Functions[j].Name })
	sort.Slice(fact.Types, func(i, j int) bool { return fact.Types[i].Name < fact.Types[j].Name })

	return fact, nil
}

func extractFunction(fn *ast.FuncDecl, file string, fset *token.FileSet) FunctionFact {
	f := FunctionFact{
		Name:     fn.Name.Name,
		File:     file,
		Line:     fset.Position(fn.Pos()).Line,
		Exported: fn.Name.IsExported(),
	}

	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			typeStr := formatExpr(field.Type)
			if len(field.Names) == 0 {
				f.Params = append(f.Params, ParamFact{Type: typeStr})
			} else {
				for _, name := range field.Names {
					f.Params = append(f.Params, ParamFact{Name: name.Name, Type: typeStr})
				}
			}
		}
	}

	if fn.Type.Results != nil {
		var rets []string
		for _, field := range fn.Type.Results.List {
			rets = append(rets, formatExpr(field.Type))
		}
		f.Returns = strings.Join(rets, ", ")
	}

	return f
}

func extractType(ts *ast.TypeSpec, file string, fset *token.FileSet) TypeFact {
	tf := TypeFact{
		Name:     ts.Name.Name,
		File:     file,
		Line:     fset.Position(ts.Pos()).Line,
		Exported: ts.Name.IsExported(),
	}

	switch t := ts.Type.(type) {
	case *ast.StructType:
		tf.Kind = "struct"
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				typeStr := formatExpr(field.Type)
				if len(field.Names) == 0 {
					tf.Fields = append(tf.Fields, FieldFact{Name: "(embedded)", Type: typeStr})
				} else {
					for _, name := range field.Names {
						tf.Fields = append(tf.Fields, FieldFact{Name: name.Name, Type: typeStr})
					}
				}
			}
		}
	case *ast.InterfaceType:
		tf.Kind = "interface"
		if t.Methods != nil {
			for _, m := range t.Methods.List {
				if len(m.Names) > 0 {
					tf.Methods = append(tf.Methods, m.Names[0].Name)
				}
			}
		}
	default:
		tf.Kind = "alias"
	}

	return tf
}

func formatExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return formatExpr(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + formatExpr(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + formatExpr(e.Elt)
		}
		return "[...]" + formatExpr(e.Elt)
	case *ast.MapType:
		return "map[" + formatExpr(e.Key) + "]" + formatExpr(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + formatExpr(e.Value)
	case *ast.Ellipsis:
		return "..." + formatExpr(e.Elt)
	default:
		return "?"
	}
}

func hashDir(dir string) string {
	h := sha256.New()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil {
			h.Write(data)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func moduleRoot(dir, modulePath string) string {
	d := dir
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
