package ui

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

const disclaimer = "For personal use of content you are entitled to listen to; you are responsible for the terms of the services you record."

type aboutScreen struct {
	app      *App
	root     fyne.CanvasObject
	versions *widget.RichText
}

func newAboutScreen(a *App) *aboutScreen {
	s := &aboutScreen{app: a, versions: caption("")}
	logo := canvas.NewImageFromResource(Wordmark)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(300, 98))
	tagline := widget.NewLabelWithStyle("Records any application's audio, splits it into tracks and tags them.", fyne.TextAlignCenter, fyne.TextStyle{})
	tagline.Wrapping = fyne.TextWrapWord
	source := widget.NewHyperlink("Source code on GitHub", nil)
	source.OnTapped = func() { a.openURL(sourceURL) }
	project := newIconButton("Project page", theme.HomeIcon(), func() { a.openURL(siteURL) })
	donate := newLitButton("♥ Donate", nil, widget.HighImportance, func() { a.openURL(donateURL) })
	updates := newIconButton("Check for updates", theme.DownloadIcon(), s.checkNow)
	legal := widget.NewLabelWithStyle(disclaimer, fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	legal.Wrapping = fyne.TextWrapWord
	legal.SizeName = theme.SizeNameCaptionText
	s.root = plain(container.NewCenter(container.NewGridWrap(fyne.NewSize(520, 420), container.NewVBox(
		container.NewCenter(logo),
		tagline,
		container.NewCenter(s.versions),
		container.NewCenter(source),
		container.NewCenter(container.NewHBox(project, updates, donate.lit)),
		layout.NewSpacer(),
		legal,
	))))
	return s
}

func (s *aboutScreen) content() fyne.CanvasObject { return s.root }

func (s *aboutScreen) render(st model.State) {
	core := "not connected"
	if st.HaveInfo {
		core = fmt.Sprintf("%s on %s", st.Info.Version, st.Info.Platform)
	}
	setCaption(s.versions, fmt.Sprintf("App %s · recorder %s · protocol %d", proto.Version, core, proto.Protocol))
}

func (s *aboutScreen) checkNow() {
	s.app.noteAction("check for updates")
	go func() {
		tag, page, err := latestRelease(context.Background())
		fyne.Do(func() {
			switch {
			case err != nil:
				s.app.showError(fmt.Errorf("update check: %w", err))
			case tag != "" && newer(tag, proto.Version):
				ask(s.app.win, "Update available", "Spytify "+tag+" is out. Open the release page?", func(ok bool) {
					if ok {
						s.app.openURL(page)
					}
				})
			default:
				inform(s.app.win, "Up to date", "Spytify "+proto.Version+" is the latest version.")
			}
		})
	}()
}

const firstRunKey = "first-run-seen"

// maybeShowFirstRun explains, once, what the recording's quality is and
// whose responsibility the recording is, and shows the Sentry bug
// tracker's switch, ticked. then runs once it is closed.
func (a *App) maybeShowFirstRun(then func()) {
	prefs := a.fa.Preferences()
	if prefs.Bool(firstRunKey) {
		a.askPrivacy(then)
		return
	}
	body := widget.NewLabel("Spytify records the audio the player sends to your speakers. " +
		"Streaming services send compressed audio, so a FLAC or WAV file avoids a second lossy pass but adds no quality.\n\n" + disclaimer)
	body.Wrapping = fyne.TextWrapWord
	again := newCheck("Don't show this again")
	again.SetChecked(true)
	box := container.NewVBox(body)
	var tracker *check
	if a.tel.Available() {
		tracker = a.trackerCheck()
		box.Add(tracker)
	}
	box.Add(again)
	showDialog(a.win, "Before you record", box, "OK", "", fyne.NewSize(460, 300), func(bool) {
		if again.Checked {
			prefs.SetBool(firstRunKey, true)
		}
		if tracker != nil {
			prefs.SetBool(privacySeenKey, true)
			a.setPrivacy(trackerKey, tracker.Checked)
		}
		then()
	})
}
