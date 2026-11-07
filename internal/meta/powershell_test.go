package meta

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func notInPath(string) (string, error) {
	return "", &exec.Error{Name: "powershell.exe", Err: exec.ErrNotFound}
}

// A PATH without Windows PowerShell's folder still finds it in the system
// folder.
func TestPowerShellIsFoundInTheSystemFolderWhenPATHLacksIt(t *testing.T) {
	system := t.TempDir()
	dir := filepath.Join(system, "WindowsPowerShell", "v1.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "powershell.exe")
	if err := os.WriteFile(want, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findPowerShell(system, notInPath)
	if err != nil || got != want {
		t.Fatalf("findPowerShell = %q, %v; want %q", got, err, want)
	}
}

func TestPowerShellFallsBackToPATH(t *testing.T) {
	got, err := findPowerShell(t.TempDir(), func(string) (string, error) { return `D:\ps\powershell.exe`, nil })
	if err != nil || got != `D:\ps\powershell.exe` {
		t.Fatalf("findPowerShell = %q, %v", got, err)
	}
}

// The error says both places it looked.
func TestPowerShellMissingSaysWhereItLooked(t *testing.T) {
	system := t.TempDir()
	_, err := findPowerShell(system, notInPath)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want exec.ErrNotFound", err)
	}
	if dir := filepath.Join(system, "WindowsPowerShell", "v1.0"); !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("err = %q, want %s and PATH named", err, dir)
	}
}
