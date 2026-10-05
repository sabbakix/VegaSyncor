package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sourceTexts collects every text passed to T / Tf in the Go sources of the module.
func sourceTexts(t *testing.T) map[string]string {
	t.Helper()
	texts := map[string]string{} // text -> position
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "dist" || strings.HasPrefix(d.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(filepath.ToSlash(path), "internal/i18n/") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		consts := map[string]string{} // file-level string constants
		for _, decl := range f.Decls {
			if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, spec := range g.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							if s, ok := literal(vs.Values[i], nil); ok {
								consts[name.Name] = s
							}
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok && x.Name == "i18n" {
					name = fn.Sel.Name
				}
			}
			if name != "T" && name != "Tf" {
				return true
			}
			s, ok := literal(call.Args[0], consts)
			if !ok {
				t.Errorf("%s: T/Tf argument is not a constant string", fset.Position(call.Pos()))
				return true
			}
			texts[s] = fset.Position(call.Pos()).String()
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return texts
}

// literal evaluates string literals, their concatenation and file-level constants.
func literal(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		a, ok1 := literal(v.X, consts)
		b, ok2 := literal(v.Y, consts)
		return a + b, ok1 && ok2
	case *ast.ParenExpr:
		return literal(v.X, consts)
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	}
	return "", false
}

func TestItalianCoverage(t *testing.T) {
	texts := sourceTexts(t)
	var missing []string
	for s, pos := range texts {
		if _, ok := italian[s]; !ok {
			missing = append(missing, pos+": "+strconv.Quote(s))
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Error("missing Italian translation: " + m)
	}
	// translations no longer used by the code
	var unused []string
	for k := range italian {
		if _, ok := texts[k]; !ok {
			unused = append(unused, strconv.Quote(k))
		}
	}
	sort.Strings(unused)
	for _, u := range unused {
		t.Error("unused Italian translation: " + u)
	}
}

// The translation must keep the same format verbs, in the same order.
func TestItalianFormatVerbs(t *testing.T) {
	verbs := func(s string) string {
		var out []string
		for i := 0; i < len(s)-1; i++ {
			if s[i] == '%' {
				j := i + 1
				for j < len(s) && strings.ContainsRune("+-# 0123456789.", rune(s[j])) {
					j++
				}
				if j < len(s) {
					out = append(out, string(s[j]))
					i = j
				}
			}
		}
		return strings.Join(out, "")
	}
	for en, it := range italian {
		if verbs(en) != verbs(it) {
			t.Errorf("format verbs differ: %q -> %q", en, it)
		}
	}
}

func TestLanguageSwitch(t *testing.T) {
	defer SetLang(Default)
	SetLang("it")
	if got := T("Every day"); got != "Ogni giorno" {
		t.Errorf("it: %q", got)
	}
	SetLang("EN")
	if got := T("Every day"); got != "Every day" {
		t.Errorf("en: %q", got)
	}
	SetLang("it_IT.UTF-8")
	if Lang() != Italian {
		t.Errorf("Normalize: %q", Lang())
	}
	SetLang("xx")
	if Lang() != Default {
		t.Errorf("unsupported language: %q", Lang())
	}
}
