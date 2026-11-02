package ui

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// walk calls visit on o and on everything under it, hidden or not, through
// containers and widgets alike, going under an object while visit says so.
func walk(o fyne.CanvasObject, visit func(fyne.CanvasObject) bool) {
	if o == nil || !visit(o) {
		return
	}
	switch w := o.(type) {
	case *fyne.Container:
		for _, c := range w.Objects {
			walk(c, visit)
		}
	case fyne.Widget:
		for _, c := range test.WidgetRenderer(w).Objects() {
			walk(c, visit)
		}
	}
}

// visible are the visible objects under o that are a T or are built on
// one, as T.
func visible[T fyne.CanvasObject](o fyne.CanvasObject) []T {
	var out []T
	walk(o, func(o fyne.CanvasObject) bool {
		if !o.Visible() {
			return false
		}
		if v, ok := as[T](o); ok {
			out = append(out, v)
		}
		return true
	})
	return out
}

// as is o as a T: o itself, or the T it is built on, as the app's buttons
// are built on Fyne's, however deep.
func as[T fyne.CanvasObject](o fyne.CanvasObject) (T, bool) {
	if v, ok := o.(T); ok {
		return v, true
	}
	var none T
	want, v := reflect.TypeFor[T](), reflect.ValueOf(o)
	if want.Kind() != reflect.Pointer || v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return none, false
	}
	f, ok := embedded(v.Elem(), want.Elem())
	if !ok {
		return none, false
	}
	// NewAt: reflect hands out nothing reached through the app's
	// unexported types, such as litButton's tipButton.
	return reflect.NewAt(want.Elem(), f.Addr().UnsafePointer()).Interface().(T), true
}

// embedded is the field of type want that s embeds, however deep.
func embedded(s reflect.Value, want reflect.Type) (reflect.Value, bool) {
	for i := range s.NumField() {
		if !s.Type().Field(i).Anonymous {
			continue
		}
		switch f := s.Field(i); {
		case f.Type() == want:
			return f, true
		case f.Kind() == reflect.Struct:
			if g, ok := embedded(f, want); ok {
				return g, true
			}
		}
	}
	return reflect.Value{}, false
}

// texts collects every label and button text under o.
func texts(o fyne.CanvasObject) []string {
	var out []string
	walk(o, func(o fyne.CanvasObject) bool {
		if !o.Visible() {
			return false
		}
		if b, ok := as[*widget.Button](o); ok {
			out = append(out, b.Text)
		}
		switch w := o.(type) {
		case *widget.Label:
			out = append(out, w.Text)
		case *widget.RichText:
			for _, seg := range w.Segments {
				if ts, ok := seg.(*widget.TextSegment); ok {
					out = append(out, ts.Text)
				}
			}
		case *canvas.Text:
			out = append(out, w.Text)
		case *widget.Hyperlink:
			out = append(out, w.Text)
		case *widget.Card:
			out = append(out, w.Title)
			out = append(out, texts(w.Content)...)
			return false
		}
		return true
	})
	return out
}

// buttons are the visible buttons under o showing text, the app's own
// included.
func buttons(o fyne.CanvasObject, text string) []*widget.Button {
	var out []*widget.Button
	for _, b := range visible[*widget.Button](o) {
		if b.Text == text {
			out = append(out, b)
		}
	}
	return out
}

// buttonObjects are the buttons showing text under o as the window holds
// them, the app's own included, to find where they are drawn.
func buttonObjects(o fyne.CanvasObject, text string) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	walk(o, func(o fyne.CanvasObject) bool {
		if !o.Visible() {
			return false
		}
		if b, ok := as[*widget.Button](o); ok && b.Text == text {
			out = append(out, o)
		}
		return true
	})
	return out
}

func tapButton(t *testing.T, o fyne.CanvasObject, text string) {
	t.Helper()
	b := buttons(o, text)
	if len(b) != 1 {
		t.Fatalf("%d %q buttons", len(b), text)
	}
	test.Tap(b[0])
}

// iconButtons are the visible buttons that show only icon.
func iconButtons(o fyne.CanvasObject, icon fyne.Resource) []*widget.Button {
	var out []*widget.Button
	for _, b := range visible[*widget.Button](o) {
		if b.Text == "" && b.Icon != nil && b.Icon.Name() == icon.Name() && !b.Disabled() {
			out = append(out, b)
		}
	}
	return out
}

// labels are the visible labels under o with this text.
func labels(o fyne.CanvasObject, text string) []*widget.Label {
	return labelsIn(o, func(s string) bool { return s == text })
}

// labelsIn are the labels shown under o whose text keep takes.
func labelsIn(o fyne.CanvasObject, keep func(string) bool) []*widget.Label {
	var out []*widget.Label
	for _, l := range visible[*widget.Label](o) {
		if keep(l.Text) {
			out = append(out, l)
		}
	}
	return out
}

// richTexts are the visible rich texts that read text.
func richTexts(o fyne.CanvasObject, text string) []*widget.RichText {
	var out []*widget.RichText
	for _, rt := range visible[*widget.RichText](o) {
		if rt.String() == text {
			out = append(out, rt)
		}
	}
	return out
}

// richText is the visible rich text under o that starts with text.
func richText(o fyne.CanvasObject, text string) *widget.RichText {
	for _, rt := range visible[*widget.RichText](o) {
		if ts, ok := rt.Segments[0].(*widget.TextSegment); ok && ts.Text == text {
			return rt
		}
	}
	return nil
}

// shown is the text a rich text draws, an ellipsis included when it is
// cut.
func shown(r *widget.RichText) string {
	var out []string
	for _, o := range test.WidgetRenderer(r).Objects() {
		if t, ok := o.(*canvas.Text); ok {
			out = append(out, t.Text)
		}
	}
	return strings.Join(out, "\n")
}

// checkBoxes are the visible check boxes with this label.
func checkBoxes(o fyne.CanvasObject, label string) []*widget.Check {
	var out []*widget.Check
	for _, c := range visible[*widget.Check](o) {
		if c.Text == label {
			out = append(out, c)
		}
	}
	return out
}

// images are the names of the visible images' resources.
func images(o fyne.CanvasObject) []string {
	var out []string
	for _, img := range visible[*canvas.Image](o) {
		if img.Resource != nil {
			out = append(out, img.Resource.Name())
		} else if img.Image != nil {
			out = append(out, picture(img.Image))
		}
	}
	return out
}

// picture names an image shown from its pixels, such as a cover's thumbnail.
func picture(img image.Image) string { return fmt.Sprintf("picture %p", img) }

// findObject is the first object under o that match accepts, and where it
// is in o: the test driver places only what a canvas has drawn.
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

// holds reports whether o is in root's tree, hidden or not.
func holds(root, o fyne.CanvasObject) bool {
	found := false
	walk(root, func(c fyne.CanvasObject) bool {
		found = found || c == o
		return !found
	})
	return found
}
