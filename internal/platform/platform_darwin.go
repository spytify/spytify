//go:build darwin

package platform

import (
	"os/exec"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// New returns Core Audio process-tap capture (through spytify-tap) and
// AppleScript metadata.
func (b Backends) New(t target.Target) (capture.Capture, meta.NowPlaying, error) {
	c, err := capture.NewDarwin(b.TapHelper, b.mute(), b.logf)
	if err != nil {
		return nil, nil, err
	}
	m := meta.NewJXA()
	m.Logf = b.logf
	m.Target(t)
	return c, m, nil
}

// Probe checks what the macOS backends need.
func (b Backends) Probe() []proto.Check {
	var checks []proto.Check
	v := capture.MacOSVersion()
	checks = append(checks, proto.Check{ID: proto.CheckMacOS, Name: "macOS 14.2 or later", OK: capture.TapSupported(v), Detail: "macOS " + v, Args: []string{v}})
	if p, err := capture.FindTapHelper(b.TapHelper); err != nil {
		checks = append(checks, proto.Check{ID: proto.CheckTapHelper, Name: "capture helper", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, proto.Check{ID: proto.CheckTapHelper, Name: "capture helper", OK: true, Detail: p + "; macOS asks once for System Audio Recording permission", Args: []string{p}})
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		checks = append(checks, proto.Check{ID: proto.CheckAppleScript, Name: "AppleScript", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, proto.Check{ID: proto.CheckAppleScript, Name: "AppleScript", OK: true, Detail: "macOS asks once to let spytify control the player"})
	}
	return checks
}
