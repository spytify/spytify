//go:build darwin

package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spytify/spytify/internal/target"
)

const (
	exeSuffix = ""
	buildHint = "build it with scripts/build-macos.sh"
)

// MacOSVersion returns the running macOS version, such as "14.2.1".
func MacOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return v
}

// TapSupported reports whether this macOS has process taps (14.2+).
func TapSupported(version string) bool {
	var major, minor int
	fmt.Sscanf(version, "%d.%d", &major, &minor)
	return major > 14 || (major == 14 && minor >= 2)
}

// NewDarwin prepares the macOS backend: spytify-tap, a small Swift helper
// that uses a Core Audio process tap (macOS 14.2 and later) and exits when
// the player's processes or the output format change. mute makes the
// player inaudible on the speakers while it is recorded
// (player.mute_output). stateDir keeps the rates the helper sets on the
// player's device, to put them back.
func NewDarwin(helperPath string, mute bool, stateDir string, logf func(format string, args ...any)) (*Helper, error) {
	if v := MacOSVersion(); v != "" && !TapSupported(v) {
		return nil, fmt.Errorf("macOS %s has no process taps; macOS 14.2 or later is needed", v)
	}
	helper, err := FindTapHelper(helperPath)
	if err != nil {
		return nil, err
	}
	state := ""
	if stateDir != "" {
		state = filepath.Join(stateDir, "device-rates.json")
	}
	// The helper finds the player's processes itself and waits for them.
	args := func(t target.Target) ([]string, bool) { return TapArgs(t, mute, state), true }
	h := newHelper(helper, args, logf)
	h.running = func(t target.Target) bool { _, ok := target.FindProcess(t); return ok }
	if state != "" {
		h.restore = []string{"--restore", "--state", state}
	}
	return h, nil
}

// TapArgs are the spytify-tap arguments that select the target. With a
// state file, the helper also sets the player's device to the target's
// rate for the session.
func TapArgs(t target.Target, mute bool, state string) []string {
	var args []string
	if t.BundleID != "" {
		args = append(args, "--bundle-id", t.BundleID)
	}
	for _, n := range t.ProcessNames {
		args = append(args, "--name", n)
	}
	if mute {
		args = append(args, "--mute")
	}
	if t.Rate > 0 && state != "" {
		args = append(args, "--rate", strconv.Itoa(t.Rate), "--state", state)
	}
	return args
}

func stopHelper(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
