package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/spytify/spytify/internal/proc"
)

// protectedID is machineid.ProtectedID with ioreg started by its full
// path: the library starts "ioreg" by name, so the first one on PATH
// would run. The UUID is read and hashed as the library does, so a Mac
// keeps the ID it had.
func protectedID(appID string) (string, error) {
	out, err := proc.Command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		if parts := strings.SplitAfter(line, `" = "`); len(parts) == 2 {
			id := strings.TrimSpace(strings.TrimRight(parts[1], `"`))
			mac := hmac.New(sha256.New, []byte(id))
			mac.Write([]byte(appID))
			return hex.EncodeToString(mac.Sum(nil)), nil
		}
	}
	return "", errors.New("no IOPlatformUUID in ioreg's output")
}
