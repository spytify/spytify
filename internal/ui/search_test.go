package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
)

// The Library's and Settings' search fields sit alike, square, the
// magnifier before the text, nothing at the end until something is typed.
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
		search *widget.Entry
	}{
		{tabLibrary, a.screens[tabLibrary].(*libraryScreen).search},
		{tabSettings, a.screens[tabSettings].(*settingsScreen).search},
	}
	var first *widget.Entry
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
		if e.ActionItem == nil || e.ActionItem.Visible() {
			t.Errorf("tab %d: a clear button with nothing to clear", f.tab)
		}
		if first == nil {
			first = e
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
// space.
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
	clear := s.search.ActionItem.(*clearButton)
	if !clear.Visible() {
		t.Fatal("something typed: no clear button")
	}
	snapshot(t, a, "settings-search-typed")
	pressTab(c)
	if c.Focused() != clear {
		t.Fatalf("Tab from the search goes to %s, not its clear button", focusName(a, c.Focused()))
	}
	pressSpace(c)
	a.render()
	if s.search.Text != "" || clear.Visible() || c.Focused() != s.search {
		t.Errorf("cleared: the search holds %q, its clear button shows %v, the focus is on %s", s.search.Text, clear.Visible(), focusName(a, c.Focused()))
	}
	if all := texts(a.win.Content()); !contains(all, "Output folder") {
		t.Errorf("cleared: the settings stay filtered: %v", all)
	}
}
