//go:build !windows

package encode

import "os/exec"

func hideConsole(*exec.Cmd) {}
