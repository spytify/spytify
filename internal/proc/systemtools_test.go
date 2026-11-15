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

// macOSTools are the system programs spytify starts on macOS, by the path
// macOS installs them at. Started by name, the first program of that name
// on PATH would run in their place.
var macOSTools = map[string]string{
	"ps":        "/bin/ps",
	"launchctl": "/bin/launchctl",
	"open":      "/usr/bin/open",
	"osascript": "/usr/bin/osascript",
	"plutil":    "/usr/bin/plutil",
	"ioreg":     "/usr/sbin/ioreg",
	"lsof":      "/usr/sbin/lsof",
}

// byName may be started by name: programs of Windows and Linux, where they
// live in different folders from one system to another, and spytify's own.
var byName = map[string]string{
	"cmd":         "Windows",
	"explorer":    "Windows",
	"xdg-open":    "Linux",
	"systemctl":   "Linux",
	"pactl":       "Linux",
	"parec":       "Linux",
	"pw-cli":      "Linux",
	"pw-dump":     "Linux",
	"pw-metadata": "Linux",
	"pw-record":   "Linux",
	"pw-top":      "Linux",
	"spytifyd":    "spytify's own, looked for beside the program first",
}

// A program spytify starts from a name written in its code is either a
// full path or a program of Windows or Linux. A new macOS tool started by
// name fails here: give its full path, or add it to byName with the reason.
func TestMacOSSystemToolsAreStartedByTheirFullPath(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, skip := range []string{"internal/proc/", "internal/tools/", "cmd/fakeplayer/"} {
				if strings.HasPrefix(rel, skip) {
					return nil
				}
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			names := map[string]string{}
			for _, im := range f.Imports {
				p, _ := strconv.Unquote(im.Path.Value)
				name := p[strings.LastIndex(p, "/")+1:]
				if im.Name != nil {
					name = im.Name.Name
				}
				names[name] = p
			}
			// program returns the argument that names the program.
			program := func(call *ast.CallExpr) ast.Expr {
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return nil
				}
				x, ok := sel.X.(*ast.Ident)
				if !ok {
					return nil
				}
				arg := -1
				switch names[x.Name] + "." + sel.Sel.Name {
				case "github.com/spytify/spytify/internal/proc.Command", "os/exec.Command", "os/exec.LookPath":
					arg = 0
				case "github.com/spytify/spytify/internal/proc.CommandContext", "os/exec.CommandContext":
					arg = 1
				}
				if arg < 0 || len(call.Args) <= arg {
					return nil
				}
				return call.Args[arg]
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					arg := program(n)
					if arg == nil {
						return true
					}
					calls++
					lit, ok := arg.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					name, _ := strconv.Unquote(lit.Value)
					if strings.HasPrefix(name, "/") || byName[name] != "" {
						return true
					}
					if full := macOSTools[name]; full != "" {
						t.Errorf("%s: %q started by name; use %q", fset.Position(lit.Pos()), name, full)
					} else {
						t.Errorf("%s: %q started by name; give its full path, or add it to byName", fset.Position(lit.Pos()), name)
					}
				case *ast.CompositeLit:
					// A command line kept as a list, such as a service's
					// launchctl commands, started later.
					if len(n.Elts) == 0 {
						return true
					}
					if lit, ok := n.Elts[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						name, _ := strconv.Unquote(lit.Value)
						if full := macOSTools[name]; full != "" && name != "open" {
							t.Errorf("%s: command %q by name; use %q", fset.Position(lit.Pos()), name, full)
						}
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
	if calls < 30 {
		t.Fatalf("found only %d programs started under %s", calls, root)
	}
}
