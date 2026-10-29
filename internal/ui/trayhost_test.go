package ui

import (
	"errors"
	"testing"
)

// fakeTrayBus answers hasTrayHost's questions as a session bus would.
type fakeTrayBus struct {
	owned, host       bool
	ownedErr, hostErr error
}

func (b fakeTrayBus) watcherOwned() (bool, error)   { return b.owned, b.ownedErr }
func (b fakeTrayBus) hostRegistered() (bool, error) { return b.host, b.hostErr }

// The tray icon shows where a StatusNotifierWatcher runs with a host
// registered: KDE, Xfce, Cinnamon, Ubuntu's GNOME. GNOME without the
// AppIndicator extension runs no watcher; a watcher alone draws nothing.
func TestATrayHostIsAWatcherWithAHost(t *testing.T) {
	gone := errors.New("org.freedesktop.DBus.Error.ServiceUnknown")
	for _, c := range []struct {
		name string
		bus  fakeTrayBus
		want bool
	}{
		{"no session bus", fakeTrayBus{ownedErr: errors.New("dbus: connection closed by user")}, false},
		{"no watcher", fakeTrayBus{hostErr: gone}, false},
		{"a watcher with no host", fakeTrayBus{owned: true}, false},
		{"a watcher with a host", fakeTrayBus{owned: true, host: true}, true},
		{"a watcher that cannot say", fakeTrayBus{owned: true, hostErr: errors.New("org.freedesktop.DBus.Error.UnknownProperty")}, true},
	} {
		if got := hasTrayHost(c.bus); got != c.want {
			t.Errorf("%s: tray host %v, want %v", c.name, got, c.want)
		}
	}
}
