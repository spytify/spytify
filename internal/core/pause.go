package core

import "github.com/spytify/spytify/internal/pcm"

// pauseExcision cuts a pause out of the stream: while paused, every run of
// at least pauseRunMs of silent frames is cut out at the sample, so a pause
// leaves neither a hole nor a seam. The session sets pausing and resumed
// as the player's status changes; trim and excise do the cutting.
type pauseExcision struct {
	pausing  bool
	excising bool
	resumed  bool
	quietRun int64
}

func quietFrame(f pcm.Format, data []byte, i int, thr float64) bool {
	for c := 0; c < f.Channels; c++ {
		if v := f.SampleAt(data, i*f.Channels+c); v > thr || v < -thr {
			return false
		}
	}
	return true
}

// trim handles a Paused notification that came after the audio stopped:
// the silence already captured is cut back to its first frame, no earlier
// than floor.
func (p *pauseExcision) trim(r *Ring, floor int64, silenceDB float64) {
	f := r.Format()
	tail := r.Bytes(floor, r.End())
	thr := pow10(silenceDB / 20)
	n := len(tail) / f.FrameBytes()
	run := 0
	for i := n - 1; i >= 0 && quietFrame(f, tail, i, thr); i-- {
		run++
	}
	p.quietRun, p.excising = int64(run), false
	if int64(run) >= f.FramesFromMs(pauseRunMs) {
		r.Truncate(r.End() - int64(run))
		p.quietRun, p.excising = 0, true
	}
}

// excise filters one chunk while paused: frames of a silent run of at
// least pauseRunMs are dropped, including the start of the run that earlier
// chunks left in the ring r, no earlier than floor; everything else is kept.
func (p *pauseExcision) excise(f pcm.Format, data []byte, r *Ring, floor func() int64, silenceDB float64) []byte {
	fb := f.FrameBytes()
	n := len(data) / fb
	thr := pow10(silenceDB / 20)
	minRun := f.FramesFromMs(pauseRunMs)
	out := make([]byte, 0, len(data))
	for i := 0; i < n; i++ {
		quiet := quietFrame(f, data, i, thr)
		if p.excising {
			if quiet {
				continue
			}
			p.excising = false
			if p.resumed {
				p.pausing, p.quietRun = false, 0
				return append(out, data[i*fb:]...)
			}
		}
		out = append(out, data[i*fb:(i+1)*fb]...)
		if !quiet {
			p.quietRun = 0
			continue
		}
		p.quietRun++
		if p.quietRun >= minRun {
			inChunk := p.quietRun
			if max := int64(len(out) / fb); inChunk > max {
				inChunk = max
			}
			out = out[:len(out)-int(inChunk)*fb]
			if earlier := p.quietRun - inChunk; earlier > 0 {
				r.Truncate(maxInt(r.End()-earlier, floor()))
			}
			p.quietRun, p.excising = 0, true
		}
	}
	return out
}
