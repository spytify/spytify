package osopen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A file name can hold &, which Windows allows and which comes with track
// titles. Opening the file runs nothing named after what follows it: pwn.cmd,
// first in PATH, would leave a marker. The test opens a text file in Notepad,
// so it runs on CI only, or with SPYTIFY_REAL_OPEN set.
func TestOpenRunsNoCommandFromTheFileName(t *testing.T) {
	if os.Getenv("CI") == "" && os.Getenv("SPYTIFY_REAL_OPEN") == "" {
		t.Skip("opens a file in Notepad: set SPYTIFY_REAL_OPEN=1 to run it")
	}
	dir := t.TempDir()
	if strings.ContainsAny(dir, " \t") {
		t.Skipf("%s holds a space: Go quotes such a path, and cmd reads & in quotes as text", dir)
	}
	marker := filepath.Join(dir, "marker")
	write(t, filepath.Join(dir, "pwn.cmd"), "@type nul > \""+marker+"\"\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, "x&pwn&.txt")
	write(t, path, "Spytify open test\r\n")

	if err := Open(path); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("opening %s ran pwn.cmd", path)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
