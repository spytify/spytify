package crash

import (
	"bufio"
	"bytes"
	"maps"
	"os"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"time"
)

// vitalsEvery is how often Vitals writes.
const vitalsEvery = time.Minute

// Vitals writes VitalsOf(path) now and every minute while the program
// runs: how big the program is, in memory, threads and handles, and how
// much memory the computer has left. A crash report then says what they
// were just before the crash, as when the runtime could not get memory or
// a handle from the system. extra, when not nil, adds the program's own
// counts, such as how many rows the app lists, read at each write from
// the goroutine that writes. Numbers only, no names.
func Vitals(path string, extra func() map[string]string) {
	started := time.Now()
	go func() {
		tick := time.NewTicker(vitalsEvery)
		defer tick.Stop()
		for {
			_ = writeVitals(VitalsOf(path), started, time.Now(), extra)
			<-tick.C
		}
	}()
}

// vital is one line of a vitals file: a count, or bytes.
type vital struct {
	key   string
	value uint64
}

// writeVitals writes the vitals, then extra's in the order of their keys,
// to file through a file beside it, so that a crash while it writes leaves
// the previous ones whole.
func writeVitals(file string, started, now time.Time, extra func() map[string]string) error {
	var b bytes.Buffer
	for _, v := range vitals(started, now) {
		b.WriteString(v.key + "=" + strconv.FormatUint(v.value, 10) + "\n")
	}
	if extra != nil {
		more := extra()
		for _, k := range slices.Sorted(maps.Keys(more)) {
			// Each is one line, split at its first "=".
			if v := more[k]; !strings.ContainsAny(k, "=\n") && !strings.Contains(v, "\n") {
				b.WriteString(k + "=" + v + "\n")
			}
		}
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// goVitals are the runtime's own counts, read from runtime/metrics.
var goVitals = []struct{ key, metric string }{
	{"go.total_bytes", "/memory/classes/total:bytes"},
	{"go.heap_bytes", "/memory/classes/heap/objects:bytes"},
	{"go.goroutines", "/sched/goroutines:goroutines"},
	{"go.gc_cycles", "/gc/cycles/total:gc-cycles"},
}

// vitals are the runtime's counts, then the system's (osVitals): what
// cannot be read is left out.
func vitals(started, now time.Time) []vital {
	v := []vital{
		{"written_at", uint64(now.Unix())},
		{"uptime_s", uint64(now.Sub(started) / time.Second)},
	}
	samples := make([]metrics.Sample, len(goVitals))
	for i, g := range goVitals {
		samples[i].Name = g.metric
	}
	metrics.Read(samples)
	for i, s := range samples {
		if s.Value.Kind() == metrics.KindUint64 {
			v = append(v, vital{goVitals[i].key, s.Value.Uint64()})
		}
	}
	return append(v, osVitals()...)
}

// ReadVitals reads the vitals the run that wrote the crash log at path
// left, as written: nil when there are none.
func ReadVitals(path string) map[string]string {
	b, err := os.ReadFile(VitalsOf(path))
	if err != nil {
		return nil
	}
	var out map[string]string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || k == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[k] = v
	}
	return out
}

// System is the computer's memory and handles now, under the vitals' keys:
// how much it had left when a program could not get its own.
func System() map[string]string {
	facts := map[string]string{}
	for _, v := range osVitals() {
		if strings.HasPrefix(v.key, "system.") {
			facts[v.key] = strconv.FormatUint(v.value, 10)
		}
	}
	return facts
}
