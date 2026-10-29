//go:build integration && linux

package capture

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

var noiseFormat = pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}

// The noise plays after a silence long enough for the capture to find the
// stream, mute it and start recording.
const (
	noiseLeadIn = 2 * time.Second
	noiseLength = 8 * time.Second
	noiseTail   = time.Second
)

// A 44.1 kHz stream played by pw-play, recorded the way spytifyd records
// the player, must not be converted by the capture. PipeWire converts it to
// the rate its graph runs at, and the graph's rates decide that: allowed to
// run at 44.1 kHz, as the gate's session lets it, the stream must come out
// bit for bit. With PipeWire's own rates (SPYTIFY_PIPEWIRE_RATES=default in
// scripts/pipewire-session.sh) the graph runs at 48 kHz only, and Spytify
// never changes the user's rates: the capture must then record at 48 kHz,
// one conversion, not convert back to 44.1 kHz. It needs a running PipeWire
// session, and pipewire-pulse for the PulseAudio backend.
func TestTheCaptureAddsNoConversionToA44100HzSource(t *testing.T) {
	if err := exec.Command("pw-cli", "info", "0").Run(); err != nil {
		t.Skip("no PipeWire session; source scripts/pipewire-session.sh first")
	}
	s := pipewireClock{}.settings(context.Background())
	exact := s.force == noiseFormat.Rate || (s.force == 0 && slices.Contains(s.allowed, noiseFormat.Rate))
	t.Logf("PipeWire's clock: force-rate %d, allowed rates %v", s.force, s.allowed)
	src := noise()
	wav := filepath.Join(t.TempDir(), "noise.wav")
	writeNoise(t, wav, src)
	for _, c := range []struct {
		name  string
		tools procBackend
		mute  bool
	}{
		{"pipewire", pipewireTools{}, false},
		{"pipewire-muted", pipewireTools{}, true},
		{"pulseaudio", pulseTools{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := c.tools.(pulseTools); ok && exec.Command("pactl", "info").Run() != nil {
				t.Skip("no PulseAudio server; start pipewire-pulse first")
			}
			p, err := NewLinux(t.Logf)
			if err != nil {
				t.Fatal(err)
			}
			p.backend, p.MuteOutput = c.tools, c.mute
			tg := target.Target{ProcessNames: []string{"pw-play"}}
			got, f, graph := recordNoise(t, p, tg, wav)
			r := compare(got, f, src)
			t.Logf("graph at %d Hz, capture asked for %d Hz: %.2f %% of samples on the 16-bit grid, %.2f %% of the source's frames found, %d offset change(s), %d overrun(s)",
				graph, f.Rate, r.grid, r.found, r.offsets, p.Overruns())
			conversions := 0
			for _, changed := range []bool{graph != noiseFormat.Rate, f.Rate != graph} {
				if changed {
					conversions++
				}
			}
			switch {
			case exact && !r.exact():
				t.Errorf("not bit-exact: %d Hz source, %d Hz graph, %d Hz capture, so %d conversion(s)", noiseFormat.Rate, graph, f.Rate, conversions)
			case conversions > 1:
				t.Errorf("converted back: %d Hz source, %d Hz graph, %d Hz capture, so %d conversions; PipeWire's alone is one", noiseFormat.Rate, graph, f.Rate, conversions)
			}
		})
	}
}

// recordNoise plays the noise and returns what the capture delivered, in
// its format, and the graph's rate while the noise played.
func recordNoise(t *testing.T, p *PipeWire, tg target.Target, wav string) ([]byte, pcm.Format, int) {
	t.Helper()
	if _, err := p.Open(context.Background(), tg); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	player := exec.Command("pw-play", wav)
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	played := make(chan error, 1)
	go func() { played <- player.Wait() }()
	defer player.Process.Kill()

	var data []byte
	var f pcm.Format
	graph := 0
	var stop <-chan time.Time
	deadline := time.After(noiseLeadIn + noiseLength + noiseTail + 20*time.Second)
	for {
		select {
		case c, ok := <-p.Frames():
			if !ok {
				t.Fatal("the capture ended")
			}
			if f != (pcm.Format{}) && c.Format != f {
				t.Fatalf("the capture restarted in %v after %v", c.Format, f)
			}
			f = c.Format
			data = append(data, c.Data...)
			// By 3 s of capture the noise plays, and a muted stream has
			// moved to its sink.
			if graph == 0 && int64(len(data)) >= f.FramesFromMs(3000)*int64(f.FrameBytes()) {
				graph = graphRate(t, tg)
			}
		case err := <-played:
			if err != nil {
				t.Fatalf("pw-play: %v", err)
			}
			stop = time.After(time.Second)
		case <-stop:
			if len(data) == 0 {
				t.Fatal("the capture delivered nothing")
			}
			return data, f, graph
		case <-deadline:
			t.Fatal("pw-play did not finish")
		}
	}
}

// graphRate is the clock rate of the driver the target's stream runs with:
// the rate PipeWire converts the stream to. pw-dump names the driver but
// not its rate (a null sink's Format stays at 48 kHz while the graph runs
// at 44.1), so the rate comes from pw-top's profiler, whose first round is
// empty.
func graphRate(t *testing.T, tg target.Target) int {
	t.Helper()
	out, err := exec.Command("pw-dump", "--no-colors").Output()
	if err != nil {
		t.Fatal(err)
	}
	stream, ok, err := findPipeWireNode(out, tg)
	if err != nil || !ok {
		t.Fatalf("no stream in pw-dump: %v", err)
	}
	driver := strconv.Itoa(stream.driver)
	top, err := exec.Command("pw-top", "--batch-mode", "--iterations", "2").Output()
	if err != nil {
		t.Fatalf("pw-top: %v", err)
	}
	rate := 0
	for _, line := range strings.Split(string(top), "\n") {
		// S ID QUANT RATE ...
		if f := strings.Fields(line); len(f) > 3 && f[1] == driver {
			rate, _ = strconv.Atoi(f[3])
		}
	}
	if rate == 0 {
		t.Fatalf("pw-top shows no rate for driver %s of stream %s:\n%s", driver, stream.key, top)
	}
	return rate
}

// noise is 16-bit stereo white noise peaking at -30 dBFS, the same every
// run, as the Windows measurement plays.
func noise() []int16 {
	n := noiseFormat.FramesFromMs(noiseLength.Milliseconds()) * int64(noiseFormat.Channels)
	s := make([]int16, n)
	x := uint32(12345)
	for i := range s {
		x = x*1664525 + 1013904223
		s[i] = int16(int(x>>16)%2001 - 1000)
	}
	return s
}

func writeNoise(t *testing.T, path string, src []int16) {
	t.Helper()
	w, err := pcm.CreateWAV(path, noiseFormat)
	if err != nil {
		t.Fatal(err)
	}
	silence := func(d time.Duration) []byte {
		return make([]byte, noiseFormat.FramesFromMs(d.Milliseconds())*int64(noiseFormat.FrameBytes()))
	}
	b := make([]byte, 2*len(src))
	for i, v := range src {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	for _, part := range [][]byte{silence(noiseLeadIn), b, silence(noiseTail)} {
		if _, err := w.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// match is how much of the source a capture holds unchanged.
type match struct {
	grid    float64 // % of the capture's non-zero samples on the 16-bit grid
	found   float64 // % of the source's frames found unchanged
	offsets int     // offset changes between runs: frames dropped or added
}

func (m match) exact() bool { return m.found >= 99 && m.offsets == 0 }

// compare finds the source's stereo frames in the capture, rounded to 16
// bits: each run that copies the source exactly, at whatever offset. Two
// frames index a position in the source, so a run can start anywhere.
func compare(data []byte, f pcm.Format, src []int16) match {
	const tolerance = 0.001
	n := len(data) / f.Sample.Bytes()
	q := make([]int32, n)
	on := make([]bool, n)
	var m match
	nonzero, grid := 0, 0
	for i := range q {
		v := f.SampleAt(data, i) * 32768
		q[i] = int32(math.Round(v))
		on[i] = math.Abs(v-float64(q[i])) < tolerance
		if v != 0 {
			nonzero++
			if on[i] {
				grid++
			}
		}
	}
	if nonzero > 0 {
		m.grid = 100 * float64(grid) / float64(nonzero)
	}
	if f.Channels != 2 {
		return m
	}
	type pair [4]int32
	at := func(s []int32, frame int) pair { return pair{s[2*frame], s[2*frame+1], s[2*frame+2], s[2*frame+3]} }
	s := make([]int32, len(src))
	for i, v := range src {
		s[i] = int32(v)
	}
	frames, srcFrames := n/2, len(s)/2
	index := map[pair]int{}
	for j := 0; j+1 < srcFrames; j++ {
		if _, ok := index[at(s, j)]; !ok {
			index[at(s, j)] = j
		}
	}
	same := func(g, j int) bool {
		return on[2*g] && on[2*g+1] && q[2*g] == s[2*j] && q[2*g+1] == s[2*j+1]
	}
	matched, offset, runs := 0, 0, 0
	for g := 0; g+1 < frames; {
		j, ok := index[at(q, g)]
		if !ok || !same(g, j) || !same(g+1, j+1) || (q[2*g] == 0 && q[2*g+1] == 0) {
			g++
			continue
		}
		run := 0
		for g+run < frames && j+run < srcFrames && same(g+run, j+run) {
			run++
		}
		if run < 4 {
			g++
			continue
		}
		if runs > 0 && g-j != offset {
			m.offsets++
		}
		offset, runs = g-j, runs+1
		matched += run
		g += run
	}
	m.found = 100 * float64(matched) / float64(srcFrames)
	return m
}
