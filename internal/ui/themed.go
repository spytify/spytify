package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// themed draws its content in a theme of its own: a container.ThemeOverride
// that only the widgets switching on *themed know about. Each refresh stamps
// the theme again on everything under it, which brings it back to a widget
// Fyne let go of after a minute undrawn, as in a tab hidden that long. Every
// stamp is a new scope, which Fyne 2.8.2 gave a new parse of the theme's
// fonts, about 17 MB per track in the Record list (#764): the Fyne in go.mod
// has fyne#6642, which keys its font cache by the font.
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
func (r themedRenderer) Refresh()                     { r.t.o.Refresh() }
func (r themedRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.t.o} }
func (r themedRenderer) Destroy()                     {}
