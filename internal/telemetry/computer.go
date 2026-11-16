package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

var computer struct {
	mu   sync.Mutex
	read bool
	id   string
}

// machineID reads the system's machine ID (machineID_*.go); tests count
// the reads.
var machineID = systemMachineID

// computerID names this computer without telling which one it is: its
// machine ID hashed with the app's name, the same at each launch and
// after a reinstall, and of no use to another app. Empty when the system
// has none. It is read the first time a report needs it, only once the bug
// tracker is on.
func computerID() string {
	computer.mu.Lock()
	defer computer.mu.Unlock()
	if !computer.read {
		computer.read = true
		if id, err := machineID(); err == nil {
			computer.id = protect(id)
		}
	}
	return computer.id
}

// protect is denisbrodbeck/machineid's ProtectedID("spytify"): the
// HMAC-SHA256 of the app's name keyed with the machine ID, so that a
// computer keeps the ID it had in earlier versions.
func protect(id string) string {
	mac := hmac.New(sha256.New, []byte(id))
	mac.Write([]byte("spytify"))
	return hex.EncodeToString(mac.Sum(nil))
}
