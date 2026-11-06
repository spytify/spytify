package crash

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The vitals file holds this system's counts, numbers only, and reads
// back as written.
func TestVitalsAreWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytify-ui.crash.log")
	now := time.Now()
	if err := writeVitals(VitalsOf(path), now.Add(-90*time.Second), now, nil); err != nil {
		t.Fatal(err)
	}
	keys := []string{"written_at", "uptime_s", "go.total_bytes", "go.heap_bytes", "go.goroutines", "go.gc_cycles"}
	switch runtime.GOOS {
	case "windows":
		keys = append(keys, "process.private_bytes", "process.working_set_bytes", "process.handles", "process.threads",
			"process.gdi_objects", "process.user_objects", "system.commit_bytes", "system.commit_limit_bytes",
			"system.physical_bytes", "system.physical_available_bytes", "system.handles")
	case "linux":
		keys = append(keys, "process.rss_bytes", "process.threads", "process.fds", "system.physical_bytes",
			"system.physical_available_bytes", "system.commit_limit_bytes", "system.commit_bytes")
	case "darwin":
		keys = append(keys, "process.max_rss_bytes", "system.physical_bytes")
	}
	got := ReadVitals(path)
	if len(got) != len(keys) {
		t.Errorf("vitals %v, want %v", got, keys)
	}
	for _, k := range keys {
		if _, err := strconv.ParseUint(got[k], 10, 64); err != nil {
			t.Errorf("%s = %q", k, got[k])
		}
	}
	if got["written_at"] != strconv.FormatInt(now.Unix(), 10) || got["uptime_s"] != "90" {
		t.Errorf("written at %s after %s s", got["written_at"], got["uptime_s"])
	}
	for _, k := range []string{"go.total_bytes", "go.goroutines", "system.physical_bytes"} {
		if got[k] == "0" {
			t.Errorf("%s is 0", k)
		}
	}
	if _, err := os.Stat(VitalsOf(path) + ".tmp"); err == nil {
		t.Error("the file written first is still there")
	}
}

// The program's own counts follow the system's; one that would not read
// back as one line is left out.
func TestVitalsTakeTheProgramsCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytify-ui.crash.log")
	extra := func() map[string]string {
		return map[string]string{"ui.rows": "48213", "ui.logs": "1000", "ui.bad=key": "1", "ui.bad_value": "1\n2"}
	}
	if err := writeVitals(VitalsOf(path), time.Now(), time.Now(), extra); err != nil {
		t.Fatal(err)
	}
	got := ReadVitals(path)
	if got["ui.rows"] != "48213" || got["ui.logs"] != "1000" || got["go.goroutines"] == "" {
		t.Errorf("vitals %v", got)
	}
	for k := range got {
		if strings.HasPrefix(k, "ui.bad") {
			t.Errorf("%s was written", k)
		}
	}
}

// A crash's vitals move with its trace, and go with it after 30 days; a
// clean run's go at the next launch.
func TestKeepMovesTheVitalsWithTheTrace(t *testing.T) {
	path := filepath.Join(crashDir(t), "spytifyd.crash.log")
	write := func(file, text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	Keep(path)
	write(VitalsOf(path), "written_at=1\n")
	Keep(path)
	if _, err := os.Stat(VitalsOf(path)); err == nil {
		t.Error("a clean run's vitals stayed")
	}

	write(VitalsOf(path), "written_at=2\n")
	write(path, "panic: boom\n")
	Keep(path)
	if got := ReadVitals(path + ".1"); got["written_at"] != "2" {
		t.Fatalf("vitals of the crash %v", got)
	}

	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(path+".1", old, old); err != nil {
		t.Fatal(err)
	}
	Keep(path)
	if _, err := os.Stat(VitalsOf(path + ".1")); err == nil {
		t.Error("a 40-day-old crash's vitals are still there")
	}
}
