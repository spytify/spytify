//go:build !windows && !linux

package opengl

// Check has nothing to look for: macOS always has OpenGL.
func Check() error { return nil }
