// Command spytify-ui is the desktop client of spytifyd: a window, a tray
// menu, and nothing that records by itself. It starts the daemon when it
// is not running and a player is configured.
package main

import (
	"context"
	"time"

	"fyne.io/fyne/v2/app"

	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/internal/ui"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
)

func main() {
	target.LoadDotEnv()
	env := target.Load()

	fa := app.NewWithID("org.spytify.ui")
	fa.SetIcon(ui.Logo)
	m := model.New()
	l := link.New(env.SocketPath, m)
	// With a player to record, the app starts spytifyd itself; with none,
	// it asks for one first.
	auto := len(env.Target.ProcessNames) > 0
	w := ui.New(fa, l, m, ui.Options{StateDir: env.StateDir, Socket: env.SocketPath, AutoStart: auto})

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
