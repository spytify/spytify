package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The plain colour fills the padding above the tabs and their bar, across
// the window, and stops a padding above the page, which stays on the grey;
// it follows the tab shown and a change of theme.
func TestThePlainColourIsBehindTheTabBarOnly(t *testing.T) {
	fa := test.NewTempApp(t)
	fa.Settings().SetTheme(spytifyTheme{mode: "light"})
	record, library := widget.NewLabel("Record"), widget.NewButton("Library", nil)
	tabs := container.NewAppTabs(container.NewTabItem("Record", record), container.NewTabItem("Library", library))
	view := withPlainTabBar(tabs)
	test.NewTempWindow(t, view)
	view.Resize(fyne.NewSize(600, 400))
	if view.MinSize() != tabs.MinSize() || tabs.Size() != view.Size() {
		t.Errorf("the tabs ask for %v and get %v; the view asks for %v and gets %v", tabs.MinSize(), tabs.Size(), view.MinSize(), view.Size())
	}
	back := view.Objects[0].(*canvas.Rectangle)
	if view.Objects[1] != tabs {
		t.Fatal("the colour is not behind the tabs")
	}
	pad := theme.Padding()
	for i, page := range []fyne.CanvasObject{record, library} {
		tabs.SelectIndex(i)
		view.Refresh()
		if page.Position().Y <= pad {
			t.Fatalf("tab %d: the page starts at %v, with no bar above it", i, page.Position().Y)
		}
		if back.Position() != fyne.NewPos(0, -pad) || back.Size() != fyne.NewSize(600, page.Position().Y) {
			t.Errorf("tab %d: the colour is %v at %v, the page starts at %v", i, back.Size(), back.Position(), page.Position().Y)
		}
	}
	for i, mode := range []string{"light", "dark"} {
		fa.Settings().SetTheme(spytifyTheme{mode: mode})
		view.Refresh()
		if back.FillColor != cardColors[colorPlain][i] {
			t.Errorf("%s: the bar is %v, want %v", mode, back.FillColor, cardColors[colorPlain][i])
		}
	}
}

// With no tab selected, the colour covers only the padding above the tabs.
func TestThePlainColourWithNoTab(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	view := withPlainTabBar(container.NewAppTabs())
	test.NewTempWindow(t, view)
	view.Resize(fyne.NewSize(600, 400))
	pad := theme.Padding()
	if back := view.Objects[0].(*canvas.Rectangle); back.Position() != fyne.NewPos(0, -pad) || back.Size() != fyne.NewSize(600, pad) {
		t.Errorf("the colour is %v at %v", back.Size(), back.Position())
	}
}
