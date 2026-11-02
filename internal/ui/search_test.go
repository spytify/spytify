package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
)

// selectsIn are the dropdowns shown under o, the Library's check select
// included.
func selectsIn(o fyne.CanvasObject) []*widget.Select {
	var out []*widget.Select
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *widget.Select:
			out = append(out, w)
			return
		case *checkSelect:
			out = append(out, &w.Select)
			return
		case *fyne.Container:
			for _, c := range w.Objects {
				walk(c)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, c := range test.WidgetRenderer(w).Objects() {
				walk(c)
			}
		}
	}
	walk(o)
	return out
}

// The Library's and Settings' search fields sit alike, square, the
// magnifier before the text, nothing at the end until something is typed;
// the dropdowns beside and under them are square too, the Library's two as
// wide as each other.
func TestTheSearchFieldsLookAlike(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	values["output.dir"] = t.TempDir()
	m.SetSettings(values)
	at := a.fa.Driver().AbsolutePositionForObject
	fields := []struct {
		tab    int
		search *keyEntry
	}{
		{tabLibrary, a.screens[tabLibrary].(*libraryScreen).search},
		{tabSettings, a.screens[tabSettings].(*settingsScreen).search},
	}
	var first *keyEntry
	for _, f := range fields {
		a.showTab(f.tab)
		a.render()
		e := f.search
		if e.Icon == nil || e.Icon.Name() != theme.SearchIcon().Name() {
			t.Errorf("tab %d: the search's leading icon is %v", f.tab, e.Icon)
		}
		if r := e.Theme().Size(theme.SizeNameInputRadius); r != 0 {
			t.Errorf("tab %d: the search's corners are %v round", f.tab, r)
		}
		if clearOf(e).Visible() {
			t.Errorf("tab %d: a clear button with nothing to clear", f.tab)
		}
		selects := selectsIn(a.win.Content())
		if len(selects) == 0 {
			t.Errorf("tab %d: no dropdown", f.tab)
		}
		for _, s := range selects {
			if r := s.Theme().Size(theme.SizeNameInputRadius); r != 0 {
				t.Errorf("tab %d: a dropdown's corners are %v round", f.tab, r)
			}
		}
		if first == nil {
			first = e
			lib := a.screens[tabLibrary].(*libraryScreen)
			if w, k := lib.sortBy.Size().Width, lib.kinds.Size().Width; w != k {
				t.Errorf("the Library's dropdowns are %v and %v wide", w, k)
			}
			snapshot(t, a, "library-search")
			continue
		}
		snapshot(t, a, "settings-search")
		a.showTab(fields[0].tab)
		a.render()
		lib, libSize := at(first), first.Size()
		a.showTab(f.tab)
		a.render()
		if at(e).X != lib.X || at(e).Y != lib.Y || e.Size().Height != libSize.Height {
			t.Errorf("Settings' search at %v, %v tall; the Library's at %v, %v tall", at(e), e.Size().Height, lib, libSize.Height)
		}
	}
}

// Something typed in a search: Tab goes to the clear button at its end, and
// Space clears the search and hands the focus back to it, without typing a
// space. The button has no background under the pointer, only when
// focused, and little padding around its icon.
func TestTheClearButtonOfASearch(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	c := a.win.Canvas()
	c.Focus(s.search)
	test.Type(s.search, "cover")
	a.render()
	clear := clearOf(s.search)
	if !clear.Visible() {
		t.Fatal("something typed: no clear button")
	}
	th := clear.Theme()
	if _, _, _, alpha := th.Color(theme.ColorNameHover, theme.VariantLight).RGBA(); alpha != 0 {
		t.Errorf("the clear button lights up under the pointer, alpha %d", alpha)
	}
	if p, base := th.Size(theme.SizeNameInnerPadding), theme.InnerPadding(); p != base/2 {
		t.Errorf("the clear button's padding is %v, want %v", p, base/2)
	}
	// The field laid its text out before the button first showed: the
	// button still gets the room of its icon and padding.
	if w, want := clear.Size().Width, th.Size(theme.SizeNameInlineIcon)+2*th.Size(theme.SizeNameInnerPadding); w != want {
		t.Errorf("the clear button is %v wide, want %v", w, want)
	}
	snapshot(t, a, "settings-search-typed")
	pressTab(c)
	if c.Focused() != clear {
		t.Fatalf("Tab from the search goes to %s, not its clear button", focusName(a, c.Focused()))
	}
	snapshot(t, a, "settings-search-clear-focused")
	pressSpace(c)
	a.render()
	if s.search.Text != "" || clear.Visible() || c.Focused() != s.search {
		t.Errorf("cleared: the search holds %q, its clear button shows %v, the focus is on %s", s.search.Text, clear.Visible(), focusName(a, c.Focused()))
	}
	if all := texts(a.win.Content()); !contains(all, "Output folder") {
		t.Errorf("cleared: the settings stay filtered: %v", all)
	}
}
