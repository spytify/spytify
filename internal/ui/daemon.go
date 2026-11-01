package ui

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/service"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// stopDaemon shuts spytifyd down as the app quits, unless it runs as a
// login service (spytify-cli service install), which keeps it alive.
func (a *App) stopDaemon() {
	if daemonIsService() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.link.Do(ctx, proto.MethodCoreShutdown, nil, nil)
}

// daemonIsService reports whether spytifyd is installed as a login service.
func daemonIsService() bool {
	daemon, err := link.FindDaemon()
	if err != nil {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	p, err := service.For(runtime.GOOS, daemon, home, os.Getuid())
	return err == nil && service.Installed(p)
}

// StartDaemon starts spytifyd and reports why when it stops at once.
func (a *App) StartDaemon() {
	fyne.Do(func() {
		a.starting, a.startErr = true, ""
		a.tel.Breadcrumb("app", "starting the recorder", nil)
		if a.launch.started.IsZero() {
			a.launch.started = time.Now()
		}
		a.render()
	})
	go func() {
		err := a.link.StartDaemon()
		if err == nil {
			// Started: it answers in a moment, or this says it did not.
			time.Sleep(startAnswerTimeout)
			err = errors.New("spytifyd started but does not answer; see its log in Logs")
			if a.m.Snapshot().Conn == model.Connected {
				return
			}
		}
		fyne.Do(func() { a.startFailed(err) })
	}()
}

// startAnswerTimeout is how long a started spytifyd has to answer.
const startAnswerTimeout = 15 * time.Second

// startFailed shows the offline screen with why the recorder did not start.
func (a *App) startFailed(err error) {
	a.starting, a.startErr = false, err.Error()
	a.launchFailed(err)
	a.render()
}
