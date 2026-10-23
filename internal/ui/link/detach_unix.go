//go:build unix

package link

import (
	"os/exec"
	"syscall"
)

const exeSuffix = ""

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
