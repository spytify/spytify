package ui

import (
	"fmt"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

const disclaimer = "For personal use of content you are entitled to listen to; you are responsible for the terms of the services you record."

type aboutScreen struct {
	app      *App
	root     fyne.CanvasObject
	versions *widget.Label
	target   *widget.Label
	checks   *fyne.Container
}

func newAboutScreen(a *App) *aboutScreen {
	s := &aboutScreen{app: a, versions: widget.NewLabel(""), target: widget.NewLabel(""), checks: container.NewVBox()}
	logo := canvas.NewImageFromResource(Logo)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(64, 64))
	project, _ := url.Parse("https://github.com/spytify/spytify")
	tagline := widget.NewLabel("Records any application's audio, splits it into tracks and tags them.")
	tagline.Wrapping = fyne.TextWrapWord
	legal := muted(disclaimer)
	legal.Wrapping = fyne.TextWrapWord
	s.root = container.NewVScroll(container.NewVBox(
		container.NewBorder(nil, nil, logo, nil, container.NewVBox(widget.NewLabelWithStyle("Spytify", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), tagline)),
		s.versions, s.target,
		widget.NewCard("Checks", "", s.checks),
		widget.NewHyperlink("github.com/spytify/spytify", project),
		legal,
	))
	return s
}

func (s *aboutScreen) content() fyne.CanvasObject { return s.root }

func (s *aboutScreen) render(st model.State) {
	core := "not connected"
	if st.HaveInfo {
		core = fmt.Sprintf("%s (%s)", st.Info.Version, st.Info.Platform)
	}
	s.versions.SetText(fmt.Sprintf("UI %s · core %s · protocol %d", proto.Version, core, proto.Protocol))
	t := st.Info.Target
	text := "Target process: " + t.Process
	if t.BundleID != "" {
		text += " · bundle " + t.BundleID
	}
	if t.MPRIS != "" {
		text += " · MPRIS " + t.MPRIS
	}
	s.target.SetText(text)
	var rows []fyne.CanvasObject
	for _, c := range st.Info.Checks {
		icon := theme.ConfirmIcon()
		if !c.OK {
			icon = theme.ErrorIcon()
		}
		detail := muted(c.Detail)
		detail.Wrapping = fyne.TextWrapWord
		rows = append(rows, container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(icon), widget.NewLabel(c.Name)), nil, detail))
	}
	s.checks.Objects = rows
	s.checks.Refresh()
}

const firstRunKey = "first-run-seen"

// maybeShowFirstRun explains, once, what the recording's quality is and
// whose responsibility the recording is.
func (a *App) maybeShowFirstRun() {
	prefs := a.fa.Preferences()
	if prefs.Bool(firstRunKey) {
		return
	}
	body := widget.NewLabel("Spytify records the audio the player sends to your speakers. " +
		"Streaming services send compressed audio, so a FLAC or WAV file avoids a second lossy pass but adds no quality.\n\n" + disclaimer)
	body.Wrapping = fyne.TextWrapWord
	again := widget.NewCheck("Don't show this again", nil)
	again.SetChecked(true)
	d := dialog.NewCustom("Before you record", "OK", container.NewVBox(body, again), a.win)
	d.SetOnClosed(func() {
		if again.Checked {
			prefs.SetBool(firstRunKey, true)
		}
	})
	d.Resize(fyne.NewSize(460, 260))
	d.Show()
}
