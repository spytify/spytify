package openfiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A file another program reads, as a virus scanner or a cloud drive does,
// cannot be renamed: the error names the program that holds it, here this
// test.
func TestARenameRefusedNamesTheProgramHoldingTheFile(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "Artist - Title.mp3.part")
	if err := os.WriteFile(part, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(part)
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	final := strings.TrimSuffix(part, ".part")
	err = os.Rename(part, final)
	if err == nil {
		t.Fatal("a file held open was renamed")
	}
	err = Explain(err, part, final)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || !strings.Contains(err.Error(), "held by ") || !strings.Contains(err.Error(), filepath.Base(exe)) {
		t.Fatalf("got %v, want the sharing violation held by %s", err, filepath.Base(exe))
	}
	windows.CloseHandle(h)
	if by := HeldBy(part); slices.Contains(by, filepath.Base(exe)) {
		t.Fatalf("after closing, still held by %v", by)
	}
}

// A file a program lets go of after a moment is renamed, and the log names
// that program.
func TestAFileLetGoIsRenamedAndItsHolderLogged(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "Artist - Title.mp3.part")
	if err := os.WriteFile(part, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(part)
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(600*time.Millisecond, func() { windows.CloseHandle(h) })
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	final := strings.TrimSuffix(part, ".part")
	if err := Rename(part, final, logf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if len(logged) < 2 || !strings.Contains(logged[0], "held by ") || !strings.Contains(logged[0], filepath.Base(exe)) {
		t.Fatalf("logged %q, want refusals naming %s", logged, filepath.Base(exe))
	}
}
