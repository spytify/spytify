//go:build !windows

package meta

import "os/exec"

func hideConsole(*exec.Cmd) {}
