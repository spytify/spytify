package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// themed draws its content in a theme of its own, as container.ThemeOverride
// does, without the override's refresh. Fyne 2.8.2 gives an override a new
// scope at each refresh, and its font cache is keyed by scope: each refresh
// parsed the theme's fonts again and kept them, about 17 MB per track in the
// Record list, until the app ran out of memory (#764, fyne#6640). A refresh
// from above refreshes the content only, in the scope it got once. Back to
// container.ThemeOverride once a Fyne release has fyne#6642.
type themed struct {
	widget.BaseWidget
	o *container.ThemeOverride
}

func newThemed(c fyne.CanvasObject, th fyne.Theme) *themed {
	t := &themed{o: container.NewThemeOverride(c, th)}
	t.ExtendBaseWidget(t)
	return t
}

func (t *themed) CreateRenderer() fyne.WidgetRenderer { return themedRenderer{t} }

type themedRenderer struct{ t *themed }

func (r themedRenderer) Layout(s fyne.Size)           { r.t.o.Resize(s) }
func (r themedRenderer) MinSize() fyne.Size           { return r.t.o.MinSize() }
func (r themedRenderer) Refresh()                     { r.t.o.Content.Refresh() }
func (r themedRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.t.o} }
func (r themedRenderer) Destroy()                     {}
