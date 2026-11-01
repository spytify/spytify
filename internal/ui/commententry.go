package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// commentEntry is a field of several lines that Tab leaves, as it leaves
// the others. Fyne's keeps Tab, and Shift+Tab, to type a tab, so the
// keyboard could not get past it to the dialog's buttons.
type commentEntry struct {
	widget.Entry
}

func newCommentEntry() *commentEntry {
	e := &commentEntry{}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	return e
}

// AcceptsTab is false: Tab moves the focus on.
func (e *commentEntry) AcceptsTab() bool { return false }
