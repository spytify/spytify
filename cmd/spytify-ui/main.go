// Command spytify-ui is the desktop client of spytifyd: a window, a tray
// menu, and nothing that records by itself. It starts the daemon when it
// is not running and a player is configured.
package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2/app"

	"github.com/spytify/spytify/internal/crash"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui"
	"github.com/spytify/spytify/internal/ui/instance"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
)

func main() {
	target.LoadDotEnv()
	env := target.Load()

	// Opened again while it runs, Spytify shows the window it has, hidden
	// to the tray or not, rather than starting a second app. Should the
	// socket fail, the app runs all the same.
	inst, err := instance.Claim(strings.TrimSuffix(env.SocketPath, ".sock") + "-ui.sock")
	if errors.Is(err, instance.ErrRunning) {
		return
	}

	// A crash of the app leaves its trace for the next launch to send.
	crash.Keep(filepath.Join(env.StateDir, "spytify-ui.crash.log"))

	fa := app.NewWithID("org.spytify.ui")
	fa.SetIcon(ui.Logo)
	m := model.New()
	l := link.New(env.SocketPath, m)
	tel := telemetry.New(telemetry.Options{Prefs: fa.Preferences()})
	// At quit, what is queued leaves within 2 s or is dropped.
	defer tel.Close()
	// With a player to record, the app starts spytifyd itself; with none,
	// it asks for one first.
	auto := len(env.Target.ProcessNames) > 0
	w := ui.New(fa, l, m, ui.Options{StateDir: env.StateDir, Socket: env.SocketPath, AutoStart: auto,
		Telemetry: tel, Privacy: privacySettings(env.ConfigPath)})
	if inst != nil {
		defer inst.Close()
		go inst.Serve(w.Reopen)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	go func() {
		// Give an already running daemon a moment to answer first.
		time.Sleep(800 * time.Millisecond)
		if m.Snapshot().Conn != model.Connected && auto {
			w.StartDaemon()
		}
	}()
	w.ShowAndRun()
}

// privacySettings are the privacy settings of config.toml, which the app
// needs before the recorder answers, or when it does not.
func privacySettings(path string) map[string]any {
	v := settings.Defaults()
	if store, err := settings.Open(path); err == nil {
		v = store.Values()
	}
	return map[string]any{"privacy.bug_tracker": v.Bool("privacy.bug_tracker")}
}
