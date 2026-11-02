package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/spytify/spytify/internal/ui/lang"
)

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
		b, ok := as[*widget.Button](o)
		return ok && b.Text == lang.ProjectPage
	})
	legal, legalAt := findObject(root, func(o fyne.CanvasObject) bool {
		l, ok := o.(*widget.Label)
		return ok && l.Text == lang.Disclaimer
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
