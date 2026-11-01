package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// The Sentry bug tracker, all or nothing (package telemetry). On, the app
// sends its crashes, the problems it runs into with the steps that led
// there, its launches and timings, counts of the tracks saved and skipped
// and how they were cut, and, on a click, the checks that fail. Off, it
// sends nothing and asks nothing.

var trackerKey = settings.PrivacyBugTracker.String()

const (
	// pendingKey holds privacy choices made before the recorder answered,
	// sent to it once it does.
	pendingKey = "privacy-pending"
	// privacySeenKey is set once the bug tracker choice was shown.
	privacySeenKey = "bug-tracker-choice-seen"
	trackerLabel   = "Sentry bug tracker: send crashes, errors, traces and recording counts"
	optedOut       = "You opted out of the Sentry bug tracker"
)

// privacy is a privacy setting as the app knows it: a choice not sent to
// the recorder yet, the recorder's value, or the config file's at launch.
func (a *App) privacy(key string) any {
	if v, ok := a.pendingPrivacy()[key]; ok {
		return v
	}
	if v, ok := a.last.Settings[key]; ok {
		return v
	}
	return a.opts.Privacy[key]
}

func (a *App) pendingPrivacy() map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(a.fa.Preferences().String(pendingKey)), &m)
	return m
}

// setPrivacy applies a privacy choice now and sends it to the recorder,
// at once or when it answers.
func (a *App) setPrivacy(key string, v any) {
	p := a.pendingPrivacy()
	p[key] = v
	b, _ := json.Marshal(p)
	a.fa.Preferences().SetString(pendingKey, string(b))
	if key == trackerKey {
		on, _ := v.(bool)
		a.tel.SetEnabled(on && a.shown)
		a.tel.Breadcrumb("settings", trackerKey, map[string]any{"value": on})
	}
	if a.last.Conn == model.Connected {
		a.flushPrivacy()
	}
}

// flushPrivacy sends the choices made while the recorder was away.
func (a *App) flushPrivacy() {
	p := a.pendingPrivacy()
	if len(p) == 0 {
		return
	}
	a.calls.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if a.link.Do(ctx, proto.MethodSettingsSet, proto.SettingsSetParams{Values: p}, nil) == nil {
			fyne.Do(func() {
				// Only what was sent: a choice made meanwhile stays.
				now := a.pendingPrivacy()
				for k, v := range p {
					if fmt.Sprint(now[k]) == fmt.Sprint(v) {
						delete(now, k)
					}
				}
				b, _ := json.Marshal(now)
				a.fa.Preferences().SetString(pendingKey, string(b))
			})
		}
	})
}

// renderPrivacy follows the connection and the settings: the bug tracker
// starts or stops, a crash left behind leaves, the launch is timed and the
// checks noted. Nothing starts before the first-run notice had its say.
func (a *App) renderPrivacy(s model.State) {
	connected := s.Conn == model.Connected
	if connected != a.wasConnected {
		a.wasConnected = connected
		if connected {
			a.launch.connected = time.Now()
			a.flushPrivacy()
		}
		a.tel.Breadcrumb("app", map[bool]string{true: "recorder connected", false: "recorder gone"}[connected], nil)
		// A recorder that went away may have crashed; one that came back
		// moved its trace aside.
		if a.shown {
			a.checkCrashes()
		}
	}
	on, _ := a.privacy(trackerKey).(bool)
	a.tel.SetEnabled(on && a.shown)
	if !a.shown || !s.HaveInfo {
		return
	}
	if !a.launch.done {
		a.launch.done = true
		a.launchReady(s)
	}
	if checks := fmt.Sprint(s.Info.Checks); checks != a.checksNoted {
		a.checksNoted = checks
		a.tel.Breadcrumb("checks", fmt.Sprintf("%d of %d pass", len(s.Info.Checks)-len(failingNames(s.Info.Checks)), len(s.Info.Checks)),
			map[string]any{"failing": strings.Join(failingNames(s.Info.Checks), ", ")})
	}
}

// askPrivacy shows, once, the bug tracker choice to someone who saw the
// first-run notice before it had one.
func (a *App) askPrivacy(then func()) {
	prefs := a.fa.Preferences()
	if !a.tel.Available() || prefs.Bool(privacySeenKey) {
		then()
		return
	}
	tracker := a.trackerCheck()
	body := widget.NewLabel("Spytify can send its crashes, errors with a picture of its window, traces and recording counts to its developers through Sentry, to help them fix issues.")
	body.Wrapping = fyne.TextWrapWord
	showDialog(a.win, "Help fix issues", container.NewVBox(body, tracker), "OK", "", fyne.NewSize(460, 220), func(bool) {
		prefs.SetBool(privacySeenKey, true)
		a.setPrivacy(trackerKey, tracker.Checked)
		then()
	})
}

// trackerCheck is the bug tracker box, as the setting is now.
func (a *App) trackerCheck() *check {
	c := newCheck(trackerLabel)
	on, _ := a.privacy(trackerKey).(bool)
	c.SetChecked(on)
	return c
}
