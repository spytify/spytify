package opengl

import (
	"errors"
	"testing"
	"unsafe"
)

func TestD3DKMTStructsMatchD3dkmthk(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want uintptr
	}{
		{"D3DKMT_OPENADAPTERFROMHDC", unsafe.Sizeof(openAdapterFromHdc{}), 24},
		{"D3DKMT_QUERYADAPTERINFO", unsafe.Sizeof(queryAdapterInfo{}), 24},
		{"D3DKMT_OPENGLINFO", unsafe.Sizeof(openGLInfo{}), 528},
	} {
		if c.got != c.want {
			t.Errorf("%s is %d bytes, want %d", c.name, c.got, c.want)
		}
	}
}

// With no driver listed, the app loads Mesa's OpenGL without asking Check:
// Check must then find no pixel format either, or a computer whose driver
// draws would be sent to Mesa. CI's runner has no graphics card; a
// computer with one runs this test as well.
func TestNoDriverListedOnlyWhenCheckFindsNone(t *testing.T) {
	listed := !NoDriverListed()
	err := Check()
	t.Logf("driver listed: %v, Check: %v", listed, err)
	if !listed && !errors.Is(err, ErrNoDriver) {
		t.Errorf("no driver listed, but Check found a pixel format (%v)", err)
	}
}
