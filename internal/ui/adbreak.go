package ui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
)

// The ads the player plays one after the other, between two tracks, are
// one line of the session list: a pill, "Advertisements 1:09", that opens
// to a slim line per ad, under the covers' column.

type itemKind int

const (
	itemTrack itemKind = iota
	itemBreak
	itemAd
)

// sessionItem is a line of the session list: a track, an ad break's pill,
// or one of its ads while the pill is open.
type sessionItem struct {
	kind itemKind
	// row is the track, or the ad.
	row model.Row
	// brk is the break's key, the ID of its first ad; n an ad's number in
	// it, from 1 for the first one played.
	brk string
	n   int
	// length is the ad's, or on the pill the break's.
	length int64
}

// sessionItems is the list of rows, newest first: each run of ads is one
// pill, followed while open has its key by its ads, newest first as well.
func sessionItems(rows []model.Row, open map[string]bool) []sessionItem {
	var items []sessionItem
	for i := 0; i < len(rows); {
		if !rows[i].IsAd() {
			items = append(items, sessionItem{kind: itemTrack, row: rows[i]})
			i++
			continue
		}
		j := i
		for j < len(rows) && rows[j].IsAd() {
			j++
		}
		run := rows[i:j]
		pill := sessionItem{kind: itemBreak, brk: run[len(run)-1].ID}
		for _, ad := range run {
			pill.length += rowLength(ad)
		}
		items = append(items, pill)
		if open[pill.brk] {
			for k, ad := range run {
				items = append(items, sessionItem{kind: itemAd, row: ad, brk: pill.brk, n: len(run) - k, length: rowLength(ad)})
			}
		}
		i = j
	}
	return items
}

// adName is an ad's line: its number in the break, then its title, unless
// the player gave it none or only a dash.
func adName(n int, title string) string {
	if strings.Trim(title, " -–—") == "" {
		return fmt.Sprintf(lang.AdNumber, n)
	}
	return fmt.Sprintf(lang.AdTitled, n, title)
}

// adText is an ad's name and length, in the break's text colour.
var adText = widget.RichTextStyle{ColorName: colorAdText, SizeName: theme.SizeNameText, Inline: true}

// newAdLine is an ad's row: the ad icon in the covers' column, its name,
// and its length at the end.
func newAdLine() *fyne.Container {
	// The icon takes the break's text colour from a theme: a coloured
	// resource kept the first theme's colour once its row was reused.
	icon := container.NewGridWrap(fyne.NewSize(rowCover, theme.IconInlineSize()),
		container.NewThemeOverride(widget.NewIcon(adIcon), adTheme{cardTheme: cardTheme{input: colorCardInput, surface: colorCard}, ink: colorAdText}))
	name := widget.NewRichText(&widget.TextSegment{Style: adText})
	name.Truncation = fyne.TextTruncateEllipsis
	length := widget.NewRichText(&widget.TextSegment{Style: adText})
	return container.NewBorder(nil, nil, container.NewCenter(icon), length, name)
}

// setAdLine shows an ad's name and length.
func setAdLine(row *fyne.Container, name, length string) {
	// Border layout: center first, then the leading and trailing parts.
	setCaption(row.Objects[0].(*widget.RichText), name)
	setCaption(row.Objects[2].(*widget.RichText), length)
}

// newAdBreak is an ad break's row: its pill in the middle, a line on each
// side, as the gap the ads left between two tracks. The pill is the row's
// button, which Space presses on the row, the ad icon at its start and the
// arrow that opens or closes it at its end.
func newAdBreak() *fyne.Container {
	pill := newRowButton("", adIcon)
	pill.Importance = widget.MediumImportance
	pill.Alignment = widget.ButtonAlignLeading
	pill.main = true
	arrow := widget.NewIcon(theme.MenuDropDownIcon())
	card := cardTheme{input: colorCardInput, surface: colorCard}
	rule := func() fyne.CanvasObject {
		return container.NewThemeOverride(widget.NewSeparator(), adTheme{cardTheme: card, ink: colorAdText, rule: colorAdRule})
	}
	return container.New(adBreakLayout{}, rule(), rule(),
		container.NewThemeOverride(container.New(pillLayout{}, pill, arrow), adTheme{cardTheme: card, ink: colorOnAdPill, fill: colorAdPill}))
}

// setAdBreak shows a break's pill: its text, its arrow, down while closed,
// and what a press does.
func setAdBreak(row *fyne.Container, text string, open bool, toggle func()) {
	pill := row.Objects[2].(*container.ThemeOverride).Content.(*fyne.Container)
	b, arrow := pill.Objects[0].(*rowButton), pill.Objects[1].(*widget.Icon)
	b.OnTapped = toggle
	if b.Text != text {
		b.SetText(text)
	}
	icon := theme.MenuDropDownIcon()
	if open {
		icon = theme.MenuDropUpIcon()
	}
	if arrow.Resource != icon {
		arrow.SetResource(icon)
	}
}

// adBreakLayout puts the pill in the middle of the row, and a line from
// each side up to it.
type adBreakLayout struct{}

func (adBreakLayout) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	left, right, pill := objs[0], objs[1], objs[2]
	m := pill.MinSize()
	x := (s.Width - m.Width) / 2
	pill.Resize(m)
	pill.Move(fyne.NewPos(x, (s.Height-m.Height)/2))
	gap, thick := 2*theme.Padding(), left.MinSize().Height
	y := (s.Height - thick) / 2
	left.Resize(fyne.NewSize(max(0, x-gap), thick))
	left.Move(fyne.NewPos(0, y))
	right.Resize(fyne.NewSize(max(0, s.Width-x-m.Width-gap), thick))
	right.Move(fyne.NewPos(x+m.Width+gap, y))
}

func (adBreakLayout) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[2].MinSize() }

// pillLayout makes the pill's button wider by its arrow, which sits inside
// its end: a press there is the button's.
type pillLayout struct{}

func (pillLayout) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	objs[0].Resize(s)
	objs[0].Move(fyne.NewPos(0, 0))
	a := theme.IconInlineSize()
	objs[1].Resize(fyne.NewSquareSize(a))
	objs[1].Move(fyne.NewPos(s.Width-a-2*theme.Padding(), (s.Height-a)/2))
}

func (pillLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return objs[0].MinSize().AddWidthHeight(theme.IconInlineSize()+theme.Padding(), 0)
}

// adTheme is the card's theme on an ad break: its text and icons in ink,
// its lines in rule when set; on the pill, which sets fill, a button of
// that colour, lower than the rows' buttons and rounded at its ends.
type adTheme struct {
	cardTheme
	ink, rule, fill fyne.ThemeColorName
}

func (p adTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch {
	case name == theme.ColorNameButton && p.fill != "":
		name = p.fill
	case name == theme.ColorNameSeparator && p.rule != "":
		name = p.rule
	case name == theme.ColorNameForeground:
		name = p.ink
	}
	return p.cardTheme.Color(name, v)
}

func (p adTheme) Size(n fyne.ThemeSizeName) float32 {
	s := p.cardTheme.Size(n)
	if p.fill == "" {
		return s
	}
	switch n {
	case theme.SizeNameInnerPadding:
		return s / 2
	case theme.SizeNameInputRadius:
		// Half the pill's height: the icon and the padding around it.
		return p.cardTheme.Size(theme.SizeNameInlineIcon)/2 + p.cardTheme.Size(theme.SizeNameInnerPadding)/2
	}
	return s
}
