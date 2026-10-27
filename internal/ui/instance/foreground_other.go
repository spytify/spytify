//go:build !windows

package instance

// allowForeground is Windows only: elsewhere the running copy may bring
// its window to the front.
func allowForeground() {}
