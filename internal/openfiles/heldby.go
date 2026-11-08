//go:build !windows

package openfiles

// HeldBy says nothing outside Windows: there, a file another program holds
// open is renamed all the same.
func HeldBy(paths ...string) []string { return nil }
