package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A link opens a web page and nothing else: on Windows, the shell runs a
// program it is given a path to.
func TestALinkOpensOnlyAWebPage(t *testing.T) {
	a, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	opened := fakeBrowser(t)
	for _, raw := range []string{
		siteURL,
		"http://example.com/a?b=c&d=e",
		`C:\Windows\System32\calc.exe`,
		"file:///C:/Windows/System32/calc.exe",
		"/System/Applications/Calculator.app",
		"-a Calculator",
		"",
	} {
		a.openURL(raw)
	}
	if want := []string{siteURL, "http://example.com/a?b=c&d=e"}; !slices.Equal(*opened, want) {
		t.Errorf("opened %v, want %v", *opened, want)
	}
}

// No link goes through Fyne, which opens one with rundll32 on Windows: the
// app's links call openURL, and a Hyperlink gets no URL of its own, for it
// would open it through Fyne when tapped. The parser reads the files of
// every OS, whatever this test runs on.
func TestNoLinkOpensThroughFyne(t *testing.T) {
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
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch {
				case sel.Sel.Name == "OpenURL", sel.Sel.Name == "SetURL":
					t.Errorf("%s: %s opens the link through Fyne; use openURL", fset.Position(n.Pos()), sel.Sel.Name)
				case sel.Sel.Name == "NewHyperlink" && len(call.Args) == 2:
					if id, ok := call.Args[1].(*ast.Ident); !ok || id.Name != "nil" {
						t.Errorf("%s: a Hyperlink with a URL opens it through Fyne; give it nil and OnTapped", fset.Position(n.Pos()))
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files < 100 {
		t.Fatalf("read %d files: the walk missed the source tree", files)
	}
}
