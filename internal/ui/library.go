package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/model"
)

type libraryFile struct {
	path    string
	rel     string
	size    int64
	modTime time.Time
}

// libraryScreen lists every audio file in the output folder, newest first,
// by name or by album, with a search box and a filter of the partial takes.
// The SQLite index of the plan replaces the folder scan when bulk re-tagging
// lands.
type libraryScreen struct {
	app    *App
	root   fyne.CanvasObject
	search *widget.Entry
	sortBy *widget.Select
	list   *widget.List
	card   fyne.CanvasObject
	count  *widget.RichText
	empty  fyne.CanvasObject
	hint   *widget.Label
	takes  map[string]*widget.Check // the filter's box for each kind of file
	kinds  *checkSelect             // the filter: those boxes, in a dropdown

	folder string
	all    []libraryFile
	shown  []libraryFile
	// rows is the list's: the files shown, under their albums' headings in
	// the Album view. unread is the keys of the files there whose album only
	// their tags can tell, not read yet.
	rows   []libraryRow
	unread []string

	covers *covers
	tagged *tagCache
	ffOnce sync.Once
	ff     *encode.FFmpeg
	// redraw is set while a redraw for covers or tags that came in waits
	// for the toolkit's goroutine: those that come in meanwhile join it.
	redraw atomic.Bool
}

// libraryRow is a row of the list: a file, or the heading of an album.
type libraryRow struct {
	file    libraryFile
	heading string // the album's name, on a heading's row
	note    string // the album's artist and how many files it has
}

// The Album sort, the kinds of file the filter ticks, and the heading of
// the files with no album.
const (
	byAlbum        = "Album"
	takeComplete   = "Complete"
	takePartial    = "Partial"
	takeLowQuality = "Low quality"
	noAlbum        = "No album"
)

var takeKinds = []string{takeComplete, takePartial, takeLowQuality}

func newLibraryScreen(a *App) *libraryScreen {
	l := &libraryScreen{app: a}
	loaded := func() {
		// Tests draw by hand: see Options.ManualRender.
		if !a.opts.ManualRender && l.redraw.CompareAndSwap(false, true) {
			fyne.Do(func() {
				l.redraw.Store(false)
				l.loaded()
			})
		}
	}
	l.covers = newCovers(loaded)
	l.tagged = &tagCache{read: map[string]trackTags{}, pending: map[string]bool{}, loaded: loaded, slots: make(chan struct{}, 2)}
	l.search = widget.NewEntry()
	l.search.SetPlaceHolder("Search artist, title or album")
	l.search.OnChanged = func(string) { l.filter() }
	l.sortBy = widget.NewSelect([]string{"Newest", "Name", byAlbum}, func(string) { l.filter() })
	// Keep anyway saves a take cut short or in low quality: the filter hides
	// those files, or shows them alone to clean them up, a box per kind of
	// file in its dropdown. They start ticked without their callback, which
	// needs the list: the sort's, below, draws the list first.
	l.takes = map[string]*widget.Check{}
	boxes := container.NewVBox()
	for _, kind := range takeKinds {
		box := newMenuBox(kind, func(bool) { l.filter() })
		box.Checked = true
		l.takes[kind] = &box.Check
		boxes.Add(box)
	}
	l.kinds = newCheckSelect(boxes)
	l.kinds.Selected = l.takesLabel()
	// A select is as wide as its placeholder, which a choice made never
	// shows: the longest label fits.
	l.kinds.PlaceHolder = takeComplete + ", " + takeLowQuality
	rescan := newIconButton("Rescan", theme.ViewRefreshIcon(), func() {
		a.noteAction("rescan the library")
		l.scan()
	})
	rescan.Importance = widget.LowImportance
	openFolder := newIconButton("Open folder", theme.FolderOpenIcon(), func() {
		a.noteAction("open the library's folder")
		if l.folder != "" {
			_ = openPath(l.folder)
		}
	})
	openFolder.Importance = widget.LowImportance
	// The count names the folder, a long path: it gives way to the footer's
	// buttons as the window narrows.
	l.count = clipped("")
	l.count.Segments[0].(*widget.TextSegment).Style.ColorName = colorLabel
	l.hint = widget.NewLabel("")
	l.empty = emptyState("No recordings yet", l.hint)
	l.list = widget.NewList(
		func() int { return len(l.rows) },
		func() fyne.CanvasObject {
			// The title and the artists, as the player lists a track; or in
			// the Album view, an album's heading. A take Keep anyway saved
			// says which in a badge, as the Record screen's rows say theirs.
			reveal, play := newIconButton("", theme.SearchIcon(), nil), newIconButton("", theme.MediaPlayIcon(), nil)
			reveal.Importance, play.Importance = widget.LowImportance, widget.LowImportance
			return container.NewStack(trackRow(2, container.NewHBox(container.NewCenter(newBadge("", colorSkippedBadge, "", colorOnSkippedBadge)), reveal, play)), albumHeading())
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(l.rows) {
				return
			}
			r := l.rows[id]
			stack := o.(*fyne.Container)
			box, heading := stack.Objects[0].(*fyne.Container), stack.Objects[1].(*fyne.Container)
			show(box, r.heading == "")
			show(heading, r.heading != "")
			if r.heading != "" {
				setAlbumHeading(heading, r.heading, r.note)
				return
			}
			f := r.file
			// Border layout: center first, then the leading and trailing parts.
			after := box.Objects[2].(*fyne.Container)
			title, artist := l.tags(f)
			kind, badgeBox := takeKind(f.rel), after.Objects[0].(*fyne.Container)
			show(badgeBox, kind != takeComplete)
			if kind != takeComplete {
				title = takeMarker.ReplaceAllString(title, "")
				badgeBox.Objects[0].(*badge).set(kind, colorSkippedBadge, colorOnSkippedBadge)
			}
			setTrackRow(box, l.cover(f).row, title, artist)
			path := f.path
			after.Objects[1].(*button).OnTapped = func() { l.app.noteAction("show a file in its folder"); _ = reveal(path) }
			after.Objects[2].(*button).OnTapped = func() { l.app.noteAction("play a file"); _ = openPath(path) }
			box.Refresh()
		},
	)
	l.card = listCard(l.list)
	// SetSelected runs the callback, which needs the list, its card and the
	// count.
	l.sortBy.SetSelected("Newest")
	// The search and the sort are white fields on the page, as in Settings.
	top := container.NewThemeOverride(container.NewBorder(nil, nil, nil, container.NewHBox(l.sortBy, l.kinds), l.search), cardTheme{input: colorCard})
	// The files from one side of the window to the other, their count and
	// folder at the bottom, as the Record screen's session.
	l.root = inReadingOrder(inset(top), footerBar(l.count, container.NewHBox(rescan, openFolder)), nil, nil, container.NewStack(l.card, l.empty))
	return l
}

// cover is a file's art: the picture embedded in it, or for WAV, which has
// no tags, the cover URL in the .json beside it. None while it loads.
func (l *libraryScreen) cover(f libraryFile) thumbs {
	if strings.EqualFold(filepath.Ext(f.path), ".wav") {
		var side struct {
			CoverURL string `json:"cover_url"`
		}
		if b, err := os.ReadFile(f.path + ".json"); err == nil && json.Unmarshal(b, &side) == nil {
			return l.covers.get(side.CoverURL)
		}
		return thumbs{}
	}
	ff := l.ffmpeg()
	if ff == nil {
		return thumbs{}
	}
	path := f.path
	return l.covers.load(f.key(), func(ctx context.Context) ([]byte, error) { return ff.Cover(ctx, path) })
}

// ffmpeg reads the files' covers and tags; nil without one.
func (l *libraryScreen) ffmpeg() *encode.FFmpeg {
	l.ffOnce.Do(func() {
		name := os.Getenv("SPYTIFY_FFMPEG")
		if name == "" {
			name = "ffmpeg"
		}
		l.ff, _ = encode.Find(name)
	})
	return l.ff
}

// key tells a file's versions apart: one written again has another.
func (f libraryFile) key() string {
	return fmt.Sprintf("%s\x00%d\x00%d", f.path, f.size, f.modTime.UnixNano())
}

func (l *libraryScreen) content() fyne.CanvasObject { return l.root }

func (l *libraryScreen) render(s model.State) {
	dir, _ := s.Settings[settings.OutputDir.String()].(string)
	dir = settings.ExpandHome(dir)
	if dir != l.folder {
		l.folder = dir
		l.scan()
	}
}

func (l *libraryScreen) scan() {
	dir := l.folder
	if l.app.opts.ManualRender {
		l.all = scanAudio(dir)
		l.filter()
		return
	}
	go func() {
		files := scanAudio(dir)
		fyne.Do(func() {
			l.all = files
			l.filter()
		})
	}()
}

func scanAudio(root string) []libraryFile {
	var out []libraryFile
	if root == "" {
		return nil
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".spytify" {
			return filepath.SkipDir
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".mp3", ".flac", ".m4a", ".wav":
		default:
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, libraryFile{path: path, rel: rel, size: info.Size(), modTime: info.ModTime()})
		return nil
	})
	return out
}

func (l *libraryScreen) filter() {
	q := strings.ToLower(strings.TrimSpace(l.search.Text))
	var shown []libraryFile
	for _, f := range l.all {
		if (q == "" || strings.Contains(strings.ToLower(f.rel), q)) && l.kept(f) {
			shown = append(shown, f)
		}
	}
	// The Album view keeps this order within each album.
	if l.sortBy.Selected == "Name" || l.sortBy.Selected == byAlbum {
		sort.Slice(shown, func(i, j int) bool { return strings.ToLower(shown[i].rel) < strings.ToLower(shown[j].rel) })
	} else {
		sort.Slice(shown, func(i, j int) bool { return shown[i].modTime.After(shown[j].modTime) })
	}
	rows := make([]libraryRow, 0, len(shown))
	var unread []string
	if l.sortBy.Selected == byAlbum {
		rows, unread = l.albums(shown)
		shown = shown[:0]
		for _, r := range rows {
			if r.heading == "" {
				shown = append(shown, r.file)
			}
		}
	} else {
		for _, f := range shown {
			rows = append(rows, libraryRow{file: f})
		}
	}
	l.shown, l.rows, l.unread = shown, rows, unread
	if label := l.takesLabel(); l.kinds.Selected != label {
		l.kinds.Selected = label
		l.kinds.Refresh()
	}
	setCaption(l.count, pluralFiles(len(shown), len(l.all), l.folder))
	l.card.Refresh()
	switch {
	case len(l.all) == 0:
		l.hint.SetText("Recorded tracks land in " + l.folder + ".")
		l.empty.Show()
	case len(shown) == 0 && q == "" && !slices.ContainsFunc(takeKinds, func(k string) bool { return l.takes[k].Checked }):
		l.hint.SetText("Tick a kind of file to show.")
		l.empty.Show()
	case len(shown) == 0 && q == "" && !l.takes[takeComplete].Checked:
		l.hint.SetText("None yet: Keep anyway saves a take cut short or in low quality.")
		l.empty.Show()
	case len(shown) == 0 && q == "":
		l.hint.SetText("Every file here is a take cut short or in low quality.")
		l.empty.Show()
	case len(shown) == 0:
		l.hint.SetText("Nothing matches \"" + l.search.Text + "\".")
		l.empty.Show()
	default:
		l.empty.Hide()
	}
	show(l.card, !l.empty.Visible())
}

// kept tells whether the filter keeps f: its kind is ticked.
func (l *libraryScreen) kept(f libraryFile) bool {
	return l.takes[takeKind(f.rel)].Checked
}

// takeKind tells whether the file at path is a take that Keep anyway
// saved, partial or in low quality, or a complete one: the recorder adds
// " (partial)" or " (low quality)" to such a take's title, and the name
// template puts the title in the file's name, its spaces made underscores
// when the setting says so, with a number after it when the name was
// taken. Its tags say so too, but are not read yet for most files, and the
// index of recorded tracks leaves these files out.
func takeKind(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(name, "(partial)"):
		return takePartial
	case strings.Contains(name, "(low quality)"), strings.Contains(name, "(low_quality)"):
		return takeLowQuality
	}
	return takeComplete
}

// takeMarker is the marker Keep anyway adds to a take's title, and the
// space before it: the row shows the kind in its chip instead.
var takeMarker = regexp.MustCompile(`(?i)[ _]\((partial|low[ _]quality)\)`)

// takesLabel says which kinds of file the filter shows.
func (l *libraryScreen) takesLabel() string {
	var ticked []string
	for _, kind := range takeKinds {
		if l.takes[kind].Checked {
			ticked = append(ticked, kind)
		}
	}
	switch len(ticked) {
	case 0:
		return "No files"
	case len(takeKinds):
		return "All files"
	}
	return strings.Join(ticked, ", ")
}

func countFiles(n int) string {
	if n == 1 {
		return "1 file"
	}
	return itoa(n) + " files"
}

// loaded draws the rows again once covers or tags came in. In the Album
// view, the tags of a file that no album folder holds may name its album:
// the files are grouped again then, and only then, as grouping thousands
// of files for each cover would make scrolling stutter.
func (l *libraryScreen) loaded() {
	if l.sortBy.Selected == byAlbum && slices.ContainsFunc(l.unread, func(key string) bool {
		_, read := l.tagged.known(key)
		return read
	}) {
		l.filter()
		return
	}
	l.list.Refresh()
}

func pluralFiles(shown, all int, folder string) string {
	if folder == "" {
		return ""
	}
	if shown == all {
		return itoa(all) + " files in " + folder
	}
	return itoa(shown) + " of " + itoa(all) + " files in " + folder
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
