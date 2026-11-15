//go:build darwin && cgo

package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisbrodbeck/machineid"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
)

// IOKit gives the IOPlatformUUID ioreg prints: on a Mac, the ID sent is
// the very one earlier versions sent.
func TestIOKitReadsWhatIoregPrints(t *testing.T) {
	want, err := machineid.ProtectedID("spytify")
	if err != nil {
		t.Fatalf("ioreg: %v", err)
	}
	raw, err := systemMachineID()
	if err != nil {
		t.Fatalf("IOKit: %v", err)
	}
	if got := protect(raw); got != want {
		t.Errorf("IOKit's ID hashes to %s, ioreg's to %s", got, want)
	}
}

// With the bug tracker on, the computer's ID is read and sent without
// starting ioreg, which security software takes for system discovery: an
// ioreg first on PATH tells if it runs.
func TestTheComputerIDStartsNoIoreg(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	shim := "#!/bin/sh\necho \"$@\" >> '" + ran + "'\nexec /usr/sbin/ioreg \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ioreg"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	forget := func() {
		computer.mu.Lock()
		computer.read, computer.id = false, ""
		computer.mu.Unlock()
	}
	forget()
	t.Cleanup(forget)

	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	c.Flush(5 * time.Second)
	tx := srv.Wait(t, "transaction", 1)[0].JSON()
	if user, _ := tx["user"].(map[string]any); user["id"] == nil || user["id"] == "" {
		t.Errorf("user %v, want the computer's hash", user)
	}
	if b, err := os.ReadFile(ran); err == nil {
		t.Errorf("ioreg ran: %s", b)
	}
}
