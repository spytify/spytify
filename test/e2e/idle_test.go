package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/openfiles"
)

// heldBy fails the test when process pid holds anything open under dir.
func heldBy(t *testing.T, what string, pid int, dir string) {
	t.Helper()
	held, err := openfiles.Held(pid, dir)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if len(held) > 0 {
		t.Errorf("%s: spytifyd holds the output folder open: %v", what, held)
	}
}

// A running spytifyd that is not recording holds nothing open in the
// output folder, so a USB drive it saves to can be ejected: before its
// first recording and after one.
func TestTheIdleRecorderHoldsNothingInTheOutputFolder(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.out, 0o755); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	d := exec.Command(exe("spytifyd"), e.daemonArgs("basic")...)
	d.Env, d.Stdout, d.Stderr = e.vars, logs, logs
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { d.Process.Kill(); d.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := e.cli("info"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("spytifyd did not answer; log:\n%s", logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	heldBy(t, "before recording", d.Process.Pid, e.out)

	e.mustCLI("start")
	for {
		status, _ := e.cli("status")
		if len(fake.AudioFiles(e.out)) == 3 && strings.Contains(status, "idle") {
			break
		}
		if time.Now().After(deadline.Add(30 * time.Second)) {
			t.Fatalf("files %v, status %q; log:\n%s", fake.AudioFiles(e.out), status, logs)
		}
		time.Sleep(200 * time.Millisecond)
	}
	heldBy(t, "after recording", d.Process.Pid, e.out)
	e.cli("shutdown")
}

// With app.autostart_recording, spytifyd starts recording at launch; with
// recorder.single_track too, it stops after one track and keeps running.
func TestAutostartRecordsOnLaunch(t *testing.T) {
	e := newEnv(t)
	logs := &bytes.Buffer{}
	d := exec.Command(exe("spytifyd"), e.daemonArgs("basic", "--set", "app.autostart_recording=true", "--set", "recorder.single_track=true")...)
	d.Env, d.Stdout, d.Stderr = e.vars, logs, logs
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { d.Process.Kill(); d.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		files := fake.AudioFiles(e.out)
		status, _ := e.cli("status")
		if len(files) == 1 && strings.Contains(status, "idle") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("files %v, status %q; log:\n%s", files, status, logs)
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	if files := fake.AudioFiles(e.out); len(files) != 1 {
		t.Fatalf("recorded %v, want one track", files)
	}
	if _, err := e.cli("info"); err != nil {
		t.Fatalf("spytifyd stopped after the track: %v", err)
	}
	e.cli("shutdown")
}
