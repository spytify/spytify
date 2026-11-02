package ui

// trayBus is what hasTrayHost asks the session bus; tests answer for it.
type trayBus interface {
	// watcherOwned says whether a program owns
	// org.kde.StatusNotifierWatcher.
	watcherOwned() (bool, error)
	// hostRegistered is the watcher's IsStatusNotifierHostRegistered.
	hostRegistered() (bool, error)
}

// hasTrayHost says whether a Linux desktop shows tray icons. The toolkit's
// tray icon is a StatusNotifierItem: it registers with the
// StatusNotifierWatcher, and a host, the panel that draws the icons,
// registers there too. KDE, Xfce, Cinnamon and Ubuntu's GNOME run both;
// GNOME elsewhere runs neither without the AppIndicator extension. A
// watcher that cannot say whether it has a host is taken to have one: the
// icon registers with it all the same.
func hasTrayHost(b trayBus) bool {
	owned, err := b.watcherOwned()
	if err != nil || !owned {
		return false
	}
	host, err := b.hostRegistered()
	return err != nil || host
}
