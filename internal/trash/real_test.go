//go:build (darwin && cgo) || windows

package trash

import (
	"os"
	"testing"
)

// realTrash skips the test unless the system's own trash may take a test
// file: on CI, whose machines are thrown away, or with SPYTIFY_REAL_TRASH
// set.
func realTrash(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" && os.Getenv("SPYTIFY_REAL_TRASH") == "" {
		t.Skip("puts a file in this computer's trash: set SPYTIFY_REAL_TRASH=1 to run it")
	}
}
