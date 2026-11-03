package ui

import (
	"testing"

	"fyne.io/fyne/v2"
)

// A new theme drops the rows put aside on a scroll: drawn again, they kept
// the former theme's icons and badges, which only the rows in view are
// refreshed for.
func TestANewThemeDropsTheRowsPutAside(t *testing.T) {
	n := 100
	l, _ := testCardList(t, &n)
	l.scroll.ScrollToOffset(fyne.NewPos(0, 500))
	l.scroll.OnScrolled(l.scroll.Offset)
	if len(l.spare) == 0 {
		t.Fatal("a scroll put no row aside")
	}
	l.Refresh()
	if len(l.spare) == 0 {
		t.Error("a refresh in the same theme dropped the rows put aside")
	}
	fyne.CurrentApp().Settings().SetTheme(spytifyTheme{mode: "dark"})
	l.Refresh()
	if len(l.spare) != 0 {
		t.Errorf("%d rows of the light theme are kept for reuse in the dark one", len(l.spare))
	}
}
