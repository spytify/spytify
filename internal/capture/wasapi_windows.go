//go:build windows

package capture

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"

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

// hideWindow keeps the helper from opening a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
