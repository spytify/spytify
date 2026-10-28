//go:build linux

package capture

import (
	"slices"
	"testing"
)

// pw-top --batch-mode --iterations 2 under PipeWire 1.4.2, pw-play's
// 44.1 kHz stream (48) on a null sink (33) whose graph runs at 48 kHz. The
// first round has no rates; a follower's RATE is the rate it asked for.
const top = `S   ID  QUANT   RATE    WAIT    BUSY   W/Q   B/Q  ERR FORMAT           NAME
C   29      0      0    ---     ---   ---   ---     0                  Dummy-Driver
C   30      0      0    ---     ---   ---   ---     0                  Freewheel-Driver
C   33      0      0    ---     ---   ---   ---     0                  spytify-null
C   48      0      0    ---     ---   ---   ---     0                  pw-play
S   ID  QUANT   RATE    WAIT    BUSY   W/Q   B/Q  ERR FORMAT           NAME
S   29      0      0    ---     ---   ---   ---     0                  Dummy-Driver
S   30      0      0    ---     ---   ---   ---     0                  Freewheel-Driver
R   33   2048  48000  10.4us  12.1us  0.00  0.00    0     F32P 2 48000 spytify-null
R   48   4410  44100  57.5us 125.9us  0.00  0.00    0    S16LE 2 44100  + pw-play
`

// The same graph idle: the driver runs at no rate.
const topIdle = `S   ID  QUANT   RATE    WAIT    BUSY   W/Q   B/Q  ERR FORMAT           NAME
C   29      0      0    ---     ---   ---   ---     0                  Dummy-Driver
C   33      0      0    ---     ---   ---   ---     0                  spytify-null
S   ID  QUANT   RATE    WAIT    BUSY   W/Q   B/Q  ERR FORMAT           NAME
S   29      0      0    ---     ---   ---   ---     0                  Dummy-Driver
I   33      0      0   0.0us   0.0us  ???   ???     0     F32P 2 48000 spytify-null
`

// Columns laid out otherwise, here without the state column: the header
// places them.
const topNoState = `  ID  QUANT   RATE    WAIT    BUSY   W/Q   B/Q  ERR  NAME
  33   1024  44100  10.4us  12.1us  0.00  0.00    0  alsa_output.usb
  48   4410  44100  57.5us 125.9us  0.00  0.00    0   + player
`

func TestDriverRateIsTheDriversLastRound(t *testing.T) {
	for _, c := range []struct {
		name   string
		top    string
		driver int
		want   int
	}{
		{"running", top, 33, 48000},
		{"follower", top, 48, 44100},
		{"absent", top, 12, 0},
		{"idle", topIdle, 33, 0},
		{"no state column", topNoState, 33, 44100},
		{"no header", "R   33   2048  48000\n", 33, 0},
		{"empty", "", 33, 0},
	} {
		if got := driverRate([]byte(c.top), c.driver); got != c.want {
			t.Errorf("%s: driver %d at %d Hz, want %d", c.name, c.driver, got, c.want)
		}
	}
}

// pw-metadata -n settings under PipeWire 1.4.2 with its own rates.
const settings = `Found "settings" metadata 31
update: id:0 key:'log.level' value:'2' type:''
update: id:0 key:'clock.rate' value:'48000' type:''
update: id:0 key:'clock.allowed-rates' value:'[ 48000 ]' type:''
update: id:0 key:'clock.quantum' value:'1024' type:''
update: id:0 key:'clock.min-quantum' value:'32' type:''
update: id:0 key:'clock.max-quantum' value:'2048' type:''
update: id:0 key:'clock.force-quantum' value:'0' type:''
update: id:0 key:'clock.force-rate' value:'0' type:''
`

func TestParseClockSettings(t *testing.T) {
	for _, c := range []struct {
		name string
		out  string
		want clockSettings
	}{
		{"default", settings, clockSettings{allowed: []int{48000}}},
		{"allowed", `update: id:0 key:'clock.allowed-rates' value:'[ 44100, 48000 ]' type:''`, clockSettings{allowed: []int{44100, 48000}}},
		{"forced", `update: id:0 key:'clock.force-rate' value:'96000' type:''` + "\n" + `update: id:0 key:'clock.allowed-rates' value:'[ 44100 48000 ]' type:''`,
			clockSettings{force: 96000, allowed: []int{44100, 48000}}},
		{"none", "", clockSettings{}},
	} {
		got := parseClockSettings([]byte(c.out))
		if got.force != c.want.force || !slices.Equal(got.allowed, c.want.allowed) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestRecordRate(t *testing.T) {
	for _, c := range []struct {
		name       string
		stream     int
		running    int
		s          clockSettings
		want       int
		why        string
		fromStream bool
	}{
		{"running", 44100, 48000, clockSettings{force: 96000, allowed: []int{96000}}, 48000, "the rate PipeWire runs at", false},
		{"forced", 44100, 0, clockSettings{force: 96000, allowed: []int{48000}}, 96000, "the rate PipeWire is forced to", false},
		{"one allowed", 44100, 0, clockSettings{allowed: []int{48000}}, 48000, "the only rate PipeWire allows", false},
		{"several allowed", 44100, 0, clockSettings{allowed: []int{44100, 48000}}, 44100, "the stream's rate", true},
		{"no settings", 96000, 0, clockSettings{}, 96000, "the stream's rate", true},
		{"nothing", 0, 0, clockSettings{}, 44100, "as the stream gives no rate", true},
	} {
		if got, why, fromStream := recordRate(c.stream, c.running, c.s); got != c.want || why != c.why || fromStream != c.fromStream {
			t.Errorf("%s: %d Hz, %s, from the stream %v; want %d Hz, %s, %v", c.name, got, why, fromStream, c.want, c.why, c.fromStream)
		}
	}
}
