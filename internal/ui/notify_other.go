//go:build !windows

package ui

import "fyne.io/fyne/v2"

// systemToast shows notifications through Fyne: D-Bus on Linux, the
// notification center on macOS. Neither says when one is not shown.
func systemToast(fa fyne.App, _ func(error)) func(title, text string) {
	return func(title, text string) {
		fa.SendNotification(fyne.NewNotification(title, text))
	}
}
