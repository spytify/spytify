//go:build windows

package proc

import (
	"context"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCommandsOpenNoWindow(t *testing.T) {
	for name, cmd := range map[string]*exec.Cmd{
		"Command":        Command("ffmpeg"),
		"CommandContext": CommandContext(context.Background(), "ffmpeg"),
	} {
		if a := cmd.SysProcAttr; a == nil || a.CreationFlags&windows.CREATE_NO_WINDOW == 0 || a.HideWindow {
			t.Errorf("%s: %+v", name, a)
		}
	}
}
