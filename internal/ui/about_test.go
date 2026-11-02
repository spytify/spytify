package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// findObject is the first object under o that match accepts, and where it
// is in o.
func findObject(o fyne.CanvasObject, match func(fyne.CanvasObject) bool) (fyne.CanvasObject, fyne.Position) {
	if o == nil {
		return nil, fyne.Position{}
	}
	if match(o) {
		return o, fyne.Position{}
	}
	var children []fyne.CanvasObject
	switch w := o.(type) {
	case *fyne.Container:
		children = w.Objects
	case fyne.Widget:
		children = test.WidgetRenderer(w).Objects()
	}
	for _, c := range children {
		if f, at := findObject(c, match); f != nil {
			return f, c.Position().Add(at)
		}
	}
	return nil, fyne.Position{}
}

// About's content sits in the middle of its column, as far under the
// column's top as it is over the disclaimer at the bottom.
func TestAboutKeepsItsContentInTheMiddle(t *testing.T) {
	a, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	a.showTab(tabAbout)
	a.render()
	root := a.screens[tabAbout].content()
	logo, logoAt := findObject(root, func(o fyne.CanvasObject) bool {
		img, ok := o.(*canvas.Image)
		return ok && img.Resource == Wordmark
	})
	project, projectAt := findObject(root, func(o fyne.CanvasObject) bool {
		b, ok := asButton(o)
		return ok && b.Text == "Project page"
	})
	legal, legalAt := findObject(root, func(o fyne.CanvasObject) bool {
		l, ok := o.(*widget.Label)
		return ok && l.Text == disclaimer
	})
	if logo == nil || project == nil || legal == nil {
		t.Fatalf("logo %v, Project page %v, disclaimer %v", logo != nil, project != nil, legal != nil)
	}
	// The disclaimer ends the column, 420 high.
	columnTop := legalAt.Y + legal.Size().Height - 420
	above := logoAt.Y - columnTop
	below := legalAt.Y - (projectAt.Y + project.Size().Height)
	if diff := above - below; diff < -theme.Padding() || diff > theme.Padding() {
		t.Errorf("the content is %v under the column's top and %v over the disclaimer", above, below)
	}
}
