//go:build windows

package meta

import "golang.org/x/sys/windows"

// systemDir is the system folder, as C:\Windows\System32.
func systemDir() string {
	d, _ := windows.GetSystemDirectory()
	return d
}
