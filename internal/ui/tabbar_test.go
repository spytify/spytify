package ui

import (
	"image/color"
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
	view := withPlainTabBar(tabs, newTabKeys(tabs))
	test.NewTempWindow(t, view)
	view.Resize(fyne.NewSize(600, 400))
	if view.MinSize() != tabs.MinSize() || tabs.Size() != view.Size() {
		t.Errorf("the tabs ask for %v and get %v; the view asks for %v and gets %v", tabs.MinSize(), tabs.Size(), view.MinSize(), view.Size())
	}
	back := view.Objects[0].(*canvas.Rectangle)
	if view.Objects[2] != tabs {
		t.Fatal("the colour is not behind the tabs")
	}
	keys := view.Objects[1]
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
		if keys.Position() != back.Position() || keys.Size() != back.Size() {
			t.Errorf("tab %d: the bar's keys are %v at %v, over a colour %v at %v", i, keys.Size(), keys.Position(), back.Size(), back.Position())
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
	tabs := container.NewAppTabs()
	keys := newTabKeys(tabs)
	view := withPlainTabBar(tabs, keys)
	test.NewTempWindow(t, view)
	view.Resize(fyne.NewSize(600, 400))
	pad := theme.Padding()
	if back := view.Objects[0].(*canvas.Rectangle); back.Position() != fyne.NewPos(0, -pad) || back.Size() != fyne.NewSize(600, pad) {
		t.Errorf("the colour is %v at %v", back.Size(), back.Position())
	}
	// Left and Right have no tab to show.
	keys.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	keys.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
}

// Tab stops on the tab bar, between the app bar and the page, which rings
// it in the focus colour. Left and Right then show the tab before or after, round the
// ends, and leave the focus on the bar; a click still goes to the tab
// under it. On Settings, the page stays where it was scrolled.
func TestTheTabBarTakesTheKeyboard(t *testing.T) {
	a, _, _ := connectedApp(t)
	c := a.win.Canvas()
	bar := tabTo(t, a, "Tabs", 1)
	ring := test.TempWidgetRenderer(t, a.tabKeys).Objects()[0].(*canvas.Rectangle)
	if ring.StrokeColor != theme.Color(theme.ColorNameFocus) || ring.FillColor != color.Transparent {
		t.Errorf("the focused bar is ringed %v and filled %v, want the focus colour round it", ring.StrokeColor, ring.FillColor)
	}
	snapshot(t, a, "tab-bar-focused")
	for _, step := range []struct {
		key  fyne.KeyName
		want int
	}{
		{fyne.KeyRight, tabLibrary},
		{fyne.KeyRight, tabSettings},
		{fyne.KeyLeft, tabLibrary},
		{fyne.KeyLeft, tabRecord},
		{fyne.KeyLeft, tabAbout},
		{fyne.KeyRight, tabRecord},
	} {
		pressKey(c, step.key)
		if got := a.tabs.SelectedIndex(); got != step.want {
			t.Fatalf("%s showed tab %d, want %d", step.key, got, step.want)
		}
		if c.Focused() != bar {
			t.Fatalf("%s took the focus off the tab bar", step.key)
		}
	}
	if _, taps := any(a.tabKeys).(fyne.Tappable); taps {
		t.Error("the bar's keys take the clicks meant for the tabs")
	}

	pressKey(c, fyne.KeyRight)
	pressKey(c, fyne.KeyRight)
	s := a.screens[tabSettings].(*settingsScreen)
	relayout(s.scroll)
	s.scroll.ScrollToBottom()
	off := s.scroll.Offset
	if off.Y <= 0 {
		t.Fatal("the settings do not scroll")
	}
	a.followFocus()
	if s.scroll.Offset != off {
		t.Errorf("the focus on the bar scrolled the settings from %v to %v", off, s.scroll.Offset)
	}

	c.Unfocus()
	if ring.StrokeColor != color.Transparent {
		t.Errorf("out of focus, the bar is ringed %v", ring.StrokeColor)
	}
}
