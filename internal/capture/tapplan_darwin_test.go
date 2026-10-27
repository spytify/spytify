//go:build darwin

package capture

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/pcm"
)

// buildTap compiles spytify-tap for this Mac's architecture with the flags of
// scripts/build-macos.sh.
func buildTap(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("no swiftc")
	}
	arch := map[string]string{"arm64": "arm64", "amd64": "x86_64"}[runtime.GOARCH]
	src := filepath.Join("..", "..", "macos", "spytify-tap")
	out := filepath.Join(t.TempDir(), "spytify-tap")
	cmd := exec.Command("swiftc", "-O", "-target", arch+"-apple-macos14.2",
		"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__info_plist", "-Xlinker", filepath.Join(src, "Info.plist"),
		filepath.Join(src, "main.swift"), "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("swiftc: %v\n%s", err, b)
	}
	return out
}

// tapPlan runs spytify-tap --plan and returns the uid of each device it
// names, "" for none, and its whole output.
func tapPlan(t *testing.T, helper string, args ...string) (map[string]string, string) {
	t.Helper()
	out, err := exec.Command(helper, append([]string{"--plan"}, args...)...).Output()
	if err != nil {
		t.Fatalf("spytify-tap --plan: %v\n%s", err, out)
	}
	uids := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		switch {
		case f[0] == "process":
		case len(f) == 4:
			uids[f[0]] = f[1]
		case len(f) == 2 && f[1] == "-":
			uids[f[0]] = ""
		default:
			t.Fatalf("unexpected --plan line %q", line)
		}
	}
	return uids, string(out)
}

// A recording runs at the rate of the device the player plays to. It ran at
// the rate of the alert sounds' device before: music on the speakers came out
// at 16 kHz while a Bluetooth headset in call mode played the alerts. --plan
// makes no tap, so this needs no System Audio Recording permission.
func TestTapClocksOnThePlayersDevice(t *testing.T) {
	if !TapSupported(MacOSVersion()) {
		t.Skip("macOS older than 14.2")
	}
	afplay, err := exec.LookPath("afplay")
	if err != nil {
		t.Skip("no afplay")
	}
	helper := buildTap(t)

	// The test itself plays nothing: the default output, where the player
	// will most likely play.
	idle, out := tapPlan(t, helper, "--pid", strconv.Itoa(os.Getpid()))
	if idle["default"] == "" {
		t.Skip("this Mac has no default output device")
	}
	if idle["clock"] != idle["default"] {
		t.Errorf("before the player plays, the clock is not the default output:\n%s", out)
	}

	f := pcm.Format{Rate: 48000, Channels: 1, Sample: pcm.S16LE}
	wav := filepath.Join(t.TempDir(), "silence.wav")
	w, err := pcm.CreateWAV(wav, f)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(make([]byte, f.FramesFromMs(20000)*int64(f.FrameBytes())))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	player := exec.Command(afplay, wav)
	player.Stderr = &stderr
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	var waitErr error
	exited := make(chan struct{})
	go func() { waitErr = player.Wait(); close(exited) }()
	t.Cleanup(func() { player.Process.Kill(); <-exited })

	pid := strconv.Itoa(player.Process.Pid)
	deadline := time.Now().Add(10 * time.Second)
	var plan map[string]string
	for {
		plan, out = tapPlan(t, helper, "--pid", pid)
		if plan["playing"] != "" {
			break
		}
		select {
		case <-exited:
			t.Fatalf("afplay ended (%v): %s", waitErr, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("afplay is not seen playing after 10 s:\n%s", out)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("spytify-tap --plan --pid %s:\n%s", pid, out)
	if plan["clock"] != plan["playing"] || plan["clock"] != plan["default"] {
		t.Fatal("the clock is not the default output afplay plays to")
	}
}
