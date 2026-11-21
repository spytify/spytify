package platform

import (
	"slices"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
)

// The capture helper missing next to spytifyd marks its check "missing",
// which the window words for the user (#934); the recorder's error stays
// in Detail for the CLI.
func TestAMissingCaptureHelperIsMarkedMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, c := range (Backends{}).Probe() {
		if c.ID != proto.CheckHelper {
			continue
		}
		if c.OK || !slices.Equal(c.Args, []string{"missing"}) || c.Detail == "" {
			t.Errorf("capture helper check: %+v, want failed with Args [missing] and the reason in Detail", c)
		}
		return
	}
	t.Fatal("no capture helper check")
}
