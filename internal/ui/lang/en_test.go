package lang

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// texts reads en.go: each name with its text, in the file's order.
func texts(t *testing.T) (names []string, text map[string]string) {
	t.Helper()
	set := token.NewFileSet()
	f, err := parser.ParseFile(set, "en.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{}).Check("lang", set, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	text = map[string]string{}
	for _, d := range f.Decls {
		for _, spec := range d.(*ast.GenDecl).Specs {
			for _, id := range spec.(*ast.ValueSpec).Names {
				names = append(names, id.Name)
				text[id.Name] = constant.StringVal(info.Defs[id].(*types.Const).Val())
			}
		}
	}
	return names, text
}

// A text is written once: under two names, one would change without the
// other.
func TestEachTextIsWrittenOnce(t *testing.T) {
	names, text := texts(t)
	first := map[string]string{}
	for _, name := range names {
		if other, ok := first[text[name]]; ok {
			t.Errorf("%s and %s both say %q", other, name, text[name])
			continue
		}
		first[text[name]] = name
	}
}

// Each text is one the window shows: the code outside tests uses its name,
// on one system at least.
func TestEachTextIsUsed(t *testing.T) {
	used := map[string]bool{}
	set := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Dir(path) == "." {
			return err
		}
		f, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "lang" {
					used[sel.Sel.Name] = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names, _ := texts(t)
	for _, name := range names {
		if !used[name] {
			t.Errorf("%s is in the lexicon but nothing shows it", name)
		}
	}
}
