//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
void spytifyShowInDock(int show);
void spytifyWatchReopen(void);
*/
import "C"

var reopened func()

//export spytifyReopened
func spytifyReopened() {
	if f := reopened; f != nil {
		f()
	}
}

// showInDock shows or hides the Dock icon: with the window hidden to the
// menu bar, the app lives there only, as macOS menu bar apps do.
func showInDock(show bool) {
	v := C.int(0)
	if show {
		v = 1
	}
	C.spytifyShowInDock(v)
}

// onReopen runs f when Spytify is opened again while it runs (from the
// Finder, Launchpad or open -a), which with no Dock icon is how the window
// comes back.
func onReopen(f func()) {
	reopened = f
	C.spytifyWatchReopen()
}
