package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheLogRotatesAndKeepsThreeOldFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytifyd.log")
	l, err := NewLogger(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Infof("the very first line")
	line := strings.Repeat("x", 1000)
	for i := 0; i < (logKeepFiles+1)*logMaxBytes/len(line)+100; i++ {
		l.Infof("%s", line)
	}
	for i := 0; i <= logKeepFiles; i++ {
		p := path
		if i > 0 {
			p = fmt.Sprintf("%s.%d", path, i)
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		if st.Size() > logMaxBytes+2000 {
			t.Fatalf("%s is %d bytes", filepath.Base(p), st.Size())
		}
		if b, _ := os.ReadFile(p); strings.Contains(string(b), "the very first line") {
			t.Fatalf("the first line is still in %s", filepath.Base(p))
		}
	}
	if _, err := os.Stat(fmt.Sprintf("%s.%d", path, logKeepFiles+1)); err == nil {
		t.Fatalf("more than %d old logs are kept", logKeepFiles)
	}
}

func TestALogThatCannotOpenIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLogger(filepath.Join(file, "spytifyd.log"), nil); err == nil {
		t.Fatal("a log under a file opened")
	}
}

// A log nobody wrote to for 30 days goes, so logs do not stay on the
// computer of someone who stopped using the app.
func TestLogsNotWrittenFor30DaysAreDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytifyd.log")
	old := time.Now().Add(-40 * 24 * time.Hour)
	for _, p := range []string{path, path + ".1", path + ".2", path + ".3"} {
		if err := os.WriteFile(p, []byte("an old line\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if p != path+".3" {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	l, err := NewLogger(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, p := range []string{path + ".1", path + ".2"} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s is still there", filepath.Base(p))
		}
	}
	if _, err := os.Stat(path + ".3"); err != nil {
		t.Fatalf("a recent old log went: %v", err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "an old line") {
		t.Fatal("the current log kept lines from 40 days ago")
	}
	l.Infof("a new line")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "a new line") {
		t.Fatal("the logger does not write after removing the old log")
	}
}
