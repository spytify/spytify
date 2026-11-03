package opengl

import (
	"errors"
	"slices"
	"testing"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// The answer depends on the display (CI's unit tests have none); any of
// the three is fine, anything else is not.
func TestCheckAnswers(t *testing.T) {
	err := Check()
	if err != nil && !errors.Is(err, ErrNoDriver) && !errors.Is(err, ErrNoDisplay) {
		t.Fatal(err)
	}
	t.Logf("Check: %v", err)
}

func TestCheckTriesTheDisplaysFyneMayDrawOn(t *testing.T) {
	x11, wl := glfw.PlatformX11, glfw.PlatformWayland
	for _, c := range []struct {
		name                   string
		fyne, display, wayland string
		want                   []glfw.Platform
	}{
		{"no display", "", "", "", nil},
		{"X11", "", ":0", "", []glfw.Platform{x11}},
		{"Wayland alone", "", "", "wayland-0", []glfw.Platform{wl}},
		{"Wayland with Xwayland", "", ":0", "wayland-0", []glfw.Platform{wl, x11}},
		{"Fyne told X11", "x11", ":0", "wayland-0", []glfw.Platform{x11}},
		{"Fyne told Wayland", "wayland", ":0", "wayland-0", []glfw.Platform{wl}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FYNE_PLATFORM", c.fyne)
			t.Setenv("DISPLAY", c.display)
			t.Setenv("WAYLAND_DISPLAY", c.wayland)
			if got := platforms(); !slices.Equal(got, c.want) {
				t.Errorf("platforms %v, want %v", got, c.want)
			}
		})
	}
}

// Started out of the desktop session, the app found no display, and Fyne's
// first window panicked: Check says so. Wayland's default socket, which
// GLFW tries without WAYLAND_DISPLAY, is looked for in XDG_RUNTIME_DIR.
func TestCheckSaysNoDisplayAnswers(t *testing.T) {
	for _, c := range []struct{ name, display string }{
		{"none named", ""},
		{"X11 display that does not answer", ":4093"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FYNE_PLATFORM", "")
			t.Setenv("DISPLAY", c.display)
			t.Setenv("WAYLAND_DISPLAY", "")
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			if err := Check(); !errors.Is(err, ErrNoDisplay) {
				t.Errorf("Check: %v, want %v", err, ErrNoDisplay)
			}
		})
	}
}
