package ui

import (
	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/ui/lang"
)

const noWindowHelp = lang.NoWindowLinux

// tell shows a desktop notification: started from its menu entry, the app
// has no terminal to write to.
func tell(fa fyne.App, title, text string) {
	fa.SendNotification(fyne.NewNotification(title, text))
}
