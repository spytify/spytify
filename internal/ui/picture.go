package ui

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The window's picture for a bug report covers what names the user, their
// home folder or the output folder, as the reports' scrubbing does. Fyne
// gives no way into a widget's parts, so the widgets that can show a path
// are listed as they are made: maskPaths for those covered where their
// text names one, maskAlways for those covered whatever they show. Fyne's
// own dialogs cannot be listed: while an error's or the folder picker is
// open, no picture is taken (App.unpictured).

// pictureWait is how long a problem's report waits for the window's
// picture: a window that does not answer leaves it without one.
const pictureWait = 2 * time.Second

// maskPaths has the window's picture cover obj's texts that name the
// user, their home folder or the output folder.
func (a *App) maskPaths(obj fyne.CanvasObject) { a.masks[obj] = false }

// maskAlways has the window's picture cover obj whatever it shows.
func (a *App) maskAlways(obj fyne.CanvasObject) { a.masks[obj] = true }

// screenshot takes the window's picture for a problem's report. It waits
// on the toolkit's queue, so never runs on the toolkit's goroutine: there
// it would hold the window still for pictureWait, and get no picture.
func (a *App) screenshot() image.Image {
	got := make(chan image.Image, 1)
	fyne.Do(func() { got <- a.picture() })
	select {
	case img := <-got:
		return img
	case <-time.After(pictureWait):
		return nil
	}
}

// picture is the window as it shows, each masked widget on screen blurred,
// or nil while the window is hidden in the tray, shows a dialog
// it cannot cover or cannot be pictured. It runs on the toolkit's
// goroutine.
func (a *App) picture() (img image.Image) {
	defer func() {
		if recover() != nil {
			img = nil
		}
	}()
	if a.notes.hidden || a.unpictured > 0 {
		return nil
	}
	c := a.win.Canvas()
	shot := c.Capture()
	if shot == nil || shot.Bounds().Empty() {
		return nil
	}
	out := image.NewNRGBA(shot.Bounds())
	draw.Draw(out, out.Bounds(), shot, shot.Bounds().Min, draw.Src)
	for _, box := range a.maskBoxes(c) {
		blur(out, box)
	}
	return out
}

// blur paints box as each of its rows' mean colour, softened from row to
// row: a line of text turns into a smudge with none of its glyphs, as a
// blur that kept them in order could be matched against a guess at the
// text blurred the same way.
func blur(img *image.NRGBA, box image.Rectangle) {
	box = box.Intersect(img.Bounds())
	if box.Empty() {
		return
	}
	rows := make([][4]float64, box.Dy())
	for y := range rows {
		for x := box.Min.X; x < box.Max.X; x++ {
			p := img.NRGBAAt(x, box.Min.Y+y)
			rows[y][0] += float64(p.R)
			rows[y][1] += float64(p.G)
			rows[y][2] += float64(p.B)
			rows[y][3] += float64(p.A)
		}
		for i := range rows[y] {
			rows[y][i] /= float64(box.Dx())
		}
	}
	// Three box blurs make about a Gaussian one; an eighth of the box's
	// height leaves a line of text a band with soft edges.
	for range 3 {
		rows = soften(rows, box.Dy()/8)
	}
	for y, m := range rows {
		c := color.NRGBA{R: uint8(m[0] + .5), G: uint8(m[1] + .5), B: uint8(m[2] + .5), A: uint8(m[3] + .5)}
		row := image.Rect(box.Min.X, box.Min.Y+y, box.Max.X, box.Min.Y+y+1)
		draw.Draw(img, row, image.NewUniform(c), image.Point{}, draw.Src)
	}
}

// soften is rows each the mean of those within r of it, the box's edge
// row standing for those past it.
func soften(rows [][4]float64, r int) [][4]float64 {
	out := make([][4]float64, len(rows))
	for y := range rows {
		for d := -r; d <= r; d++ {
			near := rows[min(max(y+d, 0), len(rows)-1)]
			for i := range near {
				out[y][i] += near[i]
			}
		}
		for i := range out[y] {
			out[y][i] /= float64(2*r + 1)
		}
	}
	return out
}

// maskBoxes are where the masked widgets on screen show, in the picture's
// pixels.
func (a *App) maskBoxes(c fyne.Canvas) []image.Rectangle {
	var boxes []image.Rectangle
	for obj, always := range a.masks {
		at := a.fa.Driver().AbsolutePositionForObject(obj)
		// Fyne puts what is not on screen at (0, 0), where no masked
		// widget sits: the window's edge is padded.
		if at.IsZero() || !obj.Visible() {
			continue
		}
		if always {
			boxes = append(boxes, pixelBox(c, at, obj.Size()))
			continue
		}
		boxes = append(boxes, privateBoxes(c, obj, at, a.tel.Private)...)
	}
	return boxes
}

// pixelBox is where size at at shows on c, in the picture's pixels.
func pixelBox(c fyne.Canvas, at fyne.Position, size fyne.Size) image.Rectangle {
	x0, y0 := c.PixelCoordinateForPosition(at)
	x1, y1 := c.PixelCoordinateForPosition(at.Add(size))
	return image.Rect(x0, y0, x1, y1)
}

// privateBoxes are where the texts shown at or under obj, which is at at,
// name what private finds: that part of a line Fyne shows whole, else the
// widget showing it.
func privateBoxes(c fyne.Canvas, obj fyne.CanvasObject, at fyne.Position, private func(string) [][2]int) []image.Rectangle {
	var boxes []image.Rectangle
	eachText(obj, at, func(text string, at fyne.Position, size fyne.Size, l *line) {
		spans := private(text)
		if len(spans) == 0 {
			return
		}
		if l == nil {
			boxes = append(boxes, pixelBox(c, at, size))
			return
		}
		// A little past each end, for glyphs that overhang their advance.
		margin := theme.Padding() / 2
		for _, sp := range spans {
			x0 := max(l.at(text, sp[0])-margin, 0)
			x1 := min(l.at(text, sp[1])+margin, size.Width)
			if x0 < x1 {
				boxes = append(boxes, pixelBox(c, at.AddXY(x0, 0), fyne.NewSize(x1-x0, size.Height)))
			}
		}
	})
	return boxes
}

// line is a text Fyne shows whole on one line, from x in its widget, at a
// size and style.
type line struct {
	x, size float32
	style   fyne.TextStyle
}

// at is where text[:i] ends on the line.
func (l *line) at(text string, i int) float32 {
	return l.x + fyne.MeasureText(text[:i], l.size, l.style).Width
}

// paddedLine is how a label or rich text shows text in its width, a
// padding in from each side; nil where it would wrap it or not start it at
// the left. Cut short, the text keeps its place: what is past the cut is
// not drawn.
func paddedLine(text string, width float32, align fyne.TextAlign, wrap fyne.TextWrap, th fyne.Theme, size fyne.ThemeSizeName, style fyne.TextStyle) *line {
	if size == "" {
		size = theme.SizeNameText
	}
	pad := th.Size(theme.SizeNameInnerPadding)
	l := &line{x: pad, size: th.Size(size), style: style}
	wraps := wrap == fyne.TextWrapWord || wrap == fyne.TextWrapBreak
	if align != fyne.TextAlignLeading || strings.Contains(text, "\n") || wraps && l.at(text, len(text))+pad > width {
		return nil
	}
	return l
}

// richLine is how rich text of a single segment shows, as paddedLine.
func richLine(rt *widget.RichText) *line {
	if len(rt.Segments) != 1 || rt.Scroll != fyne.ScrollNone {
		return nil
	}
	seg, ok := rt.Segments[0].(*widget.TextSegment)
	if !ok {
		return nil
	}
	return paddedLine(seg.Text, rt.Size().Width, seg.Style.Alignment, rt.Wrapping, rt.Theme(), seg.Style.SizeName, seg.Style.TextStyle)
}

// textLine is how a canvas text shows, from its left edge.
func textLine(t *canvas.Text) *line {
	l := &line{size: t.TextSize, style: t.TextStyle}
	if t.Alignment != fyne.TextAlignLeading || t.TextSize <= 0 || strings.Contains(t.Text, "\n") || l.at(t.Text, len(t.Text)) > t.Size().Width {
		return nil
	}
	return l
}

// eachText calls f with each text shown at or under obj, which is at at,
// and where it shows; under containers only, as Fyne opens no widget.
func eachText(obj fyne.CanvasObject, at fyne.Position, f func(text string, at fyne.Position, size fyne.Size, l *line)) {
	if obj == nil || !obj.Visible() {
		return
	}
	switch o := obj.(type) {
	case *fyne.Container:
		for _, child := range o.Objects {
			if child != nil {
				eachText(child, at.Add(child.Position()), f)
			}
		}
	case *widget.Label:
		f(o.Text, at, o.Size(), paddedLine(o.Text, o.Size().Width, o.Alignment, o.Wrapping, o.Theme(), o.SizeName, o.TextStyle))
	case *widget.RichText:
		f(o.String(), at, o.Size(), richLine(o))
	case *widget.Entry:
		f(o.Text, at, o.Size(), nil)
	case *widget.Hyperlink:
		f(o.Text, at, o.Size(), nil)
	case *canvas.Text:
		f(o.Text, at, o.Size(), textLine(o))
	}
}
