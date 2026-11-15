//go:build windows

package link

import (
	"os/exec"
	"syscall"
)

const exeSuffix = ".exe"

// detach starts spytifyd in a process group of its own, so it outlives the
// UI. DETACHED_PROCESS gives it no console; it opens no window, so no
// hidden-window flag either.
func detach(cmd *exec.Cmd) {
	const createNewProcessGroup, detachedProcess = 0x00000200, 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
