package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/spytify/spytify/internal/ui/model"
)

func TestThemeFollowsTheSetting(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["app.theme"] = "light"
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.render()
	th := a.fa.Settings().Theme()
	// The green replaces the blue, whatever variant the OS asks for.
	if got := th.Color(theme.ColorNamePrimary, theme.VariantDark); got != greenLight {
		t.Errorf("light theme primary = %v", got)
	}
	values["app.theme"] = "dark"
	m.SetSettings(values)
	a.render()
	if got := a.fa.Settings().Theme().Color(theme.ColorNamePrimary, theme.VariantLight); got != greenDark {
		t.Errorf("dark theme primary = %v", got)
	}
	values["app.theme"] = "system"
	m.SetSettings(values)
	a.render()
	if got := a.fa.Settings().Theme().Color(theme.ColorNamePrimary, theme.VariantLight); got != greenLight {
		t.Errorf("system theme on a light OS = %v", got)
	}
}

// Hover and selection are drawn over buttons: an opaque colour would hide
// a red Stop under the pointer.
func TestHoverAndSelectionAreTranslucent(t *testing.T) {
	th := spytifyTheme{mode: "system"}
	for _, v := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		for _, name := range []fyne.ThemeColorName{theme.ColorNameHover, theme.ColorNameSelection} {
			if _, _, _, a := th.Color(name, v).RGBA(); a >= 0xffff {
				t.Errorf("%s is opaque in variant %d", name, v)
			}
		}
	}
}

// A footer's button lights up under the pointer, off the footer's grey:
// the usual hover is that grey, on which it did not show. It turns white
// on the light grey, charcoal on the dark.
func TestAFootersButtonLightsUpUnderThePointer(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	clear := a.screens[tabRecord].(*recordScreen).clear
	for i, mode := range []string{"light", "dark"} {
		a.fa.Settings().SetTheme(spytifyTheme{mode: mode})
		a.render()
		hover, footer := theme.ColorForWidget(theme.ColorNameHover, clear), cardColors[colorFooter][i]
		if want := cardColors[colorFooterHover][i]; hover != want || hover == footer {
			t.Errorf("%s: a footer's button lights %v under the pointer, want %v, the footer %v", mode, hover, want, footer)
		}
	}
}
