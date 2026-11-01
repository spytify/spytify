//go:build !linux

package ui

// watchTrayHost is Linux only: macOS and Windows always show the tray
// icon.
func watchTrayHost(func(present bool)) {}
