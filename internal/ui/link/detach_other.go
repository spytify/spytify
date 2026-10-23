//go:build !unix && !windows

package link

import "os/exec"

const exeSuffix = ""

func detach(*exec.Cmd) {}
