package opengl

import (
	"errors"
	"slices"
	"testing"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// The answer depends on the display (CI's unit tests have none); either
// one is fine, anything else is not.
func TestCheckAnswers(t *testing.T) {
	err := Check()
	if err != nil && !errors.Is(err, ErrNoDriver) {
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
	t.Run("nothing to try", func(t *testing.T) {
		t.Setenv("FYNE_PLATFORM", "")
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		if err := Check(); err != nil {
			t.Errorf("Check without a display: %v, want nil so Fyne tries", err)
		}
	})
}
