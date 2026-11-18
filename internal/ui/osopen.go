package ui

import (
	"github.com/spytify/spytify/internal/osopen"
	"github.com/spytify/spytify/internal/trash"
)

// moveToTrash moves a file to the system's trash. Tests replace it.
var moveToTrash = trash.Move

// reveal shows a file in the OS file manager, selected where possible.
// Tests replace it.
var reveal = osopen.Reveal

// openPath opens a file or folder with its default application. Tests
// replace it.
var openPath = osopen.Open

// openLink opens a web page in the default browser, through the shell as
// openPath does a file. Fyne's OpenURL starts rundll32 on Windows, a
// program security software watches. Tests replace it.
var openLink = osopen.Open
