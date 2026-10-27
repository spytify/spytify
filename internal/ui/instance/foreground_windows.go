package instance

import "golang.org/x/sys/windows"

var allowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

const asfwAny = ^uintptr(0)

// allowForeground lets any process bring its window to the front until
// the user's next input: Windows refuses it to a process in the
// background, and the running copy's window would only flash in the
// taskbar.
func allowForeground() {
	if allowSetForegroundWindow.Find() == nil {
		allowSetForegroundWindow.Call(asfwAny)
	}
}
