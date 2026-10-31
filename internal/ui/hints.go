package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// hintHold is how long the shortcut key, Cmd on macOS and Ctrl elsewhere,
// is held alone before each key shows under what it acts on: a shortcut
// typed in one go, Cmd+C or Cmd+R, shows none.
const hintHold = 500 * time.Millisecond

// hintPoll is how often the keys held are read. Fyne tells a key press
// only to the widget with the focus, or to the window when none has it, so
// a held Cmd would go unseen while typing in a search field; its driver
// knows the keys held whatever has the focus. Its loop wakes 60 times a
// second anyway, and a read draws nothing.
const hintPoll = 50 * time.Millisecond

// hint is a key, shown under what it acts on while the shortcut key is
// held. It takes no click and no hover: they go to what is under it.
type hint struct {
	widget.BaseWidget
	key string
	// target is what the key acts on, nil while the key would do nothing.
	target func() fyne.CanvasObject
}

func newHint(key fyne.KeyName, target func() fyne.CanvasObject) *hint {
	h := &hint{key: string(key), target: target}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hint) CreateRenderer() fyne.WidgetRenderer {
	r := &hintRenderer{h: h, box: canvas.NewRectangle(nil), text: canvas.NewText(h.key, nil)}
	r.text.Alignment = fyne.TextAlignCenter
	r.text.TextStyle.Bold = true
	r.Refresh()
	return r
}

// hintRenderer draws a hint as a tooltip is drawn, without its shadow.
type hintRenderer struct {
	h    *hint
	box  *canvas.Rectangle
	text *canvas.Text
}

func (r *hintRenderer) Layout(s fyne.Size) {
	r.box.Resize(s)
	h := r.text.MinSize().Height
	r.text.Move(fyne.NewPos(0, (s.Height-h)/2))
	r.text.Resize(fyne.NewSize(s.Width, h))
}

// MinSize is a square around one character, wider for a longer key.
func (r *hintRenderer) MinSize() fyne.Size {
	t := r.text.MinSize()
	pad := theme.Size(theme.SizeNameInnerPadding)
	return fyne.NewSize(max(t.Width, t.Height)+pad, t.Height+pad)
}

func (r *hintRenderer) Refresh() {
	r.box.FillColor = theme.Color(theme.ColorNameOverlayBackground)
	r.box.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	r.box.StrokeWidth = theme.Size(theme.SizeNameInputBorder)
	r.box.CornerRadius = theme.Size(theme.SizeNameInputRadius)
	r.text.Color = theme.Color(theme.ColorNameForeground)
	r.text.TextSize = theme.Size(theme.SizeNameCaptionText)
	r.Layout(r.h.Size())
	r.box.Refresh()
	r.text.Refresh()
}

func (r *hintRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.box, r.text} }

func (r *hintRenderer) Destroy() {}

// newHints is the layer of the hints, over the whole window: R under the
// app bar's Record or Stop, 1 to 5 under the tabs.
func (a *App) newHints() *fyne.Container {
	hints := []fyne.CanvasObject{newHint(actionKey, func() fyne.CanvasObject {
		if a.action.Disabled() {
			return nil
		}
		return a.action
	})}
	for i, item := range a.tabs.Items[:min(len(a.tabs.Items), len(tabDigits))] {
		hints = append(hints, newHint(tabDigits[i], func() fyne.CanvasObject {
			// The offline screen shows no tabs.
			if a.center.Objects[0] != a.tabsView || !tabsFit(a.tabs) {
				return nil
			}
			return tabButton(item)
		}))
	}
	l := &hintsLayout{d: a.fa.Driver()}
	l.layer = container.New(l, hints...)
	l.layer.Hide()
	return l.layer
}

// hintsLayout lays each hint under its target, centred on it, and hides
// those whose key would do nothing. Fyne draws the window again only for a
// hint that moved or changed.
type hintsLayout struct {
	d     fyne.Driver
	layer *fyne.Container
}

func (l *hintsLayout) Layout(objs []fyne.CanvasObject, _ fyne.Size) {
	origin := l.d.AbsolutePositionForObject(l.layer)
	for _, o := range objs {
		h := o.(*hint)
		t := h.target()
		if t == nil || !t.Visible() {
			h.Hide()
			continue
		}
		size := h.MinSize()
		h.Move(l.d.AbsolutePositionForObject(t).Subtract(origin).
			AddXY((t.Size().Width-size.Width)/2, t.Size().Height+theme.Padding()))
		h.Resize(size)
		h.Show()
	}
}

func (l *hintsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

// pollKeys has watchKeys read the keys held, for as long as the app runs.
func (a *App) pollKeys(d desktop.Driver) {
	for range time.Tick(hintPoll) {
		fyne.Do(func() { a.watchKeys(d.CurrentKeyModifiers(), time.Now()) })
	}
}

// watchKeys shows the hints once the shortcut key alone has been held for
// hintHold, and hides them when it is let go: mods are the keys held at
// now. A dialog or a menu takes the keys, so no hint shows over it.
func (a *App) watchKeys(mods fyne.KeyModifier, now time.Time) {
	if mods != fyne.KeyModifierShortcutDefault {
		a.keysHeld = time.Time{}
	} else if a.keysHeld.IsZero() {
		a.keysHeld = now
	}
	show := !a.keysHeld.IsZero() && now.Sub(a.keysHeld) >= hintHold && a.win.Canvas().Overlays().Top() == nil
	switch {
	case show == a.hints.Visible():
		if show {
			// A key may have moved a target: Stop is wider than Record,
			// and the offline screen has no tabs.
			a.hints.Layout.Layout(a.hints.Objects, a.hints.Size())
		}
		return
	case show:
		a.hints.Show()
		a.hints.Refresh()
	default:
		a.hints.Hide()
	}
	// Fyne draws an object again through the window it was drawn in, and
	// the layer, hidden since the window opened, was never drawn: the
	// window is told itself.
	a.win.Canvas().Refresh(a.hints)
}
