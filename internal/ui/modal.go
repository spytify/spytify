package ui

import (
	"slices"
	"sync"
	"unicode"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Escape closes the modal on top of the window, as its Close, Cancel or No
// does. Fyne 2.8.2 hands Escape to the control focused, or to the window
// when nothing is, and neither its dialogs nor its controls take it: the
// app's controls pass it on to their window, the window takes it when
// nothing is focused, and the app's dialogs have the app's buttons.

var (
	escapesMu sync.Mutex
	// escapes is, for each window's canvas, what Escape does to each modal
	// shown on it.
	escapes = map[fyne.Canvas]map[fyne.CanvasObject]func(){}
)

// closesOnEscape makes Escape do close to the modal just shown on w, the
// overlay on top, and gives the modal the focus.
func closesOnEscape(w fyne.Window, close func()) {
	c := w.Canvas()
	shown := c.Overlays().List()
	escapesMu.Lock()
	closes := escapes[c]
	if closes == nil {
		closes = map[fyne.CanvasObject]func(){}
		escapes[c] = closes
	}
	// The modals closed by their buttons are gone.
	for o := range closes {
		if !slices.Contains(shown, o) {
			delete(closes, o)
		}
	}
	if top := c.Overlays().Top(); top != nil {
		closes[top] = close
	}
	escapesMu.Unlock()
	focusFirst(w)
}

// escape closes the modal on top of c, as its Close, Cancel or No does.
func escape(c fyne.Canvas) {
	if c == nil {
		return
	}
	top := c.Overlays().Top()
	escapesMu.Lock()
	close := escapes[c][top]
	delete(escapes[c], top)
	escapesMu.Unlock()
	if close != nil {
		close()
	}
}

// escapeFrom passes Escape on from the control o to its window.
func escapeFrom(o fyne.CanvasObject, e *fyne.KeyEvent) {
	if e.Name == fyne.KeyEscape {
		escape(fyne.CurrentApp().Driver().CanvasForObject(o))
	}
}

// newDialog is Fyne's dialog with the app's buttons: dismiss, then
// confirm, which stands out, with Fyne's cross and tick, or dismiss alone.
// A button closes the dialog, then answers, yes for confirm. Once the
// dialog is shown, closesOnEscape takes no, which answers as dismiss does.
func newDialog(w fyne.Window, title string, content fyne.CanvasObject, dismiss, confirm string, answered func(bool)) (d *dialog.CustomDialog, no func()) {
	d = dialog.NewCustomWithoutButtons(title, content, w)
	answer := func(yes bool) func() {
		return func() {
			d.Hide()
			if answered != nil {
				answered(yes)
			}
		}
	}
	if confirm == "" {
		d.SetButtons([]fyne.CanvasObject{newButton(dismiss, answer(false))})
	} else {
		d.SetButtons([]fyne.CanvasObject{
			newIconButton(dismiss, theme.CancelIcon(), answer(false)),
			newLitButton(confirm, theme.ConfirmIcon(), widget.HighImportance, answer(true)).lit,
		})
	}
	return d, answer(false)
}

// showDialog shows newDialog's dialog at size, or at its own if size is
// zero, and Escape answers it as dismiss does.
func showDialog(w fyne.Window, title string, content fyne.CanvasObject, dismiss, confirm string, size fyne.Size, answered func(bool)) {
	d, no := newDialog(w, title, content, dismiss, confirm, answered)
	if !size.IsZero() {
		d.Resize(size)
	}
	d.Show()
	closesOnEscape(w, no)
}

// showText is Fyne's text dialog with the app's buttons: the message
// centred and wrapped, the dialog as wide as it reads on one line, up to
// 600 or nine tenths of the window, as Fyne's is.
func showText(w fyne.Window, title, message, dismiss, confirm string, answered func(bool)) *dialog.CustomDialog {
	label := &widget.Label{Text: message, Alignment: fyne.TextAlignCenter, Wrapping: fyne.TextWrapWord}
	d, no := newDialog(w, title, label, dismiss, confirm, answered)
	// Fyne's dialog pads its content with 32 across, and a padding each side.
	oneLine := widget.NewLabel(message).MinSize().Width + 32 + theme.Padding()*2
	size := fyne.NewSize(min(oneLine, 600, w.Canvas().Size().Width*.9), 0)
	d.Resize(size)
	d.Show()
	// Its text wraps at the dialog's width, known once shown; the overlay
	// centres the dialog again as it lays it out.
	d.Resize(size)
	if top := w.Canvas().Overlays().Top(); top != nil {
		top.Refresh()
	}
	closesOnEscape(w, no)
	return d
}

// ask is dialog.ShowConfirm: No, then Yes.
func ask(w fyne.Window, title, message string, answered func(bool)) {
	showText(w, title, message, "No", "Yes", answered)
}

// inform is dialog.ShowInformation: OK.
func inform(w fyne.Window, title, message string) {
	showText(w, title, message, "OK", "", nil)
}

// capitalized is s with its first letter upper case, as Fyne's error dialog
// shows an error.
func capitalized(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// check is widget.Check, which passes Escape on to its window.
type check struct {
	widget.Check
}

func newCheck(label string) *check {
	c := &check{}
	c.Text = label
	c.ExtendBaseWidget(c)
	return c
}

func (c *check) TypedKey(e *fyne.KeyEvent) { escapeFrom(c, e) }
