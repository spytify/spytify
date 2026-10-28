//go:build !windows

package ui

// tell is Windows only: elsewhere opengl.Check finds nothing to tell.
func tell(title, text string) {}
