package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"
)

// button is widget.Button pressed by Space without losing the focus. Fyne
// 2.8.2's button takes Space as a tap, and a tap gives the focus to
// nothing, as a click on a button does: the next Tab started again from
// the first control of the window.
type button struct {
	widget.Button
}

func newButton(text string, tapped func()) *button {
	return newIconButton(text, nil, tapped)
}

func newIconButton(text string, icon fyne.Resource, tapped func()) *button {
	b := &button{}
	b.Text, b.Icon, b.OnTapped = text, icon, tapped
	b.ExtendBaseWidget(b)
	return b
}

// TypedKey presses the button on Space, as Fyne's does on the key, not its
// character: the buttons of Fyne's dialogs close them on the key, and the
// character then goes to the button that opened the dialog. Escape goes on
// to the window, which closes the modal the button is in.
func (b *button) TypedKey(e *fyne.KeyEvent) {
	pressOnSpace(&b.Button, e)
	escapeFrom(b, e)
}

// pressOnSpace presses b on Space, leaving the focus where it is, for Tab
// to go on from there, once closed a dialog b opens.
func pressOnSpace(b *widget.Button, e *fyne.KeyEvent) {
	if e.Name == fyne.KeySpace && !b.Disabled() && b.OnTapped != nil {
		b.OnTapped()
	}
}

// tipButton is a button with a tooltip, pressed by Space as button is.
type tipButton struct {
	ttwidget.Button
}

func newTipButton(text string, icon fyne.Resource, tapped func()) *tipButton {
	b := &tipButton{}
	b.Text, b.Icon, b.OnTapped = text, icon, tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *tipButton) TypedKey(e *fyne.KeyEvent) {
	pressOnSpace(&b.Button.Button, e)
	escapeFrom(b, e)
}
