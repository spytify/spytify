//go:build darwin

package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
		case f[0] == "process" || f[0] == "rates":
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

// clockRates runs spytify-tap --plan for a process that plays nothing, so
// the clock is the default output, and returns the clock's uid, its rate and
// the rates it offers.
func clockRates(t *testing.T, helper string, args ...string) (uid string, rate int, offers []int) {
	t.Helper()
	uids, out := tapPlan(t, helper, append([]string{"--pid", strconv.Itoa(os.Getpid())}, args...)...)
	listed := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		switch {
		case f[0] == "clock" && len(f) == 4:
			rate, _ = strconv.Atoi(f[2])
		case f[0] == "rates" && len(f) == 2:
			listed = true
			for _, r := range strings.Fields(f[1]) {
				if n, err := strconv.Atoi(r); err == nil {
					offers = append(offers, n)
				}
			}
		}
	}
	if !listed {
		t.Fatalf("spytify-tap --plan lists no rates:\n%s", out)
	}
	return uids["clock"], rate, offers
}

type rateChange struct {
	UID      string  `json:"uid"`
	Own      float64 `json:"own"`
	Set      float64 `json:"set"`
	Released bool    `json:"released"`
}

func rateChanges(t *testing.T, state string) []rateChange {
	t.Helper()
	b, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var c []rateChange
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s: %v\n%s", state, err, b)
	}
	return c
}

func restoreRates(t *testing.T, helper, state string) {
	t.Helper()
	out, err := exec.Command(helper, "--restore", "--state", state).CombinedOutput()
	t.Logf("spytify-tap --restore: %s", out)
	if err != nil {
		t.Fatalf("spytify-tap --restore: %v", err)
	}
	if _, err := os.Stat(state); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("--restore left %s (%v)", state, err)
	}
}

// The player streams at one rate; on a device at another, it resamples
// before the tap sees anything. So a recording sets the player's device to
// the player's rate, and --restore puts the device's own rate back when the
// session ends. The helper starts again during a session: it does not set
// the device twice, and a rate someone else chose in between stays, then and
// after the session. --plan makes no tap, so this needs no System Audio
// Recording permission.
func TestThePlayersDeviceRunsAtThePlayersRate(t *testing.T) {
	if !TapSupported(MacOSVersion()) {
		t.Skip("macOS older than 14.2")
	}
	helper := buildTap(t)
	uid, own, offers := clockRates(t, helper)
	if uid == "" {
		t.Skip("this Mac has no default output device")
	}
	t.Logf("%s runs at %d Hz and offers %v", uid, own, offers)
	// Under 44.1 kHz, a Bluetooth headset goes to call mode.
	var others []int
	for _, r := range offers {
		if r >= 44100 && r != own {
			others = append(others, r)
		}
	}
	if len(others) == 0 {
		t.Skipf("%s offers no other rate to set", uid)
	}
	rate, chosen := others[0], own
	if len(others) > 1 {
		chosen = others[1]
	}
	dir := t.TempDir()
	t.Cleanup(func() {
		clockRates(t, helper, "--rate", strconv.Itoa(own), "--state", filepath.Join(dir, "cleanup.json"))
	})

	state := filepath.Join(dir, "device-rates.json")
	recording := []string{"--rate", strconv.Itoa(rate), "--state", state}
	if _, now, _ := clockRates(t, helper, recording...); now != rate {
		t.Fatalf("a recording at %d Hz left the device at %d Hz", rate, now)
	}
	if got, want := rateChanges(t, state), []rateChange{{UID: uid, Own: float64(own), Set: float64(rate)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("state %+v, want %+v", got, want)
	}
	restoreRates(t, helper, state)
	if _, now, _ := clockRates(t, helper); now != own {
		t.Fatalf("--restore left the device at %d Hz, not its own %d Hz", now, own)
	}

	// Another app, here the helper with a state file of its own, changes
	// the rate during the next session.
	clockRates(t, helper, recording...)
	clockRates(t, helper, "--rate", strconv.Itoa(chosen), "--state", filepath.Join(dir, "other-app.json"))
	if _, now, _ := clockRates(t, helper, recording...); now != chosen {
		t.Fatalf("the helper started again set the device to %d Hz over the %d Hz chosen since", now, chosen)
	}
	if got := rateChanges(t, state); len(got) != 1 || !got[0].Released {
		t.Fatalf("state %+v: the device is not released", got)
	}
	restoreRates(t, helper, state)
	if _, now, _ := clockRates(t, helper); now != chosen {
		t.Fatalf("--restore set the device to %d Hz over the %d Hz chosen since", now, chosen)
	}
}
