//go:build !darwin

package telemetry

import "github.com/denisbrodbeck/machineid"

// protectedID reads files on Linux and the registry on Windows: no program
// is started.
func protectedID(appID string) (string, error) {
	return machineid.ProtectedID(appID)
}
