package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// A bar is a thin line centred in its height, filled from the left by its
// value, which stays between 0 and 1; the fill follows a resize.
func TestABarFillsItsShare(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	b := newBar(theme.ColorNameSuccess)
	test.NewTempWindow(t, b)
	if b.MinSize() != fyne.NewSize(40, barHeight) {
		t.Errorf("min size %v", b.MinSize())
	}
	b.Resize(fyne.NewSize(200, 20))
	r := test.TempWidgetRenderer(t, b).(*barRenderer)
	for _, c := range []struct{ set, width float32 }{
		{0.25, 50},
		{1.5, 200},
		{-1, 0},
		{0.5, 100},
	} {
		b.SetValue(float64(c.set))
		if r.fill.Size() != fyne.NewSize(c.width, barHeight) {
			t.Errorf("SetValue(%v): the fill is %v, want %v wide", c.set, r.fill.Size(), c.width)
		}
	}
	y := float32(20-barHeight) / 2
	if r.track.Position() != fyne.NewPos(0, y) || r.track.Size() != fyne.NewSize(200, barHeight) || r.fill.Position() != fyne.NewPos(0, y) {
		t.Errorf("the track is %v at %v, the fill at %v", r.track.Size(), r.track.Position(), r.fill.Position())
	}
	b.Resize(fyne.NewSize(300, 20))
	if r.fill.Size().Width != 150 || r.track.Size().Width != 300 {
		t.Errorf("at 300 wide, the fill is %v and the track %v", r.fill.Size().Width, r.track.Size().Width)
	}
	if r.fill.FillColor != theme.Color(theme.ColorNameSuccess) || r.track.FillColor != theme.Color(theme.ColorNameInputBackground) {
		t.Errorf("the fill is %v on a track %v", r.fill.FillColor, r.track.FillColor)
	}
	if r.fill.CornerRadius != barHeight/2 || r.track.CornerRadius != barHeight/2 {
		t.Errorf("rounded %v and %v", r.fill.CornerRadius, r.track.CornerRadius)
	}
}
