package proc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	pathpkg "path"
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

// No code starts explorer.exe: a new Explorer process, child of an unknown
// program and with the user's file on its command line, is a pattern
// security software watches. Show in folder asks the shell in-process
// (osopen.Reveal).
func TestNothingStartsExplorer(t *testing.T) {
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
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				case *ast.Ident:
					name = fun.Name
				}
				arg := 0
				switch name {
				case "Command":
				case "CommandContext":
					arg = 1
				default:
					return true
				}
				if len(call.Args) <= arg {
					return true
				}
				lit, ok := call.Args[arg].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				program, _ := strconv.Unquote(lit.Value)
				if strings.TrimSuffix(strings.ToLower(pathpkg.Base(strings.ReplaceAll(program, `\`, "/"))), ".exe") == "explorer" {
					t.Errorf("%s: starts %s; use osopen.Reveal or osopen.Open", fset.Position(call.Pos()), program)
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
