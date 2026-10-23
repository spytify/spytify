package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

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
	count  *widget.RichText
	empty  fyne.CanvasObject
	hint   *widget.Label

	folder string
	all    []libraryFile
	shown  []libraryFile
}

func newLibraryScreen(a *App) *libraryScreen {
	l := &libraryScreen{app: a}
	l.search = widget.NewEntry()
	l.search.SetPlaceHolder("Search artist, title or album")
	l.search.OnChanged = func(string) { l.filter() }
	l.sortBy = widget.NewSelect([]string{"Newest", "Name"}, func(string) { l.filter() })
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { l.scan() })
	l.count = caption("")
	l.hint = widget.NewLabel("")
	l.empty = emptyState("No recordings yet", l.hint)
	l.list = widget.NewList(
		func() int { return len(l.shown) },
		func() fyne.CanvasObject {
			name := widget.NewLabelWithStyle("Name", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			name.Truncation = fyne.TextTruncateEllipsis
			sub := clipped("Folder")
			return container.NewBorder(nil, nil, nil, container.NewHBox(
				&widget.Button{Icon: theme.SearchIcon(), Importance: widget.LowImportance},
				&widget.Button{Icon: theme.MediaPlayIcon(), Importance: widget.LowImportance}),
				container.NewVBox(name, sub))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(l.shown) {
				return
			}
			f := l.shown[id]
			box := o.(*fyne.Container)
			text := box.Objects[0].(*fyne.Container)
			buttons := box.Objects[1].(*fyne.Container)
			text.Objects[0].(*widget.Label).SetText(strings.TrimSuffix(filepath.Base(f.rel), filepath.Ext(f.rel)))
			sub := filepath.Dir(f.rel)
			if sub == "." {
				sub = ""
			} else {
				sub += " · "
			}
			setCaption(text.Objects[1].(*widget.RichText), sub+strings.ToUpper(strings.TrimPrefix(filepath.Ext(f.rel), "."))+" · "+fmtBytes(f.size)+" · "+f.modTime.Format("2006-01-02 15:04"))
			path := f.path
			buttons.Objects[0].(*widget.Button).OnTapped = func() { _ = reveal(path) }
			buttons.Objects[1].(*widget.Button).OnTapped = func() { _ = openPath(path) }
			box.Refresh()
		},
	)
	// SetSelected runs the callback, which needs the list and the count.
	l.sortBy.SetSelected("Newest")
	top := container.NewBorder(nil, nil, nil, container.NewHBox(l.sortBy, refresh), l.search)
	l.root = container.NewBorder(top, container.NewPadded(l.count), nil, nil, container.NewStack(l.list, l.empty))
	return l
}

func (l *libraryScreen) content() fyne.CanvasObject { return l.root }

func (l *libraryScreen) render(s model.State) {
	dir, _ := s.Settings["output.dir"].(string)
	dir = expandHome(dir)
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
	l.list.Refresh()
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
