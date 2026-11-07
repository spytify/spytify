//go:build !windows

package hidden

// hide has nothing to do: the dot in the name hides the folder.
func hide(string) error { return nil }
