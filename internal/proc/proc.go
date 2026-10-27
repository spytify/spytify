// Package proc starts the programs spytify runs in the background (ffmpeg,
// the capture helper, PowerShell, pw-record, ...) without a window. On
// Windows a console program started by a process with no console, as
// spytifyd is when the app starts it, opens a console window of its own:
// one for each track saved.
package proc

import (
	"context"
	"os/exec"
)

// Command is exec.Command for a program that runs out of sight.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	hide(cmd)
	return cmd
}

// CommandContext is exec.CommandContext for a program that runs out of
// sight.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hide(cmd)
	return cmd
}
