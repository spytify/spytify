package telemetry

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/crash"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
)

// A crash leaves with the vitals its run last wrote, and how long before
// the crash that was; a crash without any leaves as before.
func TestACrashSendsItsVitals(t *testing.T) {
	srv := fakesentry.New(t)
	dir := t.TempDir()
	prefs := &memPrefs{m: map[string]string{}}
	FindCrashes(dir, prefs, time.Now().Add(-time.Minute))
	at := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	for _, p := range []string{"spytify-ui", "spytifyd"} {
		path := filepath.Join(dir, p+".crash.log.1")
		if err := os.WriteFile(path, []byte(strings.Replace(trace, "index out of range", p+" index out of range", 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	vitals := "written_at=" + strconv.FormatInt(at.Add(-42*time.Second).Unix(), 10) + "\ngo.heap_bytes=123456\nsystem.commit_bytes=789\n"
	if err := os.WriteFile(crash.VitalsOf(filepath.Join(dir, "spytify-ui.crash.log.1")), []byte(vitals), 0o600); err != nil {
		t.Fatal(err)
	}
	crashes := FindCrashes(dir, prefs, time.Now())
	if len(crashes) != 2 {
		t.Fatalf("crashes %+v", crashes)
	}
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	for _, cr := range crashes {
		if err := c.SendCrash(cr); err != nil {
			t.Fatal(err)
		}
	}
	for _, it := range srv.Wait(t, "event", 2) {
		ev := it.JSON()
		program := ev["tags"].(map[string]any)["program"]
		got, _ := ev["contexts"].(map[string]any)["vitals"].(map[string]any)
		switch program {
		case "spytify-ui":
			if got["go.heap_bytes"] != "123456" || got["system.commit_bytes"] != "789" || got["age_s"] != "42" {
				t.Errorf("vitals %v", got)
			}
		case "spytifyd":
			if got != nil {
				t.Errorf("a crash without vitals sent %v", got)
			}
		default:
			t.Errorf("event of %v", program)
		}
	}
}
