//go:build windows

package encode

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps ffmpeg from flashing a console window over the app.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
