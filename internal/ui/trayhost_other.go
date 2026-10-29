//go:build !linux

package ui

// lateTrayShows says a tray set up once the toolkit's loop runs shows.
const lateTrayShows = true

// watchTrayHost is Linux only: macOS and Windows always show the tray
// icon.
func watchTrayHost(func(present bool)) {}
