//go:build !windows

package opengl

// NoDriverListed is Windows only: elsewhere Check decides.
func NoDriverListed() bool { return false }
