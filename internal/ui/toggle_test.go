package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// toggleWindow is a switch alone in a window, in the app's light theme, with
// its renderer.
func toggleWindow(t *testing.T, changed func(bool)) (*toggle, *toggleRenderer, fyne.Window) {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	sw := newToggle(changed)
	w := test.NewTempWindow(t, sw)
	return sw, test.TempWidgetRenderer(t, sw).(*toggleRenderer), w
}

// Off, the knob sits at the track's left end, grey on the input colour; on,
// at its right end, white on the green. The pointer's halo stays centred on
// the knob.
func TestTheToggleShowsItsValueByTheKnobsSide(t *testing.T) {
	sw, r, _ := toggleWindow(t, nil)
	ts := toggleSize()
	for _, c := range []struct {
		on          bool
		track, knob color.Color
	}{
		{false, theme.Color(theme.ColorNameInputBackground), theme.Color(theme.ColorNamePlaceHolder)},
		{true, theme.Color(theme.ColorNamePrimary), color.White},
	} {
		sw.SetOn(c.on)
		track, knob := r.track.Position(), r.knob.Position()
		if c.on {
			if right := track.X + ts.Width - toggleInset; knob.X+r.knob.Size().Width != right {
				t.Errorf("on: the knob ends at %v, want %v", knob.X+r.knob.Size().Width, right)
			}
		} else if knob.X != track.X+toggleInset {
			t.Errorf("off: the knob starts at %v, want %v", knob.X, track.X+toggleInset)
		}
		if knob.Y != track.Y+toggleInset || r.knob.Size().Height != ts.Height-2*toggleInset {
			t.Errorf("on %v: the knob at y %v, %v tall, in a track at y %v, %v tall", c.on, knob.Y, r.knob.Size().Height, track.Y, ts.Height)
		}
		if r.track.FillColor != c.track || r.knob.FillColor != c.knob {
			t.Errorf("on %v: track %v and knob %v, want %v and %v", c.on, r.track.FillColor, r.knob.FillColor, c.track, c.knob)
		}
		centre := func(p fyne.Position, s fyne.Size) fyne.Position { return p.AddXY(s.Width/2, s.Height/2) }
		if h, k := centre(r.halo.Position(), r.halo.Size()), centre(knob, r.knob.Size()); h != k {
			t.Errorf("on %v: the halo is centred at %v, the knob at %v", c.on, h, k)
		}
	}
}

// The switch keeps its size in a form row wider and taller than it,
// centred in the row's height, and asks for a padding above and below.
func TestTheToggleKeepsItsSizeInABiggerRow(t *testing.T) {
	sw, r, _ := toggleWindow(t, nil)
	ts := toggleSize()
	if got, want := sw.MinSize(), ts.AddWidthHeight(0, 2*theme.Padding()); got != want {
		t.Errorf("min size %v, want %v", got, want)
	}
	sw.Resize(fyne.NewSize(400, 60))
	if want := fyne.NewPos(0, (60-ts.Height)/2); r.track.Position() != want || r.track.Size() != ts {
		t.Errorf("in a 400x60 row, the track is %v at %v; want %v at %v", r.track.Size(), r.track.Position(), ts, want)
	}
}

// A tap gives the switch the focus, which a second tap keeps, and the space
// bar then flips it. A switch with no callback flips all the same.
func TestATappedToggleTakesTheFocus(t *testing.T) {
	sw, _, w := toggleWindow(t, nil)
	test.Tap(sw)
	if w.Canvas().Focused() != sw || !sw.on {
		t.Fatalf("after a tap: focused %v, on %v", w.Canvas().Focused(), sw.on)
	}
	test.Tap(sw)
	if w.Canvas().Focused() != sw || sw.on {
		t.Errorf("after a second tap: focused %v, on %v", w.Canvas().Focused(), sw.on)
	}
	w.Canvas().Focused().TypedRune(' ')
	if !sw.on {
		t.Error("the space bar did not flip the focused switch")
	}
}

// The halo shows around the knob while the pointer is over the switch.
func TestTheToggleHaloFollowsThePointer(t *testing.T) {
	sw, r, w := toggleWindow(t, nil)
	if r.halo.FillColor != color.Transparent {
		t.Fatalf("a halo %v before the pointer came", r.halo.FillColor)
	}
	test.MoveMouse(w.Canvas(), sw.Position().AddXY(5, 5))
	if r.halo.FillColor != theme.Color(theme.ColorNameHover) {
		t.Errorf("under the pointer, the halo is %v", r.halo.FillColor)
	}
	test.MoveMouse(w.Canvas(), fyne.NewPos(-10, -10))
	if r.halo.FillColor != color.Transparent {
		t.Errorf("after the pointer left, the halo is %v", r.halo.FillColor)
	}
}
