//go:build !windows

package opengl

import "errors"

// Software, UseSoftware and StartSoftware are Windows only: elsewhere
// Check always finds OpenGL.
func Software() bool { return false }

func UseSoftware() error { return errors.ErrUnsupported }

func StartSoftware() error { return errors.ErrUnsupported }
