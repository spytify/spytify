package ui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"fyne.io/fyne/v2/widget"
)

// Each theme override drawn for the first time used to cost the window a
// new parse of the theme's fonts, kept until the app quit: Fyne 2.8.2 keys
// its font cache by the override's scope, which is new for every override
// (SPYTIFY-S: 970 MB after 4 h, out of memory while parsing a font). The
// replace in go.mod keys it by the font (fyne-io/fyne#6642); this fails if
// the replace goes before a Fyne release carries the fix.
func TestNewThemeOverridesShareTheirFonts(t *testing.T) {
	a, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	draw := func(from, to int) {
		for i := from; i < to; i++ {
			a.win.SetContent(newThemed(widget.NewLabel(fmt.Sprintf("Track %d", i)), cardTheme{}))
			a.win.Canvas().Capture()
		}
	}
	heap := func() uint64 {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}
	// The first one fills the caches a window keeps.
	draw(0, 1)
	before := heap()
	draw(1, 31)
	if grown := int64(heap()) - int64(before); grown > 20<<20 {
		t.Fatalf("30 new theme overrides grew the heap by %d MB", grown>>20)
	}
}
