//go:build !darwin || !cgo

package ui

// showInDock and onReopen are macOS only: elsewhere the tray icon is all
// there is.
func showInDock(bool) {}

func onReopen(func()) {}
