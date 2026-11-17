//go:build windows

package core

import "github.com/spytify/spytify/internal/osopen"

// openApp starts the player by its name through the shell, with no
// cmd.exe: a windowless recorder starting a hidden shell is what behaviour
// shields score (#829). Tests replace it.
var openApp = osopen.App
