//go:build (!unix && !windows) || (darwin && !cgo)

package trash

import (
	"errors"
	"os"
)

// Move fails where Spytify knows no trash: it never deletes instead.
func Move(path string) error {
	return &os.PathError{Op: "trash", Path: path, Err: errors.ErrUnsupported}
}
