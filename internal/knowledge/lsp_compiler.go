package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type LSPCompileResult struct {
	Facts []*PackageFact
}

func CompileWithLSP(projectDir string, lang DetectedLang) (*LSPCompileResult, error) {
	client, err := NewLSPClient(lang.LSP, lspArgs(lang), projectDir)
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", lang.LSP, err)
	}
	defer client.Shutdown()

	files, err := findSourceFiles(projectDir, lang.Name)
	if err != nil {
		return nil, fmt.Errorf("finding source files: %w", err)
	}

	// Group files by directory (simulates packages)
	dirFiles := map[string][]string{}
	for _, f := range files {
		dir := filepath.Dir(f)
		dirFiles[dir] = append(dirFiles[dir], f)
	}

	var facts []*PackageFact
	totalSymbols := 0
	filesProcessed := 0
	filesErrored := 0

	for dir, sourceFiles := range dirFiles {
		fact := &PackageFact{
			Package:    relativePackage(projectDir, dir),
			Path:       dir,
			CompiledAt: time.Now().UTC(),
			SourceHash: hashDir(dir),
		}

		for _, f := range sourceFiles {
			if err := client.OpenFile(f); err != nil {
				filesErrored++
				continue
			}
			symbols, err := client.DocumentSymbols(f)
			client.CloseFile(f)
			if err != nil {
				filesErrored++
				continue
			}
			filesProcessed++
			totalSymbols += len(symbols)
			extractLSPSymbols(fact, symbols, f, projectDir)
		}

		if len(fact.Functions) > 0 || len(fact.Types) > 0 {
			facts = append(facts, fact)
		}
	}

	if filesProcessed == 0 && filesErrored > 0 {
		return nil, fmt.Errorf("all %d files failed LSP analysis", filesErrored)
	}

	fmt.Fprintf(os.Stderr, "  %s: %d files, %d symbols, %d packages\n", lang.Name, filesProcessed, totalSymbols, len(facts))
	return &LSPCompileResult{Facts: facts}, nil
}

func extractLSPSymbols(fact *PackageFact, symbols []DocumentSymbol, file, projectDir string) {
	relFile, _ := filepath.Rel(projectDir, file)

	for _, sym := range symbols {
		exported := isExported(sym.Name)
		line := sym.Range.Start.Line + 1 // LSP is 0-based

		switch sym.Kind {
		case SymbolKindFunction:
			fact.Functions = append(fact.Functions, FunctionFact{
				Name:     sym.Name,
				File:     relFile,
				Line:     line,
				Exported: exported,
				Returns:  extractDetail(sym.Detail),
			})

		case SymbolKindMethod:
			fact.Functions = append(fact.Functions, FunctionFact{
				Name:     sym.Name,
				File:     relFile,
				Line:     line,
				Exported: exported,
				Returns:  extractDetail(sym.Detail),
			})

		case SymbolKindClass, SymbolKindStruct:
			kind := "class"
			if sym.Kind == SymbolKindStruct {
				kind = "struct"
			}
			tf := TypeFact{
				Name:     sym.Name,
				Kind:     kind,
				File:     relFile,
				Line:     line,
				Exported: exported,
			}
			for _, child := range sym.Children {
				switch child.Kind {
				case SymbolKindField:
					tf.Fields = append(tf.Fields, FieldFact{
						Name: child.Name,
						Type: child.Detail,
					})
				case SymbolKindMethod:
					tf.Methods = append(tf.Methods, child.Name)
				}
			}
			fact.Types = append(fact.Types, tf)

		case SymbolKindInterface:
			tf := TypeFact{
				Name:     sym.Name,
				Kind:     "interface",
				File:     relFile,
				Line:     line,
				Exported: exported,
			}
			for _, child := range sym.Children {
				if child.Kind == SymbolKindMethod {
					tf.Methods = append(tf.Methods, child.Name)
				}
			}
			fact.Types = append(fact.Types, tf)

		case SymbolKindConstant, SymbolKindVariable:
			// Skip for now — focus on functions and types
		}
	}
}

func findSourceFiles(projectDir, lang string) ([]string, error) {
	var exts []string
	switch lang {
	case "python":
		exts = []string{".py"}
	case "typescript":
		exts = []string{".ts", ".tsx"}
	case "rust":
		exts = []string{".rs"}
	case "go":
		exts = []string{".go"}
	case "java":
		exts = []string{".java"}
	case "ruby":
		exts = []string{".rb"}
	case "cpp":
		exts = []string{".cpp", ".c", ".h", ".hpp"}
	default:
		for ext, entry := range langExtensions {
			if entry.lang == lang {
				exts = append(exts, ext)
			}
		}
	}

	var files []string
	filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" ||
				base == ".baton" || base == "testdata" || base == "__pycache__" ||
				base == ".venv" || base == "venv" || base == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		for _, e := range exts {
			if ext == e {
				files = append(files, path)
				break
			}
		}
		return nil
	})

	return files, nil
}

func relativePackage(projectDir, dir string) string {
	rel, err := filepath.Rel(projectDir, dir)
	if err != nil || rel == "." {
		return filepath.Base(projectDir)
	}
	return strings.ReplaceAll(rel, string(filepath.Separator), "/")
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	r := rune(name[0])
	// Go convention: uppercase = exported
	// Python/JS convention: no underscore prefix = public
	return unicode.IsUpper(r) || (unicode.IsLetter(r) && r != '_')
}

func extractDetail(detail string) string {
	if detail == "" {
		return ""
	}
	// LSP detail often contains return type or signature
	if idx := strings.LastIndex(detail, "->"); idx >= 0 {
		return strings.TrimSpace(detail[idx+2:])
	}
	return detail
}

func lspArgs(lang DetectedLang) []string {
	switch lang.LSP {
	case "gopls":
		return []string{"serve"}
	default:
		return []string{"--stdio"}
	}
}
