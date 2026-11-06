//go:build windows

package meta

import (
	"os"
	"testing"
)

// The check failed on a PC whose PATH had lost Windows PowerShell's folder:
// exec: "powershell.exe": executable file not found in %PATH%.
func TestPowerShellIsFoundWithAnEmptyPATH(t *testing.T) {
	t.Setenv("PATH", "")
	p, err := PowerShell()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
