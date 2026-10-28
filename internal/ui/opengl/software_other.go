//go:build !windows

package opengl

import "errors"

// Software, UseSoftware and StartSoftware are Windows only: elsewhere the
// app draws with the system's OpenGL, Mesa's included.
func Software() bool { return false }

func UseSoftware() error { return errors.ErrUnsupported }

func StartSoftware() error { return errors.ErrUnsupported }
