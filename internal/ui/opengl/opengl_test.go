package opengl

import "testing"

func TestUsableIsWhatGLFWTakes(t *testing.T) {
	const window = pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer
	for _, c := range []struct {
		name      string
		flags     uint32
		pixelType uint8
		want      bool
	}{
		{"a driver's format", window, pfdTypeRGBA, true},
		{"Windows' software OpenGL 1.1", window | pfdGenericFormat, pfdTypeRGBA, false},
		{"a generic format a driver accelerates", window | pfdGenericFormat | pfdGenericAccelerated, pfdTypeRGBA, true},
		{"single buffered", pfdDrawToWindow | pfdSupportOpenGL, pfdTypeRGBA, false},
		{"for bitmaps only", pfdSupportOpenGL | pfdDoubleBuffer, pfdTypeRGBA, false},
		{"without OpenGL", pfdDrawToWindow | pfdDoubleBuffer, pfdTypeRGBA, false},
		{"colour index", window, 1, false},
	} {
		if got := usable(c.flags, c.pixelType); got != c.want {
			t.Errorf("%s: usable %v, want %v", c.name, got, c.want)
		}
	}
}
