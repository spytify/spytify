//go:build !unix

package link

import "os/exec"

func detach(*exec.Cmd) {}
