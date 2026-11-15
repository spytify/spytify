package telemetry

import (
	"testing"

	"github.com/denisbrodbeck/machineid"
)

// A Mac keeps the computer ID it had when the library started ioreg
// itself.
func TestTheComputerIDIsTheLibrarysHash(t *testing.T) {
	want, err := machineid.ProtectedID("spytify")
	if err != nil {
		t.Skip(err)
	}
	if got, err := protectedID("spytify"); err != nil || got != want {
		t.Fatalf("protectedID = %q, %v; want the library's %q", got, err, want)
	}
}
