//go:build !darwin || !cgo

package telemetry

import "github.com/denisbrodbeck/machineid"

// systemMachineID reads the registry's MachineGuid on Windows and
// /var/lib/dbus/machine-id or /etc/machine-id on Linux, in the app. A
// macOS build without cgo, which ships nowhere, starts ioreg.
func systemMachineID() (string, error) { return machineid.ID() }
