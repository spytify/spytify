package ui

import (
	"errors"
	"fmt"
	"os"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/opengl"
)

// NoWindow says why the app cannot open its window, err being what
// opengl.Check found, and reports it. With no window, the bug tracker
// choice cannot be shown: it sends only when it was shown at an earlier
// launch and left on.
func NoWindow(fa fyne.App, tel *telemetry.Client, privacy map[string]any, err error) {
	on, _ := privacy[trackerKey].(bool)
	tel.SetEnabled(on && fa.Preferences().Bool(privacySeenKey))
	reason, text := "no_opengl", lang.NoWindow+"\n\n"+noWindowHelp
	if errors.Is(err, opengl.ErrNoDisplay) {
		reason, text = "no_display", lang.NoDisplay
	}
	tel.Problem(reason, err.Error())
	fmt.Fprintln(os.Stderr, "spytify:", err)
	fmt.Fprintln(os.Stderr, text)
	tell(fa, lang.Spytify, fmt.Sprintf(lang.WithError, text, err.Error()))
}
