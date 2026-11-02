package ui

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/pkg/proto"
)

// stereoMeter is the level of what is recorded as a bar per channel, left
// then right, as a mixing desk shows it: lit from the bottom up to the
// channel's RMS, green turning yellow as it gets loud. A mono stream lights
// both alike.
type stereoMeter struct {
	widget.BaseWidget
	// level is each bar's level in dBFS as drawn: it rises with the sound
	// at once and falls back meterFall dB a second at most, as a needle
	// does, rather than jump down between two beats. at is when it was
	// set: the screen also renders on other events than the level's.
	level [2]float64
	at    time.Time
	now   func() time.Time
	// lit is each bar's lit height, in pixels of meterHeight, when it was
	// last drawn: the meter is drawn again only when one changed.
	lit [2]float32
}

const (
	meterHeight   = 64
	meterBarWidth = 6
	// meterFall is how fast a bar falls, in dB a second: 2 dB between two
	// reports, a tenth of a second apart.
	meterFall = 20.0
	// meterFloor is the foot of the scale, where a bar is out.
	meterFloor = -70.0
	// Below meterWarm a bar is green; it turns yellow up to meterHot.
	// Recorded songs, which the player brings to a like loudness, spend
	// most of their time from -21 to -14 dBFS RMS, their loud parts above:
	// their tops are green-yellow, and yellow when they get loud.
	meterWarm = -20.0
	meterHot  = -9.0
)

func newStereoMeter() *stereoMeter {
	m := &stereoMeter{level: [2]float64{meterFloor, meterFloor}, now: time.Now}
	m.ExtendBaseWidget(m)
	return m
}

// meterShare is the share of a bar that a level in dBFS lights, on the
// scale of digital meters (IEC 60268-18): the top half for the last 20 dB,
// where songs are, and less and less below. A song's median lights a bar
// from half to two thirds of the way up.
func meterShare(db float64) float64 {
	switch {
	case db < meterFloor:
		return 0
	case db < -60:
		return (db + 70) * 0.0025
	case db < -50:
		return 0.025 + (db+60)*0.005
	case db < -40:
		return 0.075 + (db+50)*0.0075
	case db < -30:
		return 0.15 + (db+40)*0.015
	case db < -20:
		return 0.3 + (db+30)*0.02
	case db < 0:
		return 0.5 + (db+20)*0.025
	}
	return 1
}

// SetLevel takes a level report: each bar its channel's, both the one
// channel's for mono, or both the whole's from a recorder that does not
// tell the channels apart.
func (m *stereoMeter) SetLevel(e proto.LevelEvent) {
	left, right := e.RMSDBFS, e.RMSDBFS
	switch len(e.Channels) {
	case 0:
	case 1:
		left, right = e.Channels[0].RMSDBFS, e.Channels[0].RMSDBFS
	default:
		left, right = e.Channels[0].RMSDBFS, e.Channels[1].RMSDBFS
	}
	now := m.now()
	fall := meterFall * now.Sub(m.at).Seconds()
	m.at = now
	for i, db := range [2]float64{left, right} {
		m.level[i] = max(db, m.level[i]-fall, meterFloor)
	}
	m.draw()
}

// Clear puts both bars out at once: nothing is recorded.
func (m *stereoMeter) Clear() {
	m.level = [2]float64{meterFloor, meterFloor}
	m.draw()
}

func (m *stereoMeter) draw() {
	var lit [2]float32
	for i, db := range m.level {
		lit[i] = float32(math.Round(meterShare(db) * meterHeight))
	}
	if lit == m.lit {
		return
	}
	m.lit = lit
	m.Refresh()
}

func (m *stereoMeter) CreateRenderer() fyne.WidgetRenderer {
	r := &meterRenderer{m: m}
	for i := range r.bars {
		r.tracks[i] = canvas.NewRectangle(nil)
		r.bars[i] = canvas.NewVerticalGradient(nil, nil)
	}
	r.Refresh()
	return r
}

type meterRenderer struct {
	m      *stereoMeter
	tracks [2]*canvas.Rectangle
	bars   [2]*canvas.LinearGradient
}

// Layout puts each bar over the middle of a cell of a two-column grid as
// wide as the meter, where its L or R caption is.
func (r *meterRenderer) Layout(s fyne.Size) {
	pad := theme.Padding()
	cell := (s.Width - pad) / 2
	for i := range r.bars {
		x := float32(i)*(cell+pad) + (cell-meterBarWidth)/2
		r.tracks[i].Move(fyne.NewPos(x, 0))
		r.tracks[i].Resize(fyne.NewSize(meterBarWidth, s.Height))
		h := r.m.lit[i] * s.Height / meterHeight
		r.bars[i].Move(fyne.NewPos(x, s.Height-h))
		r.bars[i].Resize(fyne.NewSize(meterBarWidth, h))
	}
}

func (r *meterRenderer) MinSize() fyne.Size {
	return fyne.NewSize(2*meterBarWidth+theme.Padding(), meterHeight)
}

// Refresh colours each lit bar from green at its foot to the colour of its
// level at its top.
func (r *meterRenderer) Refresh() {
	nrgba := func(n fyne.ThemeColorName) color.NRGBA {
		return color.NRGBAModel.Convert(theme.ColorForWidget(n, r.m)).(color.NRGBA)
	}
	green, yellow := nrgba(theme.ColorNamePrimary), nrgba(colorMeterLoud)
	for i := range r.bars {
		r.tracks[i].FillColor = theme.ColorForWidget(colorMediaBadge, r.m)
		r.tracks[i].CornerRadius = 1
		t := (r.m.level[i] - meterWarm) / (meterHot - meterWarm)
		r.bars[i].StartColor = mix(green, yellow, min(max(t, 0), 1))
		r.bars[i].EndColor = green
		r.tracks[i].Refresh()
		r.bars[i].Refresh()
	}
	r.Layout(r.m.Size())
}

func (r *meterRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.tracks[0], r.tracks[1], r.bars[0], r.bars[1]}
}

func (r *meterRenderer) Destroy() {}

// mix is a colour t of the way from a to b.
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	at := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{R: at(a.R, b.R), G: at(a.G, b.G), B: at(a.B, b.B), A: at(a.A, b.A)}
}
