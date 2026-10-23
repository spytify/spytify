//go:build linux

package platform

import (
	"os/exec"
	"regexp"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// New returns PipeWire (or PulseAudio) capture and MPRIS metadata.
func (b Backends) New(t target.Target) (capture.Capture, meta.NowPlaying, error) {
	c, err := capture.NewLinux(b.logf)
	if err != nil {
		return nil, nil, err
	}
	m := meta.NewMPRIS()
	m.Target(t)
	return c, m, nil
}

// Probe checks what the Linux backends need.
func (b Backends) Probe() []proto.Check {
	var checks []proto.Check
	tools := func(names ...string) bool {
		for _, n := range names {
			if _, err := exec.LookPath(n); err != nil {
				return false
			}
		}
		return true
	}
	switch {
	case tools("pw-record", "pw-dump"):
		checks = append(checks, proto.Check{Name: "capture", OK: true, Detail: "PipeWire (pw-record)"})
		if tools("pw-metadata") {
			checks = append(checks, rateCheck())
		}
	case tools("parec", "pactl"):
		checks = append(checks, proto.Check{Name: "capture", OK: true, Detail: "PulseAudio (parec)"})
	default:
		checks = append(checks, proto.Check{Name: "capture", OK: false, Detail: "install pipewire-bin (pw-record, pw-dump) or pulseaudio-utils"})
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		checks = append(checks, proto.Check{Name: "D-Bus session bus", OK: false, Detail: err.Error()})
	} else {
		conn.Close()
		checks = append(checks, proto.Check{Name: "D-Bus session bus", OK: true})
	}
	return append(checks, keyringCheck())
}

var allowedRates = regexp.MustCompile(`key:'clock\.allowed-rates' value:'\[([^\]]*)\]'`)

// rateCheck warns when PipeWire can only run at 48 kHz: 44.1 kHz players
// are then resampled before they are recorded.
func rateCheck() proto.Check {
	out, err := exec.Command("pw-metadata", "-n", "settings", "0").Output()
	m := allowedRates.FindSubmatch(out)
	if err != nil || m == nil {
		return proto.Check{Name: "PipeWire sample rates", OK: true, Detail: "allowed rates unknown"}
	}
	rates := strings.Fields(strings.ReplaceAll(string(m[1]), ",", " "))
	for _, r := range rates {
		if r == "44100" {
			return proto.Check{Name: "PipeWire sample rates", OK: true, Detail: strings.Join(rates, ", ") + " Hz"}
		}
	}
	return proto.Check{Name: "PipeWire sample rates", OK: false, Detail: "only " + strings.Join(rates, ", ") +
		" Hz: 44.1 kHz audio is resampled before it is recorded; set default.clock.allowed-rates = [ 44100 48000 ]"}
}
