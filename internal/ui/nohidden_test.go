package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// No program of Spytify sets a file's Hidden attribute, for every OS: the
// parser reads the files of all three, whatever this test runs on. A
// program that hides a folder it writes in the user's files is T1564.001
// to a behaviour engine, and the user cannot see what takes the space
// (#900).
func TestNothingSetsTheHiddenAttribute(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := 0
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "FILE_ATTRIBUTE_HIDDEN" {
					t.Errorf("%s: uses FILE_ATTRIBUTE_HIDDEN", fset.Position(id.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files < 50 {
		t.Fatalf("read only %d files under %s", files, root)
	}
}
