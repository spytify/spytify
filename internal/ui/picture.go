package ui

import (
	"image"
	"image/color"
	"image/draw"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// The window's picture for a bug report covers what names the user, their
// home folder or the output folder, as the reports' scrubbing does. Fyne
// gives no way into a widget's parts, so the widgets that can show a path
// are listed as they are made: maskPaths for those covered when their
// text names one, maskAlways for those covered whatever they show. Fyne's
// own dialogs cannot be listed: while an error's or the folder picker is
// open, no picture is taken (App.unpictured).

// pictureWait is how long a problem's report waits for the window's
// picture: a window that does not answer leaves it without one.
const pictureWait = 2 * time.Second

// maskColor is the box over what the picture leaves out.
var maskColor = color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}

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

// picture is the window as it shows, with a box over each masked widget
// on screen, or nil while the window is hidden in the tray, shows a dialog
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
		draw.Draw(out, box, image.NewUniform(maskColor), image.Point{}, draw.Src)
	}
	return out
}

// maskBoxes are where the masked widgets on screen show, in the picture's
// pixels.
func (a *App) maskBoxes(c fyne.Canvas) []image.Rectangle {
	var boxes []image.Rectangle
	box := func(at fyne.Position, size fyne.Size) {
		x0, y0 := c.PixelCoordinateForPosition(at)
		x1, y1 := c.PixelCoordinateForPosition(at.Add(size))
		boxes = append(boxes, image.Rect(x0, y0, x1, y1))
	}
	for obj, always := range a.masks {
		at := a.fa.Driver().AbsolutePositionForObject(obj)
		// Fyne puts what is not on screen at (0, 0), where no masked
		// widget sits: the window's edge is padded.
		if at.IsZero() || !obj.Visible() {
			continue
		}
		if always {
			box(at, obj.Size())
			continue
		}
		eachText(obj, at, func(text string, at fyne.Position, size fyne.Size) {
			if a.tel.Private(text) {
				box(at, size)
			}
		})
	}
	return boxes
}

// eachText calls f with each text shown at or under obj, which is at at,
// and where it shows; under containers only, as Fyne opens no widget.
func eachText(obj fyne.CanvasObject, at fyne.Position, f func(text string, at fyne.Position, size fyne.Size)) {
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
		f(o.Text, at, o.Size())
	case *widget.RichText:
		f(o.String(), at, o.Size())
	case *widget.Entry:
		f(o.Text, at, o.Size())
	case *widget.Hyperlink:
		f(o.Text, at, o.Size())
	case *canvas.Text:
		f(o.Text, at, o.Size())
	}
}
