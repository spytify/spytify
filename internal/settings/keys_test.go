package settings_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// Code outside the package names a setting with a generated Key: a string,
// misspelt or not, does not compile, and neither does a Key made up there.
func TestOnlyAGeneratedKeyCompiles(t *testing.T) {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	compile := func(arg string) error {
		src := "package p\n\nimport \"github.com/spytify/spytify/internal/settings\"\n\nvar _ = settings.Defaults().Bool(" + arg + ")\n"
		f, err := parser.ParseFile(fset, "p.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&types.Config{Importer: imp}).Check("p", fset, []*ast.File{f}, nil)
		return err
	}
	// Also tells that the package is found, which the failures below need
	// to mean anything.
	if err := compile("settings.RecorderTrimSilence"); err != nil {
		t.Fatalf("a generated key does not compile: %v", err)
	}
	for _, arg := range []string{
		`"recorder.made_up"`,
		`"recorder.trim_silence"`,
		`settings.Key("recorder.made_up")`,
		`settings.Key{"recorder.made_up"}`,
	} {
		if compile(arg) == nil {
			t.Errorf("Bool(%s) compiles", arg)
		}
	}
}
