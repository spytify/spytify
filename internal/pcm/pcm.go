// Package pcm holds the audio primitives shared by capture, core and the
// fakes: sample formats, levels, WAV files and the comparison used by the
// boundary tests.
package pcm

import (
	"encoding/binary"
	"fmt"
	"math"
)

// SampleType is the encoding of one sample.
type SampleType int

const (
	S16LE SampleType = iota
	F32LE
)

func (s SampleType) String() string {
	if s == F32LE {
		return "f32le"
	}
	return "s16le"
}

// Bytes is the size of one sample.
func (s SampleType) Bytes() int {
	if s == F32LE {
		return 4
	}
	return 2
}

// Format describes interleaved PCM.
type Format struct {
	Rate     int
	Channels int
	Sample   SampleType
}

func (f Format) String() string {
	return fmt.Sprintf("%s %d Hz %dch", f.Sample, f.Rate, f.Channels)
}

func (f Format) Valid() bool { return f.Rate > 0 && f.Channels > 0 }

// FrameBytes is the size of one frame (one sample per channel).
func (f Format) FrameBytes() int { return f.Channels * f.Sample.Bytes() }

// FramesFromMs converts a duration in milliseconds to a frame count.
func (f Format) FramesFromMs(ms int64) int64 {
	return int64(math.Round(float64(ms) * float64(f.Rate) / 1000))
}

// MsFromFrames converts a frame count to milliseconds.
func (f Format) MsFromFrames(frames int64) int64 {
	if f.Rate == 0 {
		return 0
	}
	return int64(math.Round(float64(frames) * 1000 / float64(f.Rate)))
}

// Sample returns sample i of b as a float in [-1, 1].
func (f Format) SampleAt(b []byte, i int) float64 {
	if f.Sample == F32LE {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])))
	}
	return float64(int16(binary.LittleEndian.Uint16(b[i*2:]))) / 32768
}

// PutSample writes v (clamped to [-1, 1]) as sample i of b.
func (f Format) PutSample(b []byte, i int, v float64) {
	if v > 1 {
		v = 1
	} else if v < -1 {
		v = -1
	}
	if f.Sample == F32LE {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(float32(v)))
		return
	}
	s := math.Round(v * 32767)
	binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(s)))
}

// Mono mixes interleaved frames down to one float per frame.
func (f Format) Mono(b []byte) []float32 {
	n := len(b) / f.FrameBytes()
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		var sum float64
		for c := 0; c < f.Channels; c++ {
			sum += f.SampleAt(b, i*f.Channels+c)
		}
		out[i] = float32(sum / float64(f.Channels))
	}
	return out
}

// Silence is the level reported for digital silence.
const Silence = -120.0

// DBFS converts a linear amplitude to dBFS.
func DBFS(amplitude float64) float64 {
	if amplitude <= 1e-6 {
		return Silence
	}
	return 20 * math.Log10(amplitude)
}

// Level returns the peak and RMS of b in dBFS, over every channel.
func (f Format) Level(b []byte) (peak, rms float64) {
	n := len(b) / f.Sample.Bytes()
	if n == 0 {
		return Silence, Silence
	}
	var p, sum float64
	for i := 0; i < n; i++ {
		v := f.SampleAt(b, i)
		sum += v * v
		if a := math.Abs(v); a > p {
			p = a
		}
	}
	return DBFS(p), DBFS(math.Sqrt(sum / float64(n)))
}
