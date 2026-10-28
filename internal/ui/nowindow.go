package ui

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/telemetry"
)

// noWindowText is followed by noWindowHelp, what to do on this system.
const noWindowText = "Spytify can't open its window. It draws it with OpenGL, and no graphics driver on this computer offers it."

// NoWindow says why the app cannot open its window, err being what
// opengl.Check found, and reports it. With no window, the bug tracker
// choice cannot be shown: it sends only when it was shown at an earlier
// launch and left on.
func NoWindow(fa fyne.App, tel *telemetry.Client, privacy map[string]any, err error) {
	on, _ := privacy[trackerKey].(bool)
	tel.SetEnabled(on && fa.Preferences().Bool(privacySeenKey))
	tel.Problem("no_opengl", err.Error())
	text := noWindowText + "\n\n" + noWindowHelp
	fmt.Fprintln(os.Stderr, "spytify:", err)
	fmt.Fprintln(os.Stderr, text)
	tell(fa, "Spytify", text+"\n\n("+err.Error()+")")
}
