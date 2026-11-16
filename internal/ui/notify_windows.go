//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/ui/toast"
)

// systemToast shows notifications in this process, through the Windows
// Runtime (package toast), never through Fyne's PowerShell. A toast Windows
// refuses is passed to failed, on the app's goroutine, and nothing else is
// tried.
func systemToast(fa fyne.App, failed func(error)) func(title, text string) {
	var iconOnce sync.Once
	icon := ""
	return func(title, text string) {
		go func() {
			iconOnce.Do(func() { icon = toastIcon(fa) })
			if err := toast.Show(fa.UniqueID(), title, text, icon); err != nil {
				fyne.Do(func() { failed(err) })
			}
		}()
	}
}

// toastIcon writes the app's icon where Fyne keeps it for its toasts, and
// says where; "" when there is none.
func toastIcon(fa fyne.App) string {
	res := fa.Icon()
	cache, err := os.UserCacheDir()
	if res == nil || err != nil {
		return ""
	}
	dir := filepath.Join(cache, "fyne", fa.UniqueID())
	path := filepath.Join(dir, "icon.png")
	if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(path, res.Content(), 0o600) != nil {
		return ""
	}
	return path
}
