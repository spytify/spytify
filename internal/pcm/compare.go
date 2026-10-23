package pcm

import "math"

// Comparison is the result of matching a recorded file against its source.
type Comparison struct {
	LagFrames   int     // best alignment of got against want
	LagMs       float64 // the same in milliseconds
	ErrorDBFS   float64 // RMS of (got - want) at that lag, over the longer of the two
	LengthDelta int64   // frames got has beyond want after trimming (negative = shorter)
}

// Matches applies the plan's acceptance rule: the cross-correlation peak sits
// within maxLagMs of zero and the residual is below maxErrorDB.
func (c Comparison) Matches(maxLagMs, maxErrorDB float64) bool {
	return math.Abs(c.LagMs) <= maxLagMs && c.ErrorDBFS <= maxErrorDB
}

// Compare trims both signals to their first and last frame above trimDB,
// finds the lag within ±searchMs that maximises their cross-correlation, and
// measures the residual. A neighbour's audio in got, or a missing chunk,
// shows up as residual energy because the residual covers both lengths.
func Compare(wantFormat Format, want []byte, gotFormat Format, got []byte, trimDB, searchMs float64) Comparison {
	w := trim(wantFormat.Mono(want), trimDB)
	g := trim(gotFormat.Mono(got), trimDB)
	rate := float64(wantFormat.Rate)
	maxLag := int(searchMs * rate / 1000)
	bestLag, best := 0, math.Inf(-1)
	for lag := -maxLag; lag <= maxLag; lag++ {
		var dot float64
		// Every fourth sample is plenty to locate the peak.
		for i := 0; i < len(w); i += 4 {
			j := i + lag
			if j < 0 || j >= len(g) {
				continue
			}
			dot += float64(w[i]) * float64(g[j])
		}
		if dot > best {
			best, bestLag = dot, lag
		}
	}
	n := len(w)
	if len(g)-bestLag > n {
		n = len(g) - bestLag
	}
	var sum float64
	count := 0
	for i := 0; i < n; i++ {
		var a, b float64
		if i < len(w) {
			a = float64(w[i])
		}
		if j := i + bestLag; j >= 0 && j < len(g) {
			b = float64(g[j])
		}
		d := a - b
		sum += d * d
		count++
	}
	errDB := Silence
	if count > 0 {
		errDB = DBFS(math.Sqrt(sum / float64(count)))
	}
	return Comparison{
		LagFrames:   bestLag,
		LagMs:       float64(bestLag) * 1000 / rate,
		ErrorDBFS:   errDB,
		LengthDelta: int64(len(g)-bestLag) - int64(len(w)),
	}
}

func trim(x []float32, thresholdDB float64) []float32 {
	thr := float32(math.Pow(10, thresholdDB/20))
	start, end := 0, len(x)
	for start < end && abs32(x[start]) < thr {
		start++
	}
	for end > start && abs32(x[end-1]) < thr {
		end--
	}
	return x[start:end]
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
