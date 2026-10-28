package ui

import "golang.org/x/sys/windows"

// tell shows a message box, which Windows draws without OpenGL.
func tell(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}
