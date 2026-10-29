package ui

import "fyne.io/fyne/v2"

const noWindowHelp = "Install Mesa's OpenGL drivers, which draw with the graphics card or, without one, with the processor, then open Spytify again:\n" +
	"Debian, Ubuntu: sudo apt install libgl1-mesa-dri libglx-mesa0 libegl-mesa0\n" +
	"Fedora: sudo dnf install mesa-dri-drivers mesa-libGL mesa-libEGL"

// tell shows a desktop notification: started from its menu entry, the app
// has no terminal to write to.
func tell(fa fyne.App, title, text string) {
	fa.SendNotification(fyne.NewNotification(title, text))
}
