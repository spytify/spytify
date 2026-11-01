package ui

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// untitled is the folder the recorder files an album with no name in, with
// its year when the year is known.
var untitled = regexp.MustCompile(`(?i)^untitled(?:[ _]\(\d+\))?$`)

// albumOf is the album f is filed under: the album folder the recorder puts
// it in, under its artist's, as the Group by folders setting does by
// default; else the album its tags name. key is empty for no album, and
// read false while the tags that may name it are not read.
func (l *libraryScreen) albumOf(f libraryFile) (key, name, artist string, read bool) {
	dir := filepath.Dir(f.rel)
	if up := filepath.Dir(dir); dir != "." && up != "." {
		if name := filepath.Base(dir); !untitled.MatchString(name) {
			return dir, name, filepath.Base(up), true
		}
		return "", "", "", true
	}
	t, read := l.tagged.known(f.key())
	if strings.TrimSpace(t.album) == "" {
		return "", "", "", read
	}
	// A NUL starts no folder's name: an album from tags never takes a
	// folder's files.
	return "\x00" + strings.ToLower(t.album+"\x1f"+t.albumArtist), t.album, t.albumArtist, true
}

// albums puts files under a heading per album, the albums in order of name
// and the files with none last, under a heading of their own; each album
// keeps its files in the order they come. It reads no tags: ffmpeg takes
// tens of milliseconds for each file, and a library holds thousands. unread
// is the keys of the files whose tags, not read yet, may name their album.
func (l *libraryScreen) albums(files []libraryFile) (rows []libraryRow, unread []string) {
	type album struct {
		key, name, artist string
		files             []libraryFile
	}
	byKey := map[string]*album{}
	var all []*album
	for _, f := range files {
		key, name, artist, read := l.albumOf(f)
		if !read {
			unread = append(unread, f.key())
		}
		a := byKey[key]
		if a == nil {
			a = &album{key: key, name: name, artist: artist}
			byKey[key] = a
			all = append(all, a)
		}
		a.files = append(a.files, f)
	}
	slices.SortFunc(all, func(a, b *album) int {
		if (a.key == "") != (b.key == "") {
			if a.key == "" {
				return 1
			}
			return -1
		}
		return cmp.Or(cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name)),
			cmp.Compare(strings.ToLower(a.artist), strings.ToLower(b.artist)), cmp.Compare(a.key, b.key))
	})
	rows = make([]libraryRow, 0, len(files)+len(all))
	for _, a := range all {
		heading, note := a.name, countFiles(len(a.files))
		if a.key == "" {
			heading = noAlbum
		}
		if a.artist != "" {
			note = a.artist + " · " + note
		}
		rows = append(rows, libraryRow{heading: heading, note: note})
		for _, f := range a.files {
			rows = append(rows, libraryRow{file: f})
		}
	}
	return rows, unread
}

// albumHeading is the row over an album's files: its name, then its artist
// and how many files it has, at the foot of a row as tall as a file's, the
// space above setting the albums apart.
func albumHeading() *fyne.Container {
	text := widget.NewRichText(
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNameForeground, SizeName: theme.SizeNameText, TextStyle: fyne.TextStyle{Bold: true}, Inline: true}},
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameCaptionText, Inline: true}},
	)
	text.Truncation = fyne.TextTruncateEllipsis
	return container.NewVBox(layout.NewSpacer(), text)
}

func setAlbumHeading(heading *fyne.Container, name, note string) {
	text := heading.Objects[1].(*widget.RichText)
	nameSeg, noteSeg := text.Segments[0].(*widget.TextSegment), text.Segments[1].(*widget.TextSegment)
	if nameSeg.Text != name || noteSeg.Text != "   "+note {
		nameSeg.Text, noteSeg.Text = name, "   "+note
		text.Refresh()
	}
}
