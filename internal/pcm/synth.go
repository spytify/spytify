package pcm

import "math"

// Synth describes a deterministic test signal: a chord of three partials
// with a slow tremolo and a little noise, distinct for every seed, so a
// neighbour's samples in a file show up in a cross-correlation.
type Synth struct {
	Seed     int64
	Hz       float64
	LevelDB  float64 // peak level of the body, default -12 dBFS
	FadeInMs int64
	FadeOut  int64 // fade-out length in ms
}

// Render returns frames of the signal in format.
func (s Synth) Render(format Format, frames int64) []byte {
	b := make([]byte, frames*int64(format.FrameBytes()))
	level := s.LevelDB
	if level == 0 {
		level = -12
	}
	amp := math.Pow(10, level/20)
	hz := s.Hz
	if hz == 0 {
		hz = 220 + float64(s.Seed%17)*31
	}
	rng := uint64(s.Seed)*6364136223846793005 + 1442695040888963407
	fadeIn := format.FramesFromMs(s.FadeInMs)
	fadeOut := format.FramesFromMs(s.FadeOut)
	rate := float64(format.Rate)
	trem := 0.5 + float64(s.Seed%5)*0.37
	for i := int64(0); i < frames; i++ {
		t := float64(i) / rate
		v := 0.55*math.Sin(2*math.Pi*hz*t) +
			0.25*math.Sin(2*math.Pi*hz*1.5*t+float64(s.Seed)) +
			0.12*math.Sin(2*math.Pi*hz*2.01*t)
		rng = rng*6364136223846793005 + 1442695040888963407
		noise := (float64(rng>>40)/float64(1<<24) - 0.5) * 0.08
		env := 0.8 + 0.2*math.Sin(2*math.Pi*trem*t)
		if fadeIn > 0 && i < fadeIn {
			env *= float64(i) / float64(fadeIn)
		}
		if fadeOut > 0 && i >= frames-fadeOut {
			env *= float64(frames-1-i) / float64(fadeOut)
		}
		sample := amp * env * (v + noise)
		for c := 0; c < format.Channels; c++ {
			// A small inter-channel difference keeps stereo honest.
			format.PutSample(b, int(i)*format.Channels+c, sample*(1-0.05*float64(c)))
		}
	}
	return b
}
