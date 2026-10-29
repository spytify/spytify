package opengl

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// Check opens, hidden, the window Fyne opens, with the same OpenGL
// context: when GLFW cannot make it, Fyne keeps running with no window.
// Fyne may draw on Wayland or on X11, so one that works is enough. With no
// display to try, it says nothing and lets Fyne try.
func Check() error {
	// Init hints outlive Terminate: Fyne's Init gets GLFW's own choices
	// back. The hidden window needs no decorations, and libdecor's start
	// GTK.
	defer func() {
		glfw.InitHint(glfw.PlatformHint, int(glfw.AnyPlatform))
		glfw.InitHint(glfw.WaylandLibdecor, glfw.WaylandPreferLibdecor)
	}()
	glfw.InitHint(glfw.WaylandLibdecor, glfw.WaylandDisableLibdecor)
	var errs []error
	for _, p := range platforms() {
		glfw.InitHint(glfw.PlatformHint, int(p))
		if glfw.Init() != nil {
			continue
		}
		err := probe()
		glfw.Terminate()
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrNoDriver, errors.Join(errs...))
}

// platforms are the display systems Fyne may draw on: the one FYNE_PLATFORM
// names, else those the session has.
func platforms() []glfw.Platform {
	switch os.Getenv("FYNE_PLATFORM") {
	case "x11":
		return []glfw.Platform{glfw.PlatformX11}
	case "wayland":
		return []glfw.Platform{glfw.PlatformWayland}
	}
	var ps []glfw.Platform
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		ps = append(ps, glfw.PlatformWayland)
	}
	if os.Getenv("DISPLAY") != "" {
		ps = append(ps, glfw.PlatformX11)
	}
	return ps
}

// probe makes and closes the hidden window. go-gl/glfw panics on the
// errors it does not expect, such as no pixel format, and returns none for
// some it only logs.
func probe() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	glfw.WindowHint(glfw.Visible, glfw.False)
	// Fyne's hints: OpenGL ES 2.0 on ARM, OpenGL 2.1 elsewhere.
	if runtime.GOARCH == "arm" || runtime.GOARCH == "arm64" {
		glfw.WindowHint(glfw.ClientAPI, glfw.OpenGLESAPI)
		glfw.WindowHint(glfw.ContextVersionMajor, 2)
		glfw.WindowHint(glfw.ContextVersionMinor, 0)
	} else {
		glfw.WindowHint(glfw.ContextVersionMajor, 2)
		glfw.WindowHint(glfw.ContextVersionMinor, 1)
	}
	w, err := glfw.CreateWindow(1, 1, "Spytify", nil, nil)
	if w == nil {
		if err == nil {
			err = errors.New("GLFW made no window")
		}
		return err
	}
	w.Destroy()
	return nil
}
