package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/internal/ui/model"
)

type libraryFile struct {
	path    string
	rel     string
	size    int64
	modTime time.Time
}

// libraryScreen lists every audio file in the output folder, newest first,
// with a search box. The SQLite index of the plan replaces the folder scan
// when bulk re-tagging lands.
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

	folder string
	all    []libraryFile
	shown  []libraryFile

	covers *covers
	tagged *tagCache
	ffOnce sync.Once
	ff     *encode.FFmpeg
}

func newLibraryScreen(a *App) *libraryScreen {
	l := &libraryScreen{app: a}
	loaded := func() {
		// Tests draw by hand: see Options.ManualRender.
		if !a.opts.ManualRender {
			fyne.Do(l.list.Refresh)
		}
	}
	l.covers = newCovers(loaded)
	l.tagged = &tagCache{read: map[string]trackTags{}, pending: map[string]bool{}, loaded: loaded, slots: make(chan struct{}, 2)}
	l.search = widget.NewEntry()
	l.search.SetPlaceHolder("Search artist, title or album")
	l.search.OnChanged = func(string) { l.filter() }
	l.sortBy = widget.NewSelect([]string{"Newest", "Name"}, func(string) { l.filter() })
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		a.noteAction("rescan the library")
		l.scan()
	})
	l.count = caption("")
	l.hint = widget.NewLabel("")
	l.empty = emptyState("No recordings yet", l.hint)
	l.list = widget.NewList(
		func() int { return len(l.shown) },
		func() fyne.CanvasObject {
			// The title and the artists, as the player lists a track.
			return trackRow(2, container.NewHBox(
				&widget.Button{Icon: theme.SearchIcon(), Importance: widget.LowImportance},
				&widget.Button{Icon: theme.MediaPlayIcon(), Importance: widget.LowImportance}))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(l.shown) {
				return
			}
			f := l.shown[id]
			box := o.(*fyne.Container)
			// Border layout: center first, then the leading and trailing parts.
			buttons := box.Objects[2].(*fyne.Container)
			title, artist := l.tags(f)
			setTrackRow(box, l.cover(f).row, title, artist)
			path := f.path
			buttons.Objects[0].(*widget.Button).OnTapped = func() { l.app.noteAction("show a file in its folder"); _ = reveal(path) }
			buttons.Objects[1].(*widget.Button).OnTapped = func() { l.app.noteAction("play a file"); _ = openPath(path) }
			box.Refresh()
		},
	)
	l.card = listCard(l.list)
	// SetSelected runs the callback, which needs the list, its card and the
	// count.
	l.sortBy.SetSelected("Newest")
	// The search and the sort are white fields on the page, as in Settings.
	top := container.NewThemeOverride(container.NewBorder(nil, nil, nil, container.NewHBox(l.sortBy, refresh), l.search), cardTheme{input: colorCard})
	l.root = container.NewBorder(inset(top), inset(l.count), nil, nil, container.NewStack(inset(l.card), l.empty))
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

// tags is a file's title and artists, as its tags say once they are read,
// or for WAV, which has no tags, the .json beside it. Until then, and for a
// file without them, they are its name's, "Artist - Title" as the naming
// template writes it by default.
func (l *libraryScreen) tags(f libraryFile) (title, artist string) {
	path := f.path
	if t := l.tagged.get(f.key(), func(ctx context.Context) trackTags { return l.readTags(ctx, path) }); t.title != "" {
		return t.title, t.artist
	}
	name := strings.TrimSuffix(filepath.Base(f.rel), filepath.Ext(f.rel))
	if artist, title, ok := tag.SplitArtistTitle(name); ok {
		return title, artist
	}
	return name, ""
}

func (l *libraryScreen) readTags(ctx context.Context, path string) trackTags {
	if strings.EqualFold(filepath.Ext(path), ".wav") {
		var side tag.Track
		if b, err := os.ReadFile(path + ".json"); err == nil && json.Unmarshal(b, &side) == nil {
			return trackTags{side.Title, strings.Join(side.Performers, ", ")}
		}
		return trackTags{}
	}
	ff := l.ffmpeg()
	if ff == nil {
		return trackTags{}
	}
	tags, err := ff.Tags(ctx, path)
	if err != nil {
		return trackTags{}
	}
	return trackTags{tags["title"], tags["artist"]}
}

// trackTags is a file's title and artists; none for a file without tags.
type trackTags struct{ title, artist string }

// tagCache reads each file's tags once, off the toolkit's goroutine and two
// at a time: a library holds thousands of files, and only the rows in view
// ask for theirs.
type tagCache struct {
	mu      sync.Mutex
	read    map[string]trackTags
	pending map[string]bool
	loaded  func()
	slots   chan struct{}
}

// get returns the tags read under key, or starts read and returns none until
// it is done; loaded runs then, so the screen can draw them.
func (c *tagCache) get(key string, read func(context.Context) trackTags) trackTags {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.read[key]; ok {
		return t
	}
	if !c.pending[key] {
		c.pending[key] = true
		go func() {
			c.slots <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			t := read(ctx)
			cancel()
			<-c.slots
			c.mu.Lock()
			delete(c.pending, key)
			c.read[key] = t
			c.mu.Unlock()
			if c.loaded != nil {
				c.loaded()
			}
		}()
	}
	return trackTags{}
}

func (l *libraryScreen) content() fyne.CanvasObject { return l.root }

func (l *libraryScreen) render(s model.State) {
	dir, _ := s.Settings["output.dir"].(string)
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
		if q == "" || strings.Contains(strings.ToLower(f.rel), q) {
			shown = append(shown, f)
		}
	}
	if l.sortBy.Selected == "Name" {
		sort.Slice(shown, func(i, j int) bool { return strings.ToLower(shown[i].rel) < strings.ToLower(shown[j].rel) })
	} else {
		sort.Slice(shown, func(i, j int) bool { return shown[i].modTime.After(shown[j].modTime) })
	}
	l.shown = shown
	setCaption(l.count, pluralFiles(len(shown), len(l.all), l.folder))
	l.card.Refresh()
	switch {
	case len(l.all) == 0:
		l.hint.SetText("Recorded tracks land in " + l.folder + ".")
		l.empty.Show()
	case len(shown) == 0:
		l.hint.SetText("Nothing matches \"" + l.search.Text + "\".")
		l.empty.Show()
	default:
		l.empty.Hide()
	}
	show(l.card, !l.empty.Visible())
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
