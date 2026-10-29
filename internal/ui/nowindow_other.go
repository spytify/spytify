//go:build !windows && !linux

package ui

import "fyne.io/fyne/v2"

// noWindowHelp and tell are for Windows and Linux: on macOS opengl.Check
// finds nothing to tell.
const noWindowHelp = ""

func tell(fyne.App, string, string) {}
