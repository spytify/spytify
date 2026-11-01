package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// tabsLayout puts the plain colour behind the tabs' bar, and behind the
// padding the window's layout leaves above it, up to the app bar: the tabs
// draw no background of their own, and the page's grey is below. The tab
// keys lie over the colour, under the tabs.
type tabsLayout struct{ tabs *container.AppTabs }

// withPlainTabBar is tabs with the plain colour behind their bar, and keys,
// the bar's stop in the Tab order, before the page.
func withPlainTabBar(tabs *container.AppTabs, keys *tabKeys) *fyne.Container {
	return container.New(tabsLayout{tabs}, canvas.NewRectangle(theme.Color(colorPlain)), keys, tabs)
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
	objs[1].Move(back.Position())
	objs[1].Resize(back.Size())
}

func (l tabsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.tabs.MinSize() }

// tabKeys is the tab bar's stop in the Tab order, after the app bar and
// before the page. Focused, it rings the bar in the focus colour, and Left
// and Right show the tab before or after, from the last round to the first.
// It takes no taps: a click goes on to the tab under it.
type tabKeys struct {
	widget.BaseWidget
	tabs    *container.AppTabs
	focused bool
}

func newTabKeys(tabs *container.AppTabs) *tabKeys {
	k := &tabKeys{tabs: tabs}
	k.ExtendBaseWidget(k)
	return k
}

func (k *tabKeys) FocusGained()   { k.focused = true; k.Refresh() }
func (k *tabKeys) FocusLost()     { k.focused = false; k.Refresh() }
func (k *tabKeys) TypedRune(rune) {}

func (k *tabKeys) TypedKey(e *fyne.KeyEvent) {
	n := len(k.tabs.Items)
	if n == 0 {
		return
	}
	switch e.Name {
	case fyne.KeyLeft:
		k.tabs.SelectIndex((k.tabs.SelectedIndex() + n - 1) % n)
	case fyne.KeyRight:
		k.tabs.SelectIndex((k.tabs.SelectedIndex() + 1) % n)
	}
}

func (k *tabKeys) CreateRenderer() fyne.WidgetRenderer {
	r := &tabKeysRenderer{k: k, ring: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type tabKeysRenderer struct {
	k    *tabKeys
	ring *canvas.Rectangle
}

// Layout rings the bar, below the padding above it, inside the window's
// edges.
func (r *tabKeysRenderer) Layout(size fyne.Size) {
	pad := theme.Padding()
	r.ring.Move(fyne.NewPos(pad/2, pad))
	r.ring.Resize(fyne.NewSize(size.Width-pad, size.Height-pad))
}

func (r *tabKeysRenderer) MinSize() fyne.Size           { return fyne.Size{} }
func (r *tabKeysRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.ring} }
func (r *tabKeysRenderer) Destroy()                     {}

func (r *tabKeysRenderer) Refresh() {
	r.ring.CornerRadius = theme.InputRadiusSize()
	r.ring.StrokeWidth = 2
	r.ring.StrokeColor = color.Transparent
	if r.k.focused {
		r.ring.StrokeColor = theme.Color(theme.ColorNameFocus)
	}
	r.Layout(r.k.Size())
	r.ring.Refresh()
}
