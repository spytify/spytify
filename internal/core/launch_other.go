//go:build !windows

package core

import "errors"

// openApp is Windows' way to start the player by name (launch_windows.go);
// launch never calls it elsewhere.
var openApp = func(string) error { return errors.ErrUnsupported }
