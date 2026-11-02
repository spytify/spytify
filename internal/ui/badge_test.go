package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// A song lights the middle bars and its loud parts all six, classical
// music two, and close to silence the stub alone. The levels are the
// recorder's tenth-of-a-second RMS as recorded songs reach them: a median
// from -20 to -16 dBFS, their loudest quarter above -15.
func TestTheMeterSpreadsASongOverItsBars(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	m := newLevelMeter(colorOnRecBadge)
	for _, c := range []struct {
		what string
		rms  float64
		lit  int
	}{
		{"silence", -120, 1},
		{"close to silence", -45, 1},
		{"classical music", -32, 2},
		{"a soft song", -24, 3},
		{"a song's quiet part", -20, 4},
		{"a song's median", -17, 5},
		{"loud rock", -13, 6},
	} {
		m.SetLevel(c.rms)
		if m.lit != c.lit {
			t.Errorf("%s, at %v dBFS, lights %d bars, want %d", c.what, c.rms, m.lit, c.lit)
		}
	}
}

// The meter has a stub for level 0, then bars that rise by at least two
// pixels each, the last as clearly as the others.
func TestTheMetersBarsRiseFromAStub(t *testing.T) {
	if len(levelHeights) != len(levelSteps)+1 {
		t.Fatalf("%d bars for %d steps", len(levelHeights), len(levelSteps))
	}
	if h := levelHeights[0]; h < 1 || h > 2 {
		t.Errorf("the stub is %v pixels high", h)
	}
	for i := 1; i < len(levelHeights); i++ {
		if d := levelHeights[i] - levelHeights[i-1]; d < 2 {
			t.Errorf("bar %d is %v pixels over the one before", i, d)
		}
	}
}
