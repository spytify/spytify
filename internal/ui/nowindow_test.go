package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/opengl"
)

func TestNoWindowIsReportedOnceTheChoiceWasShown(t *testing.T) {
	noGL := fmt.Errorf("%w: none of the screen's 12 pixel formats is drawn by a driver", opengl.ErrNoDriver)
	for _, c := range []struct {
		name     string
		seen, on bool
		sent     bool
	}{
		{"shown and on", true, true, true},
		{"never shown", false, true, false},
		{"off", true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakesentry.New(t)
			fa := test.NewTempApp(t)
			fa.Preferences().SetBool(privacySeenKey, c.seen)
			tel := telemetry.New(telemetry.Options{DSN: srv.DSN(), Prefs: fa.Preferences(), ProblemSampleRate: 1})
			t.Cleanup(tel.Close)
			NoWindow(fa, tel, map[string]any{trackerKey: c.on}, noGL)
			tel.Flush(2 * time.Second)
			if !c.sent {
				time.Sleep(200 * time.Millisecond)
				if n := srv.Requests(); n != 0 {
					t.Fatalf("%d requests reached Sentry", n)
				}
				return
			}
			ev := srv.Wait(t, "event", 1)[0].JSON()
			if ev["message"] != "blocked: no_opengl" {
				t.Errorf("message %v", ev["message"])
			}
			ctx, _ := ev["contexts"].(map[string]any)
			problem, _ := ctx["problem"].(map[string]any)
			if detail, _ := problem["detail"].(string); !strings.Contains(detail, "12 pixel formats") {
				t.Errorf("detail %q, want what the check found", detail)
			}
		})
	}
}
