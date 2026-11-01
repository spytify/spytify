package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	snWatcher     = "org.kde.StatusNotifierWatcher"
	snWatcherPath = dbus.ObjectPath("/StatusNotifierWatcher")
	// trayBusTimeout bounds each question: a watcher that hangs must not
	// hold the answer back.
	trayBusTimeout = 2 * time.Second
)

// watchTrayHost tells changed, from its own goroutine, whether the desktop
// shows tray icons (hasTrayHost): once, then each time a watcher or a host
// comes or goes, as when the AppIndicator extension is turned on or off.
func watchTrayHost(changed func(present bool)) {
	go func() {
		// A connection of its own: the tray's is shared, and its reader
		// stops at a signal it does not expect.
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			changed(false)
			return
		}
		defer conn.Close()
		signals := make(chan *dbus.Signal, 8)
		conn.Signal(signals)
		watching := true
		for _, rule := range [][]dbus.MatchOption{
			{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, snWatcher)},
			{dbus.WithMatchInterface(snWatcher), dbus.WithMatchMember("StatusNotifierHostRegistered")},
			{dbus.WithMatchInterface(snWatcher), dbus.WithMatchMember("StatusNotifierHostUnregistered")},
		} {
			watching = watching && conn.AddMatchSignal(rule...) == nil
		}
		bus := sessionTray{conn}
		changed(hasTrayHost(bus))
		if !watching {
			return
		}
		for range signals {
			changed(hasTrayHost(bus))
		}
	}()
}

// sessionTray asks the session bus.
type sessionTray struct{ conn *dbus.Conn }

func (s sessionTray) watcherOwned() (owned bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), trayBusTimeout)
	defer cancel()
	err = s.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, snWatcher).Store(&owned)
	return owned, err
}

func (s sessionTray) hostRegistered() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), trayBusTimeout)
	defer cancel()
	var v dbus.Variant
	err := s.conn.Object(snWatcher, snWatcherPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart,
		snWatcher, "IsStatusNotifierHostRegistered").Store(&v)
	if err != nil {
		return false, err
	}
	host, ok := v.Value().(bool)
	if !ok {
		return false, fmt.Errorf("IsStatusNotifierHostRegistered is a %s", v.Signature())
	}
	return host, nil
}
