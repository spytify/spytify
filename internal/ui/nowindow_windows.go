package ui

import (
	"fyne.io/fyne/v2"
	"golang.org/x/sys/windows"

	"github.com/spytify/spytify/internal/ui/lang"
)

const noWindowHelp = lang.NoWindowWindows

// tell shows a message box, which Windows draws without OpenGL.
func tell(_ fyne.App, title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}
