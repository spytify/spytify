// Package hidden makes folders the file manager does not show: a name that
// starts with a dot hides one on macOS and Linux, and Windows needs the
// folder's Hidden attribute as well.
package hidden

import "os"

// MkdirAll makes path and its missing parents, and hides path.
func MkdirAll(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return hide(path)
}
