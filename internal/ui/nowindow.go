package ui

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/telemetry"
)

const noWindowText = "Spytify can't open its window. It draws it with OpenGL, and no graphics driver on this computer offers it.\n\n" +
	"This happens when the graphics card is disabled or has no driver (Windows then uses its basic display driver), and in some virtual machines. " +
	"Turn the card back on in Device Manager, or install its driver, then open Spytify again."

// NoWindow says why the app cannot open its window, err being what
// opengl.Check found, and reports it. With no window, the bug tracker
// choice cannot be shown: it sends only when it was shown at an earlier
// launch and left on.
func NoWindow(fa fyne.App, tel *telemetry.Client, privacy map[string]any, err error) {
	on, _ := privacy[trackerKey].(bool)
	tel.SetEnabled(on && fa.Preferences().Bool(privacySeenKey))
	tel.Problem("no_opengl", err.Error())
	fmt.Fprintln(os.Stderr, "spytify:", err)
	tell("Spytify", noWindowText+"\n\n("+err.Error()+")")
}
