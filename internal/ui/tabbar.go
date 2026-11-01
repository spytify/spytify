package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// tabsLayout puts the plain colour behind the tabs' bar, and behind the
// padding the window's layout leaves above it, up to the app bar: the tabs
// draw no background of their own, and the page's grey is below.
type tabsLayout struct{ tabs *container.AppTabs }

// withPlainTabBar is tabs with the plain colour behind their bar.
func withPlainTabBar(tabs *container.AppTabs) *fyne.Container {
	return container.New(tabsLayout{tabs}, canvas.NewRectangle(theme.Color(colorPlain)), tabs)
}

func (l tabsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	l.tabs.Resize(size)
	pad := theme.Padding()
	// The tabs put their content a padding below their bar.
	bar := float32(0)
	if item := l.tabs.Selected(); item != nil {
		bar = item.Content.Position().Y - pad
	}
	back := objs[0].(*canvas.Rectangle)
	back.FillColor = theme.Color(colorPlain)
	back.Move(fyne.NewPos(0, -pad))
	back.Resize(fyne.NewSize(size.Width, bar+pad))
}

func (l tabsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.tabs.MinSize() }
