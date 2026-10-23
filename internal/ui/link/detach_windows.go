//go:build windows

package link

import (
	"os/exec"
	"syscall"
)

const exeSuffix = ".exe"

// detach starts spytifyd in a process group of its own and without a
// console window, so it outlives the UI.
func detach(cmd *exec.Cmd) {
	const createNewProcessGroup, detachedProcess, createNoWindow = 0x00000200, 0x00000008, 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNewProcessGroup | detachedProcess | createNoWindow}
}
