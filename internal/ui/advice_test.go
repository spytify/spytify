package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// lowRates is the advice PipeWire's default gives: 48 kHz only.
var lowRates = proto.Check{ID: proto.CheckSampleRates, Name: "PipeWire sample rates", Advice: true, Args: []string{"48000"}, Detail: "only 48000 Hz: 44.1 kHz tracks are resampled once. To record them unchanged, " +
	"put context.properties = { default.clock.allowed-rates = [ 44100 48000 ] } in a .conf file under ~/.config/pipewire/pipewire.conf.d/, then log out and back in"}

// linuxInfo is info on Linux, where PipeWire captures.
func linuxInfo() proto.CoreInfoResult {
	in := info()
	in.Platform = "linux/amd64"
	in.Checks[3] = proto.Check{ID: proto.CheckCapture, Name: "capture", OK: true, Detail: "PipeWire (pw-record)", Args: []string{"pipewire"}}
	return in
}

// Advice that fails sends nobody to the diagnostics nor to the developers:
// recording works. A check that does fail beside it is sent alone.
func TestAdviceNeedsNoAttention(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	withAdvice := func(checks ...proto.Check) {
		in := linuxInfo()
		in.Checks = append(append(in.Checks, checks...), lowRates)
		m.SetInfo(in)
		a.render()
	}
	withAdvice()
	if contains(texts(a.win.Content()), lang.OpenDiagnostics) {
		t.Error("advice sends the user to the diagnostics")
	}
	if b := a.screens[tabLogs].(*logsScreen).diagnose; b.Importance != widget.LowImportance {
		t.Errorf("advice gives the diagnostics button importance %v", b.Importance)
	}
	d := diagnostics(t, a)
	if all := texts(d); !contains(all, fmt.Sprintf(lang.ChecksPass, 5)) || !contains(all, "PipeWire sample rates") || !contains(all, "resampled once") {
		t.Errorf("the diagnostics: %v", all)
	}
	if len(buttons(d, lang.SendToDevelopers)) != 0 {
		t.Error("advice offers to send it to the developers")
	}
	snapshot(t, a, "logs-advice")

	withAdvice(proto.Check{ID: proto.CheckFFmpeg, Name: "ffmpeg found", Detail: "not on PATH"})
	if all := texts(overlay(t, a)); !contains(all, fmt.Sprintf(lang.ChecksNeedAttention, 1, 6)) {
		t.Errorf("a check fails beside the advice: %v", all)
	}
	tapButton(t, overlay(t, a), lang.SendToDevelopers)
	tapButton(t, overlay(t, a), lang.Send)
	if ev := srv.Wait(t, "event", 1)[0].JSON(); ev["message"] != "stuck: ffmpeg found" {
		t.Errorf("sent %v", ev["message"])
	}
}

// The launch names the advice apart from the checks that fail, so Sentry
// counts both.
func TestTheLaunchNamesTheAdvice(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	in := linuxInfo()
	in.Checks = append(in.Checks, lowRates)
	m.SetConn(model.Connected, "")
	m.SetInfo(in)
	values := defaults()
	values[trackerKey] = true
	m.SetSettings(values)
	a.render()
	a.tel.Flush(2 * time.Second)
	if l := launchLog(t, srv); !strings.Contains(l.Attrs["advice"], lowRates.Name) || strings.Contains(l.Attrs["failing"], lowRates.Name) {
		t.Errorf("advice %q, failing %q", l.Attrs["advice"], l.Attrs["failing"])
	}
}
