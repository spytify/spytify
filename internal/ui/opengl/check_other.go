//go:build !windows

package opengl

// Check has nothing to look for: macOS always has OpenGL, and Linux's
// comes with the Mesa library the app needs to start at all.
func Check() error { return nil }
