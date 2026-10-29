package ui

import (
	"fyne.io/fyne/v2"
	"golang.org/x/sys/windows"
)

const noWindowHelp = "This happens when the graphics card is disabled or has no driver (Windows then uses its basic display driver), and in some virtual machines. " +
	"Turn the card back on in Device Manager, or install its driver, then open Spytify again."

// tell shows a message box, which Windows draws without OpenGL.
func tell(_ fyne.App, title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}
