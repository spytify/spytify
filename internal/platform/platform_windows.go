//go:build windows

package platform

import (
	"fmt"
	"os/exec"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// New returns WASAPI process loopback capture (through spytify-tap.exe) and
// the media session API for what plays.
func (b Backends) New(t target.Target) (capture.Capture, meta.NowPlaying, error) {
	if build := capture.WindowsBuild(); !capture.LoopbackSupported(build) {
		return nil, nil, fmt.Errorf("Windows build %d has no process loopback capture; Windows 10 version 2004 or later is needed", build)
	}
	c, err := capture.NewWindows(b.TapHelper, target.FindProcess, b.logf)
	if err != nil {
		return nil, nil, err
	}
	m := meta.NewSMTC()
	m.Logf = b.logf
	m.Target(t)
	if helper, err := capture.FindTapHelper(b.TapHelper); err == nil {
		m.Mixer = mixer{helper: helper, t: t}
	}
	return c, m, nil
}

// mixer reaches the player's audio sessions through spytify-tap.exe.
type mixer struct {
	helper string
	t      target.Target
}

func (x mixer) pid() (int, error) {
	p, ok := target.FindProcess(x.t)
	if !ok {
		return 0, meta.ErrNoPlayer
	}
	return p.PID, nil
}

func (x mixer) Volume() (float64, error) {
	pid, err := x.pid()
	if err != nil {
		return 0, err
	}
	return capture.SessionVolume(x.helper, pid)
}

func (x mixer) SetVolume(v float64) error {
	pid, err := x.pid()
	if err != nil {
		return err
	}
	return capture.SetSessionVolume(x.helper, pid, v)
}

// Probe checks what the Windows backends need.
func (b Backends) Probe() []proto.Check {
	build := capture.WindowsBuild()
	checks := []proto.Check{{Name: "Windows 10 2004 or later", OK: capture.LoopbackSupported(build), Detail: fmt.Sprintf("build %d", build)}}
	if p, err := capture.FindTapHelper(b.TapHelper); err != nil {
		checks = append(checks, proto.Check{Name: "capture helper", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, proto.Check{Name: "capture helper", OK: true, Detail: p})
	}
	if p, err := exec.LookPath("powershell.exe"); err != nil {
		checks = append(checks, proto.Check{Name: "Windows PowerShell", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, proto.Check{Name: "Windows PowerShell", OK: true, Detail: p + "; reads what plays through the media session API"})
	}
	return checks
}
