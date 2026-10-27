//go:build windows

package capture

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/internal/target"
)

const (
	exeSuffix = ".exe"
	buildHint = "build it with scripts/build-windows.sh"
)

// WindowsBuild returns the running Windows build number, such as 22631.
func WindowsBuild() uint32 {
	return windows.RtlGetVersion().BuildNumber
}

// LoopbackSupported reports whether this Windows has process loopback
// capture: Windows 10 version 2004 (build 19041) and later.
func LoopbackSupported(build uint32) bool { return build >= 19041 }

// NewWindows prepares the Windows backend: spytify-tap.exe records the
// player's process tree with WASAPI process loopback and exits with status
// 3 when that process ends, so the next run finds the player again.
func NewWindows(helperPath string, find func(target.Target) (target.Process, bool), logf func(format string, args ...any)) (*Helper, error) {
	helper, err := FindTapHelper(helperPath)
	if err != nil {
		return nil, err
	}
	args := func(t target.Target) ([]string, bool) {
		p, ok := find(t)
		if !ok {
			return nil, false
		}
		return []string{"--pid", strconv.Itoa(p.PID)}, true
	}
	return newHelper(helper, args, logf), nil
}

func stopHelper(p *os.Process) error { return p.Kill() }

// ErrNoSession means the player has not opened an audio session yet: it
// has played nothing since it started.
var ErrNoSession = errors.New("the player has no audio session yet")

// SessionVolume reads the volume of the audio sessions of pid's process
// tree: the player's slider in the Volume Mixer, from 0 to 1.
func SessionVolume(helper string, pid int) (float64, error) {
	out, err := volumeCmd(helper, pid, "--get-volume")
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(out), 64)
}

// SetSessionVolume sets that volume.
func SetSessionVolume(helper string, pid int, v float64) error {
	_, err := volumeCmd(helper, pid, "--set-volume", strconv.FormatFloat(v, 'f', 3, 64))
	return err
}

func volumeCmd(helper string, pid int, args ...string) (string, error) {
	cmd := proc.Command(helper, append([]string{"--pid", strconv.Itoa(pid)}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee) && ee.ExitCode() == 4:
		return "", ErrNoSession
	case err != nil:
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
