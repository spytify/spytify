package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed assets/logo.png
var logoPNG []byte

// Logo is the app and tray icon.
var Logo = fyne.NewStaticResource("logo.png", logoPNG)

//go:embed assets/logo-wordmark.png
var wordmarkPNG []byte

// Wordmark is the logo with the name beside it, for the app bar and the About screen.
var Wordmark = fyne.NewStaticResource("logo-wordmark.png", wordmarkPNG)

//go:embed assets/open-in-new.svg
var openInNewSVG []byte

// openInNewIcon marks a button that opens a page in the browser: Material
// Design's open_in_new, like Fyne's own icons, in the button's colour.
var openInNewIcon = theme.NewThemedResource(fyne.NewStaticResource("open-in-new.svg", openInNewSVG))

//go:embed assets/heart.png
var heartPNG []byte

// heartIcon is the app bar's way to Donate: v1's heart, from its icons, in
// its pink in both themes.
var heartIcon = fyne.NewStaticResource("heart.png", heartPNG)

//go:embed assets/new-releases.png
var newReleasesPNG []byte

// newReleasesIcon marks a newer release: v1's badge, from its icons, in its
// green with the mark cut out.
var newReleasesIcon = fyne.NewStaticResource("new-releases.png", newReleasesPNG)
