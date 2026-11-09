package ui

import (
	"time"

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
//
// The scope is stamped on each widget, and Fyne drops a widget's stamp with
// its drawing when it has not drawn it for a minute: App.keepWindowDrawn
// keeps the window's widgets drawn.
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

// keepEvery is half the minute Fyne keeps the drawing of a widget it does
// not draw.
const keepEvery = 30 * time.Second

// keepWindowDrawn keeps the drawings of the whole window, the tabs not shown
// and the parts hidden included, each of its parts every keepEvery, one part
// at a time so that no frame waits for all of them (about 20 ms). Fyne 2.8.2
// drops the drawing of a widget it has not drawn for a minute, and its theme
// with it: a tab or a card hidden that long came back in the app's theme,
// the Record card's white text dark on its cover in light. A ThemeOverride
// stamps its theme again at each refresh, with a new font set; themed never
// does. Switching tabs, a hidden one back after a minute, grew the window
// too (about 3 MB each time); kept drawn, it stays flat.
func (a *App) keepWindowDrawn() {
	n := len(a.tabs.Items) + 2
	for i := 0; ; i++ {
		time.Sleep(keepEvery / time.Duration(n))
		fyne.Do(func() { keepDrawn(a.keptParts()[i%n]) })
	}
}

// keptParts are the parts of the window keepWindowDrawn goes over in turn.
// It starts from the window rather than from each themed, so that a row the
// list drops is let go.
func (a *App) keptParts() []fyne.CanvasObject {
	parts := []fyne.CanvasObject{a.win.Content()}
	for _, it := range a.tabs.Items {
		parts = append(parts, it.Content)
	}
	// The diagnostics' checks wait in a pop-up of their own.
	return append(parts, a.screens[tabLogs].(*logsScreen).diag)
}

// parted is a widget of ours that holds other widgets, shown or kept for
// later: keepDrawn goes into them rather than refreshing it.
type parted interface {
	fyne.Widget
	parts() []fyne.CanvasObject
}

// keepDrawn keeps the drawing of o and of everything under it, shown or
// not. A widget's size keeps its drawing without drawing it again: the walk
// asks it of what it goes into, and refreshes only the widgets it does not,
// whose refresh reaches their insides. Refreshing all of it took 40 to 70 ms.
func keepDrawn(o fyne.CanvasObject) {
	switch o := o.(type) {
	case nil:
	case *fyne.Container:
		for _, c := range o.Objects {
			keepDrawn(c)
		}
	case *themed:
		// The override's own refresh would stamp a new scope.
		o.MinSize()
		o.o.MinSize()
		keepDrawn(o.o.Content)
	case *container.AppTabs:
		// Its bar is always shown, and drawn; its tabs are parts of their
		// own.
	case *container.Scroll:
		// Its refresh draws its bars and shadows, not its content.
		o.Refresh()
		keepDrawn(o.Content)
	case *widget.PopUp:
		o.MinSize()
		keepDrawn(o.Content)
	case parted:
		o.MinSize()
		for _, c := range o.parts() {
			keepDrawn(c)
		}
	default:
		o.Refresh()
	}
}
