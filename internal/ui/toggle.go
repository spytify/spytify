package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// toggle is an on/off switch for a yes-or-no setting; Fyne has only check
// boxes. A tap on it, or the space bar once it has the focus, flips it.
type toggle struct {
	widget.BaseWidget
	on               bool
	changed          func(bool)
	focused, hovered bool
}

func newToggle(changed func(bool)) *toggle {
	t := &toggle{changed: changed}
	t.ExtendBaseWidget(t)
	return t
}

// SetOn shows a value without reporting it as a change.
func (t *toggle) SetOn(on bool) {
	if on == t.on {
		return
	}
	t.on = on
	t.Refresh()
}

func (t *toggle) flip() {
	t.on = !t.on
	t.Refresh()
	if t.changed != nil {
		t.changed(t.on)
	}
}

// Tapped flips the switch when the tap lands on it, not elsewhere in the
// form row it stretches across.
func (t *toggle) Tapped(e *fyne.PointEvent) {
	if e.Position.X > toggleSize().Width {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(t); c != nil && !t.focused && !fyne.CurrentDevice().IsMobile() {
		c.Focus(t)
	}
	t.flip()
}

func (t *toggle) FocusGained() { t.focused = true; t.Refresh() }
func (t *toggle) FocusLost()   { t.focused = false; t.Refresh() }

func (t *toggle) TypedRune(r rune) {
	if r == ' ' {
		t.flip()
	}
}

func (t *toggle) TypedKey(*fyne.KeyEvent) {}

func (t *toggle) MouseIn(*desktop.MouseEvent)    { t.hovered = true; t.Refresh() }
func (t *toggle) MouseMoved(*desktop.MouseEvent) {}
func (t *toggle) MouseOut()                      { t.hovered = false; t.Refresh() }

func (t *toggle) CreateRenderer() fyne.WidgetRenderer {
	r := &toggleRenderer{t: t, ring: canvas.NewRectangle(color.Transparent), track: canvas.NewRectangle(nil), halo: canvas.NewCircle(nil), knob: canvas.NewCircle(nil)}
	r.Refresh()
	return r
}

// toggleSize is the track's: as tall as an inline icon and a little, about
// twice as wide.
func toggleSize() fyne.Size {
	h := theme.Size(theme.SizeNameInlineIcon) + 2
	return fyne.NewSize(h*9/5, h)
}

// toggleInset is the gap between the track's edge and the knob.
const toggleInset = 3

// toggleRingGap is the gap between the track and the focus ring around it.
const toggleRingGap = 3

type toggleRenderer struct {
	t *toggle
	// ring shows the keyboard's focus around the track, apart from it: on
	// the track itself, the focus colour, a translucent green, vanishes
	// into the green of a switch that is on.
	ring, track *canvas.Rectangle
	halo, knob  *canvas.Circle
}

func (r *toggleRenderer) Layout(s fyne.Size) {
	ts := toggleSize()
	top := (s.Height - ts.Height) / 2
	r.ring.Move(fyne.NewPos(-toggleRingGap, top-toggleRingGap))
	r.ring.Resize(ts.AddWidthHeight(2*toggleRingGap, 2*toggleRingGap))
	r.track.Move(fyne.NewPos(0, top))
	r.track.Resize(ts)
	d := ts.Height - 2*toggleInset
	x := float32(toggleInset)
	if r.t.on {
		x = ts.Width - toggleInset - d
	}
	r.knob.Move(fyne.NewPos(x, top+toggleInset))
	r.knob.Resize(fyne.NewSquareSize(d))
	// The pointer's halo, around the knob.
	h := d + 2*toggleInset
	r.halo.Move(fyne.NewPos(x-toggleInset, top))
	r.halo.Resize(fyne.NewSquareSize(h))
}

func (r *toggleRenderer) MinSize() fyne.Size {
	return toggleSize().AddWidthHeight(0, 2*theme.Padding())
}

// Refresh draws on as a primary track with a white knob, and off as an
// input-coloured track with a grey knob, so both read in either theme.
func (r *toggleRenderer) Refresh() {
	ts := toggleSize()
	r.track.CornerRadius = ts.Height / 2
	r.track.StrokeWidth = theme.InputBorderSize()
	if r.t.on {
		r.track.FillColor = theme.Color(theme.ColorNamePrimary)
		r.track.StrokeColor = theme.Color(theme.ColorNamePrimary)
		r.knob.FillColor = color.White
	} else {
		r.track.FillColor = theme.Color(theme.ColorNameInputBackground)
		r.track.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		r.knob.FillColor = theme.Color(theme.ColorNamePlaceHolder)
	}
	r.ring.CornerRadius = ts.Height/2 + toggleRingGap
	r.ring.StrokeWidth = 2
	r.ring.StrokeColor = color.Transparent
	if r.t.focused {
		r.ring.StrokeColor = theme.Color(theme.ColorNameFocus)
	}
	r.halo.FillColor = color.Transparent
	if r.t.hovered {
		r.halo.FillColor = theme.Color(theme.ColorNameHover)
	}
	r.Layout(r.t.Size())
	r.ring.Refresh()
	r.track.Refresh()
	r.halo.Refresh()
	r.knob.Refresh()
}

func (r *toggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.ring, r.track, r.halo, r.knob}
}

func (r *toggleRenderer) Destroy() {}
