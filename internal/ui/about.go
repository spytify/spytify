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

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

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
	tagline := widget.NewLabelWithStyle(lang.Tagline, fyne.TextAlignCenter, fyne.TextStyle{})
	tagline.Wrapping = fyne.TextWrapWord
	source := widget.NewHyperlink(lang.SourceCode, nil)
	source.OnTapped = func() { a.openURL(sourceURL) }
	project := newIconButton(lang.ProjectPage, theme.HomeIcon(), func() { a.openURL(siteURL) })
	donate := newLitButton(lang.DonateHeart, nil, widget.HighImportance, func() { a.openURL(donateURL) })
	updates := newIconButton(lang.CheckForUpdates, theme.DownloadIcon(), s.checkNow)
	legal := widget.NewLabelWithStyle(lang.Disclaimer, fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
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
	core := lang.RecorderNotConnected
	if st.HaveInfo {
		core = fmt.Sprintf(lang.RecorderOn, st.Info.Version, st.Info.Platform)
	}
	setCaption(s.versions, fmt.Sprintf(lang.Versions, proto.Version, core, proto.Protocol))
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
				ask(s.app.win, lang.UpdateAvailable, fmt.Sprintf(lang.UpdateAvailableText, tag), func(ok bool) {
					if ok {
						s.app.openURL(page)
					}
				})
			default:
				inform(s.app.win, lang.UpToDate, fmt.Sprintf(lang.UpToDateText, proto.Version))
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
	body := widget.NewLabel(lang.FirstRunText + "\n\n" + lang.Disclaimer)
	body.Wrapping = fyne.TextWrapWord
	again := newCheck(lang.DontShowAgain)
	again.SetChecked(true)
	box := container.NewVBox(body)
	var tracker *check
	if a.tel.Available() {
		tracker = a.trackerCheck()
		box.Add(tracker)
	}
	box.Add(again)
	showDialog(a.win, lang.BeforeYouRecord, box, lang.OK, "", fyne.NewSize(460, 300), func(bool) {
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
