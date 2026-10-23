//go:build linux

package platform

import (
	"os/exec"

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
