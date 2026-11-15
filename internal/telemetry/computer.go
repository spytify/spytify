package telemetry

import "sync"

var computer struct {
	once sync.Once
	id   string
}

// computerID names this computer without telling which one it is: its
// machine ID hashed with the app's name, the same at each launch and
// after a reinstall, and of no use to another app. Empty when the system
// has none.
func computerID() string {
	computer.once.Do(func() { computer.id, _ = protectedID("spytify") })
	return computer.id
}
