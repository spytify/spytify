package proc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowed may start programs with os/exec directly.
var allowed = map[string]string{
	"internal/proc/":    "the helper itself",
	"internal/ui/link/": "starts spytifyd detached, with flags of its own",
	"internal/tools/":   "developer tools",
	"cmd/spytify-cli/":  "a console program run from a terminal",
	"cmd/fakeplayer/":   "the test player",
}

// Every program spytify starts goes through Command or CommandContext, for
// every OS: the parser reads the files of all three, whatever this test
// runs on. A program started another way opens a console window on
// Windows each time it runs.
func TestEveryChildProcessGoesThroughProc(t *testing.T) {
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
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for prefix := range allowed {
				if strings.HasPrefix(rel, prefix) {
					return nil
				}
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files++
			exec := ""
			for _, im := range f.Imports {
				if p, _ := strconv.Unquote(im.Path.Value); p == "os/exec" {
					exec = "exec"
					if im.Name != nil {
						exec = im.Name.Name
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if sel, ok := n.Fun.(*ast.SelectorExpr); ok && exec != "" {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == exec && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
							t.Errorf("%s: exec.%s; use proc.%s", fset.Position(n.Pos()), sel.Sel.Name, sel.Sel.Name)
						}
					}
				case *ast.SelectorExpr:
					// Setting SysProcAttr would drop the flags that hide the window.
					if n.Sel.Name == "SysProcAttr" {
						t.Errorf("%s: sets SysProcAttr, which undoes what proc set", fset.Position(n.Pos()))
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
	if files < 50 {
		t.Fatalf("read only %d files under %s", files, root)
	}
}
