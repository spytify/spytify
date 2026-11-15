//go:build darwin && cgo

package telemetry

import (
	"testing"

	"github.com/denisbrodbeck/machineid"
)

// IOKit gives the IOPlatformUUID ioreg prints: on a Mac, the ID sent is
// the very one earlier versions sent.
func TestIOKitReadsWhatIoregPrints(t *testing.T) {
	want, err := machineid.ProtectedID("spytify")
	if err != nil {
		t.Fatalf("ioreg: %v", err)
	}
	raw, err := systemMachineID()
	if err != nil {
		t.Fatalf("IOKit: %v", err)
	}
	if got := protect(raw); got != want {
		t.Errorf("IOKit's ID hashes to %s, ioreg's to %s", got, want)
	}
}
