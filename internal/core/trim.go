package core

import (
	"math"

	"github.com/spytify/spytify/internal/pcm"
)

// scanTrim returns the frame range [start, end) of a WAV file between its
// first and last sample above db. A sample only counts when the 10 ms
// around it is above db too, so an isolated click does not stop the trim.
func scanTrim(path string, db float64) (start, end int64, ok bool, err error) {
	w, err := pcm.OpenWAV(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer w.Close()
	format := w.Format()
	fw := &frameWindow{w: w, size: format.FramesFromMs(4000)}
	block := format.FramesFromMs(10)
	total := w.Frames()
	thr := math.Pow(10, db/20)
	loudFrame := func(i int64) bool {
		b := fw.frames(i, i+1)
		for c := 0; c < format.Channels; c++ {
			if math.Abs(format.SampleAt(b, c)) > thr {
				return true
			}
		}
		return false
	}
	loudSpan := func(from, to int64) bool {
		_, rms := format.Level(fw.frames(from, to))
		return rms > db
	}
	start = -1
	for i := int64(0); i < total; i++ {
		if loudFrame(i) && loudSpan(i, i+block) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false, fw.err
	}
	end = start + 1
	for j := total - 1; j >= start; j-- {
		if loudFrame(j) && loudSpan(j+1-block, j+1) {
			end = j + 1
			break
		}
	}
	// silence_db finds the sound; the fade around it is kept down to 12 dB
	// under it, until 1 ms of quieter samples.
	floor := thr / 4
	quietRun := format.FramesFromMs(1)
	maxExtend := format.FramesFromMs(100)
	above := func(i int64) bool {
		b := fw.frames(i, i+1)
		for c := 0; c < format.Channels; c++ {
			if math.Abs(format.SampleAt(b, c)) > floor {
				return true
			}
		}
		return false
	}
	for i, quiet := start-1, int64(0); i >= 0 && start-i <= maxExtend && quiet < quietRun; i-- {
		if above(i) {
			start, quiet = i, 0
		} else {
			quiet++
		}
	}
	for i, quiet := end, int64(0); i < total && i-end <= maxExtend && quiet < quietRun; i++ {
		if above(i) {
			end, quiet = i+1, 0
		} else {
			quiet++
		}
	}
	return start, end, true, fw.err
}

// frameWindow keeps a few seconds of a WAV file around the frames asked
// for, loading ahead or behind as the scan moves, so trimming a long take
// does not load it whole.
type frameWindow struct {
	w    *pcm.WAVFile
	size int64
	from int64
	data []byte
	err  error
}

// frames returns frames [from, to) of the file, clamped to it.
func (fw *frameWindow) frames(from, to int64) []byte {
	from, to = max(from, 0), min(to, fw.w.Frames())
	if to <= from {
		return nil
	}
	fb := int64(fw.w.Format().FrameBytes())
	if from < fw.from || to > fw.from+int64(len(fw.data))/fb {
		lo := from
		if from < fw.from {
			// Moving back: the window ends where the scan is.
			lo = min(from, to-fw.size)
		}
		lo = max(lo, 0)
		data, err := fw.w.ReadFrames(lo, max(to, lo+fw.size))
		if err != nil {
			if fw.err == nil {
				fw.err = err
			}
			lo, data = from, make([]byte, (to-from)*fb)
		}
		fw.from, fw.data = lo, data
	}
	return fw.data[(from-fw.from)*fb : (to-fw.from)*fb]
}
