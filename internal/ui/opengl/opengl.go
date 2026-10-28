// Package opengl tells, before the desktop app opens its window, whether
// the system can draw it. Fyne draws with OpenGL, which Windows offers
// only through a graphics driver; without one, Fyne closes the app
// without a word.
package opengl

import "errors"

// ErrNoDriver is what Check finds when no graphics driver offers OpenGL.
var ErrNoDriver = errors.New("no graphics driver offers OpenGL")

// PIXELFORMATDESCRIPTOR flags and pixel types, from wingdi.h.
const (
	pfdDoubleBuffer       = 0x00000001
	pfdDrawToWindow       = 0x00000004
	pfdSupportOpenGL      = 0x00000020
	pfdGenericFormat      = 0x00000040
	pfdGenericAccelerated = 0x00001000

	pfdTypeRGBA = 0
)

// usable reports whether GLFW, which opens Fyne's window, takes a pixel
// format: OpenGL in a window, double buffered, in RGBA, and drawn by a
// driver. A generic format that no driver accelerates is Windows' own
// OpenGL 1.1 in software, which GLFW refuses.
func usable(flags uint32, pixelType uint8) bool {
	const need = pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer
	if flags&need != need || pixelType != pfdTypeRGBA {
		return false
	}
	return flags&pfdGenericFormat == 0 || flags&pfdGenericAccelerated != 0
}
