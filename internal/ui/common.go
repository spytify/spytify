package ui

import (
	"errors"
	"fmt"
	"image"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

func showError(w fyne.Window, err error) dialog.Dialog {
	var perr *proto.Error
	if errors.As(err, &perr) {
		err = errors.New(perr.Message)
	}
	return showText(w, lang.Error, capitalized(err.Error()), lang.OK, "", nil)
}

func fmtDuration(ms int64) string {
	if ms <= 0 {
		return "0:00"
	}
	d := time.Duration(ms) * time.Millisecond
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// heading is a section title.
func heading(text string) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = theme.SizeNameSubHeadingText
	return l
}

// caption is secondary text: smaller and grey, but readable, unlike a
// low-importance label, which uses the disabled colour.
func caption(text string) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{Text: text, Style: widget.RichTextStyle{
		ColorName: theme.ColorNamePlaceHolder,
		SizeName:  theme.SizeNameCaptionText,
		Inline:    true,
	}})
	return rt
}

// clipped is a caption that ends in an ellipsis when its space runs out,
// for list rows; a plain caption keeps its full width.
func clipped(text string) *widget.RichText {
	rt := caption(text)
	rt.Truncation = fyne.TextTruncateEllipsis
	return rt
}

// setImage changes an image only when its resource changes: a refresh
// decodes the picture again.
func setImage(img *canvas.Image, res fyne.Resource) {
	if img.Resource != res {
		img.Resource = res
		img.Refresh()
	}
}

// setCover shows a cover's thumbnail, or the music icon while there is none.
func setCover(img *canvas.Image, cover image.Image) {
	if cover == nil {
		setImage(img, theme.MediaMusicIcon())
		return
	}
	if img.Image != cover || img.Resource != nil {
		img.Resource, img.Image = nil, cover
		img.Refresh()
	}
}

// setLabel changes a label only when its text changes.
func setLabel(l *widget.Label, text string) {
	if l.Text != text {
		l.SetText(text)
	}
}

// rowLine is the style of a list row's lines under its title.
var rowLine = widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameCaptionText}

// rowText is a list row's title over its details as one block: two stacked
// widgets would each add their padding between the lines.
func rowText() *widget.RichText {
	rt := widget.NewRichText(
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNameForeground, SizeName: theme.SizeNameText, TextStyle: fyne.TextStyle{Bold: true}}},
		&widget.TextSegment{Style: rowLine},
	)
	rt.Truncation = fyne.TextTruncateEllipsis
	return rt
}

// setRowText shows the lines one under another, each in the style of its
// segment; a line past the text's segments takes the style of the one above.
func setRowText(rt *widget.RichText, lines ...string) {
	changed := len(rt.Segments) != len(lines)
	for i, text := range lines {
		if i == len(rt.Segments) {
			rt.Segments = append(rt.Segments, &widget.TextSegment{Style: rt.Segments[i-1].(*widget.TextSegment).Style})
		}
		if seg := rt.Segments[i].(*widget.TextSegment); seg.Text != text {
			seg.Text, changed = text, true
		}
	}
	if changed {
		rt.Segments = rt.Segments[:len(lines)]
		rt.Refresh()
	}
}

// trackRow is a list row for a track: its cover, its text, then the parts
// after it. The row is as tall as lines lines of text, the first one the
// title's; a track showing fewer has them centred in that height, every row
// of a list being as tall as the others.
func trackRow(lines int, after fyne.CanvasObject) *fyne.Container {
	img := canvas.NewImageFromResource(theme.MediaMusicIcon())
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSquareSize(rowCover))
	text := rowText()
	setRowText(text, make([]string, lines)...)
	return container.NewBorder(nil, nil, img, after, container.NewVBox(layout.NewSpacer(), text, layout.NewSpacer()))
}

// setTrackRow shows a track's cover, and its title over the lines after it,
// one style each so that each line is shortened on its own. An empty line is
// left out.
func setTrackRow(row *fyne.Container, cover image.Image, title string, lines ...string) {
	shown := []string{title}
	for _, line := range lines {
		if line != "" {
			shown = append(shown, line)
		}
	}
	// Border layout: center first, then the leading and trailing parts.
	text := row.Objects[0].(*fyne.Container).Objects[1].(*widget.RichText)
	// A line that the track shown before left out comes back in the style of
	// the lines under the title, not in the title's.
	for len(text.Segments) < len(shown) {
		text.Segments = append(text.Segments, &widget.TextSegment{Style: rowLine})
	}
	setRowText(text, shown...)
	setCover(row.Objects[1].(*canvas.Image), cover)
}

// wrapped is a caption that breaks into lines.
func wrapped(text string) *widget.RichText {
	rt := caption(text)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// emptyState is the centered message of a screen with nothing to show.
func emptyState(title string, body *widget.Label) fyne.CanvasObject {
	body.Alignment = fyne.TextAlignCenter
	body.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(420, 120), container.NewVBox(
		widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}), body)))
}

// setCaption changes a caption's text.
func setCaption(rt *widget.RichText, text string) {
	seg := rt.Segments[0].(*widget.TextSegment)
	if seg.Text != text {
		seg.Text = text
		rt.Refresh()
	}
}

// relayout lays a scroll's content out again after part of it showed or
// hid: Fyne lays a container out only when it is resized or refreshed, so
// what follows would stay where it was and overlap the next section.
func relayout(s *container.Scroll) {
	s.Content.Refresh()
	s.Refresh()
}

// plain sets a part of the window on the plain colour instead of the page.
func plain(content fyne.CanvasObject) fyne.CanvasObject {
	box := newPanel(colorPlain, "", 0, fyne.Size{}, content)
	return container.NewThemeOverride(box, cardTheme{surface: colorPlain})
}

// margin keeps a screen off the window's sides and bottom, as the window's
// own padding would; the window has none, so the bars' plain colour goes to
// its edges.
func margin(o fyne.CanvasObject) *fyne.Container {
	pad := theme.Padding()
	return container.New(layout.NewCustomPaddedLayout(0, pad, pad, pad), o)
}

// inset keeps a part of a screen off the window's sides, in line with the
// cards.
func inset(o fyne.CanvasObject) *fyne.Container {
	pad := theme.Padding()
	return container.New(layout.NewCustomPaddedLayout(pad, pad, pad*2, pad*2), o)
}

// footerBar is a screen's bottom bar, on a grey of its own under a line:
// what the screen holds on the left, its actions on the right.
func footerBar(left, actions fyne.CanvasObject) fyne.CanvasObject {
	pad := theme.Padding()
	row := container.New(layout.NewCustomPaddedLayout(pad/2, pad/2, pad*2, pad),
		container.NewBorder(nil, nil, nil, actions, container.NewVBox(layout.NewSpacer(), left, layout.NewSpacer())))
	return container.NewBorder(widget.NewSeparator(), nil, nil, nil, newPanel(colorFooter, "", 0, fyne.Size{}, row))
}
