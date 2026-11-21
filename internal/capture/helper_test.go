//go:build darwin || windows

package capture

import (
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"testing"
)

// The output device's name, which the user chose, leaves the helper's
// stderr lines kept for its health; the rest of each line stays.
func TestTheHelpersLinesLoseTheDevicesName(t *testing.T) {
	for line, want := range map[string]string{
		"recording 4242 at 48000 Hz, the mix rate of Speakers (Jo's Headset)":                        "recording 4242 at 48000 Hz, the mix rate of a device",
		"recording 4242 at 48000 Hz, the mix rate of Speakers (Jo's Headset), the default output":    "recording 4242 at 48000 Hz, the mix rate of a device, the default output",
		"Speakers (Jo's Headset) now mixes at 44100 Hz":                                              "a device now mixes at 44100 Hz",
		"tapping player[101], player Helper[102] on Jo's AirPods at 48000 Hz (tap reports 48000 Hz)": "tapping player[101], player Helper[102] on a device at 48000 Hz (tap reports 48000 Hz)",
		"process 4242 has no audio session yet":                                                      "process 4242 has no audio session yet",
		"recording 4242 at 48000 Hz: no output device gives its mix rate (0x88890004)":               "recording 4242 at 48000 Hz: no output device gives its mix rate (0x88890004)",
	} {
		if got := deviceless(line); got != want {
			t.Errorf("deviceless(%q) = %q, want %q", line, got, want)
		}
	}
}

// The Windows helper says the player's Volume Mixer slider as "volume
// 0.850"; another line, or a value out of 0..1, is not one.
func TestTheHelpersVolumeLine(t *testing.T) {
	for line, want := range map[string]float64{"volume 0.850": 0.85, "volume 0.000": 0, "volume 1.000": 1} {
		if got, ok := volumeLine(line); !ok || got != want {
			t.Errorf("volumeLine(%q) = %v, %v; want %v", line, got, ok, want)
		}
	}
	for _, line := range []string{"volume", "volume loud", "volume 1.500", "volume -0.100", "process 4242 plays to its output"} {
		if v, ok := volumeLine(line); ok {
			t.Errorf("volumeLine(%q) = %v, want no volume", line, v)
		}
	}
}

// Why the helper could not start keeps the system's error and drops the
// helper's path, which names the user's folders.
func TestTheHelpersStartErrorLosesItsPath(t *testing.T) {
	const helper = `C:\Users\Jo\AppData\Local\Spytify\spytify-tap.exe`
	for _, c := range []struct {
		err  error
		want string
	}{
		{&fs.PathError{Op: "fork/exec", Path: helper, Err: errors.New("Access is denied.")}, "fork/exec: Access is denied."},
		{&exec.Error{Name: helper, Err: &fs.PathError{Op: "stat", Path: helper, Err: fs.ErrNotExist}}, "stat: file does not exist"},
		{&exec.Error{Name: helper, Err: exec.ErrNotFound}, "executable file not found in $PATH"},
		{errors.New("could not run " + helper + ": blocked"), "could not run <helper>: blocked"},
	} {
		if got := startError(c.err, helper); got != c.want {
			t.Errorf("startError(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if got := startError(errors.New(strings.Repeat("x", 2*stderrBytes)), helper); len(got) != stderrBytes {
		t.Errorf("a long start error kept %d bytes, want %d", len(got), stderrBytes)
	}
}
