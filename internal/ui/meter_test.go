package ui

import (
	"image/color"
	"testing"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/pkg/proto"
)

// The scale is a digital meter's: the top half for the last 20 dB. A song,
// whose median the recorder measures from -21 to -14 dBFS RMS, lights a
// bar from about half to two thirds of the way up, and its loud parts
// barely more: the top is left to what is louder still.
func TestTheMeterScaleIsADigitalMeters(t *testing.T) {
	for _, c := range []struct {
		db, share float64
	}{
		{-120, 0}, {-70, 0}, {-60, 0.025}, {-50, 0.075}, {-40, 0.15},
		{-30, 0.3}, {-20, 0.5}, {-10, 0.75}, {0, 1}, {3, 1},
	} {
		if got := meterShare(c.db); got < c.share-1e-9 || got > c.share+1e-9 {
			t.Errorf("%v dBFS lights %v of a bar, want %v", c.db, got, c.share)
		}
	}
	if s := meterShare(-21); s < 0.45 {
		t.Errorf("a soft song lights %v of a bar", s)
	}
	if s := meterShare(-14); s > 2.0/3 {
		t.Errorf("a song's median lights %v of a bar", s)
	}
	if s := meterShare(-13); s > 0.7 {
		t.Errorf("loud rock lights %v of a bar", s)
	}
}

// Each bar is its channel's: left then right. A mono stream lights both
// alike, and so does a recorder that sends only the whole's level.
func TestTheMeterHasABarPerChannel(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "dark"})
	both := func(rms float64) [2]float32 {
		px := float32(meterShare(rms) * meterHeight)
		return [2]float32{px, px}
	}
	for _, c := range []struct {
		what string
		e    proto.LevelEvent
		lit  [2]float32
	}{
		{"stereo", proto.LevelEvent{RMSDBFS: -14, Channels: []proto.ChannelLevel{{RMSDBFS: -10}, {RMSDBFS: -30}}}, [2]float32{48, 19}},
		{"mono", proto.LevelEvent{RMSDBFS: -20, Channels: []proto.ChannelLevel{{RMSDBFS: -20}}}, both(-20)},
		{"no channels", proto.LevelEvent{RMSDBFS: -10}, both(-10)},
	} {
		m := newStereoMeter()
		m.SetLevel(c.e)
		if m.lit != c.lit {
			t.Errorf("%s lights %v pixels, want %v", c.what, m.lit, c.lit)
		}
	}
}

// A bar rises with the sound at once and falls back 20 dB a second, as a
// needle does; paused or stopped, it is out at once.
func TestTheMeterRisesAtOnceAndFallsBack(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "dark"})
	m := newStereoMeter()
	clock := time.Now()
	m.now = func() time.Time { return clock }
	level := func(db float64) {
		m.SetLevel(proto.LevelEvent{RMSDBFS: db})
	}
	level(-10)
	if m.level[0] != -10 {
		t.Fatalf("the first report shows %v dBFS", m.level[0])
	}
	clock = clock.Add(100 * time.Millisecond)
	level(-40)
	if d := m.level[0] - (-12); d < -1e-9 || d > 1e-9 {
		t.Errorf("a tenth of a second after -10 dBFS, a bar at -40 shows %v", m.level[0])
	}
	// Other renders between two reports bring nothing new: the bar falls
	// with the time only.
	level(-40)
	if d := m.level[0] - (-12); d < -1e-9 || d > 1e-9 {
		t.Errorf("the same report again shows %v", m.level[0])
	}
	clock = clock.Add(time.Second)
	level(-40)
	if d := m.level[0] - (-32); d < -1e-9 || d > 1e-9 {
		t.Errorf("a second later the bar shows %v dBFS, want -32", m.level[0])
	}
	level(-6)
	if m.level[0] != -6 {
		t.Errorf("a louder report shows %v dBFS", m.level[0])
	}
	m.Clear()
	if m.lit != [2]float32{} {
		t.Errorf("cleared, %v pixels are lit", m.lit)
	}
}

// A bar is green at its foot; its top turns yellow as it gets loud, and a
// quiet one stays green.
func TestTheMeterTurnsYellowAsItGetsLoud(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "dark"})
	m := newStereoMeter()
	container.NewThemeOverride(m, mediaTheme{})
	r := test.TempWidgetRenderer(t, m).(*meterRenderer)
	m.SetLevel(proto.LevelEvent{Channels: []proto.ChannelLevel{{RMSDBFS: -30}, {RMSDBFS: -6}}})
	quiet, loud := r.bars[0], r.bars[1]
	for _, b := range []*canvas.LinearGradient{quiet, loud} {
		if b.EndColor != greenDark {
			t.Errorf("a bar's foot is %v, want the green %v", b.EndColor, greenDark)
		}
	}
	if quiet.StartColor != greenDark {
		t.Errorf("a quiet bar's top is %v, want green", quiet.StartColor)
	}
	if want := (color.NRGBA{R: 0xff, G: 0xd5, B: 0x3d, A: 0xff}); loud.StartColor != want {
		t.Errorf("a loud bar's top is %v, want the yellow %v", loud.StartColor, want)
	}
}
