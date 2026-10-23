//go:build windows

package meta

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps PowerShell from opening a console window for the poller.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
