package ui

import (
	"testing"

	"fyne.io/fyne/v2/widget"
)

// While the template is typed, a line under it names the {names} that are
// not placeholders, which saving leaves out; it goes once they are fixed.
func TestTheTemplateNamesWhatIsNotAPlaceholder(t *testing.T) {
	a, _, s := settingsApp(t)
	s.search.SetText("template")
	a.render()
	f := s.fields["output.template"]
	if !holds(f.row, s.unknown) {
		t.Fatal("the line is not in the template's row")
	}
	if s.unknown.Visible() {
		t.Error("the line shows with the default template")
	}
	e := f.input.(*applyEntry)
	// Focused, as while typing, so a render keeps the text.
	a.win.Canvas().Focus(e)
	line := func() string { return s.unknown.Segments[0].(*widget.TextSegment).Text }
	e.SetText("{album_artist} {discc} - {title} {foo} {Yearr} {discc}")
	if !s.unknown.Visible() || line() != "Not placeholders, left out when saved: {discc}, {foo}, {Yearr}" {
		t.Errorf("shown %v: %q", s.unknown.Visible(), line())
	}
	if pos, size := s.unknown.Position(), s.unknown.Size(); pos.Y < e.Size().Height || size.Height == 0 {
		t.Errorf("the line is at %v, %v: not laid out under the template, %v high", pos, size, e.Size().Height)
	}
	snapshot(t, a, "settings-template-unknown")
	e.SetText("{album_artist} {discc} - {title}")
	if !s.unknown.Visible() || line() != "Not a placeholder, left out when saved: {discc}" {
		t.Errorf("shown %v: %q", s.unknown.Visible(), line())
	}
	e.SetText("{album_artist} - {title}")
	if s.unknown.Visible() {
		t.Error("the line stayed once the template was fixed")
	}
}
