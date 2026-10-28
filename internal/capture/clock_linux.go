//go:build linux

package capture

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spytify/spytify/internal/proc"
)

// rateInterval is how often a recording checks the rate PipeWire's graph
// runs at. A running graph changes rate only when its driver restarts, as
// when the stream moves to another output, so once a second follows it
// soon enough; pw-top waits for two profiler rounds (about 45 ms) to say it.
const rateInterval = time.Second

// clockTimeout bounds one reading of the clock. A running graph gives pw-top
// a profiler round each cycle, an idle one once a second only: pw-top is
// asked about running streams only.
const clockTimeout = time.Second

// graphClock reads the clock of PipeWire's graph, which converts every
// stream to the rate it runs at before anyone records it. Both backends
// record from that graph: pipewire-pulse serves parec from it.
type graphClock interface {
	// running is the rate the driver runs its graph at, 0 when unknown.
	running(ctx context.Context, driver int) int
	settings(ctx context.Context) clockSettings
}

// clockSettings is what PipeWire's settings metadata says about the rate.
// Spytify only reads it: setting a rate would switch the user's sound card.
type clockSettings struct {
	force   int   // clock.force-rate, 0 when the rate is not forced
	allowed []int // clock.allowed-rates
}

// recordRate is the rate to record a stream at, and why. At the rate the
// graph runs at, the stream is converted once at most, and kept bit for bit
// when the graph runs at the stream's rate; at another rate it is converted
// a second time. When the graph's rate is unknown, PipeWire's settings may
// still fix it. fromStream says no clock did, so the stream's next rate is
// the next recording's.
func recordRate(stream, running int, s clockSettings) (rate int, why string, fromStream bool) {
	switch {
	case running > 0:
		return running, "the rate PipeWire runs at", false
	case s.force > 0:
		return s.force, "the rate PipeWire is forced to", false
	case len(s.allowed) == 1 && s.allowed[0] > 0:
		return s.allowed[0], "the only rate PipeWire allows", false
	case stream > 0:
		return stream, "the stream's rate", true
	}
	return 44100, "as the stream gives no rate", true
}

// pipewireClock reads the clock with pw-top and pw-metadata. Either may be
// missing, or too old for pw-top's batch mode: the rate is then unknown and
// the recording goes on at the next rate recordRate finds.
type pipewireClock struct{}

func (pipewireClock) running(ctx context.Context, driver int) int {
	if driver <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, clockTimeout)
	defer cancel()
	// The first round has no rates yet.
	out, err := proc.CommandContext(ctx, "pw-top", "--batch-mode", "--iterations", "2").Output()
	if err != nil {
		return 0
	}
	return driverRate(out, driver)
}

func (pipewireClock) settings(ctx context.Context) clockSettings {
	ctx, cancel := context.WithTimeout(ctx, clockTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, "pw-metadata", "-n", "settings").Output()
	if err != nil {
		return clockSettings{}
	}
	return parseClockSettings(out)
}

// driverRate is the RATE pw-top's last round gives the driver, 0 when the
// driver is idle or absent. A driver's RATE is its graph's; a follower's is
// the rate it asked for. The header places the columns.
func driverRate(top []byte, driver int) int {
	idCol, rateCol := -1, -1
	id := strconv.Itoa(driver)
	rate := 0
	for _, line := range strings.Split(string(top), "\n") {
		f := strings.Fields(line)
		if i, r := slices.Index(f, "ID"), slices.Index(f, "RATE"); i >= 0 && r >= 0 {
			idCol, rateCol = i, r
			continue
		}
		if idCol < 0 || len(f) <= max(idCol, rateCol) || f[idCol] != id {
			continue
		}
		rate, _ = strconv.Atoi(f[rateCol])
	}
	return rate
}

// metadataEntry is one line of pw-metadata's output:
//
//	update: id:0 key:'clock.force-rate' value:'0' type:''
var metadataEntry = regexp.MustCompile(`key:'([^']*)' value:'([^']*)'`)

func parseClockSettings(out []byte) clockSettings {
	var s clockSettings
	for _, m := range metadataEntry.FindAllStringSubmatch(string(out), -1) {
		switch m[1] {
		case "clock.force-rate":
			s.force, _ = strconv.Atoi(m[2])
		case "clock.allowed-rates":
			s.allowed = nil
			for _, r := range strings.FieldsFunc(m[2], func(c rune) bool { return c < '0' || c > '9' }) {
				if n, err := strconv.Atoi(r); err == nil && n > 0 {
					s.allowed = append(s.allowed, n)
				}
			}
		}
	}
	return s
}
