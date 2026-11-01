package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
)

// libraryOf writes files, empty but for the .json ones, under a new output
// folder, the first of order newest, and shows them in the Library. Their
// tags wait for release: no ffmpeg, a file's but a WAV's are none.
func libraryOf(t *testing.T, files map[string]string, order ...string) (a *App, lib *libraryScreen, dir string, release func()) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("SPYTIFY_FFMPEG", filepath.Join(dir, "none"))
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range order {
		at := time.Now().Add(-time.Duration(i) * time.Minute)
		if err := os.Chtimes(filepath.Join(dir, name), at, at); err != nil {
			t.Fatal(err)
		}
	}
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	lib = a.screens[tabLibrary].(*libraryScreen)
	release = holdTags(t, lib)
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.showTab(tabLibrary)
	a.render()
	return a, lib, dir, release
}

// rowNames is the list's rows: an album's heading and note, or a file's
// name.
func rowNames(l *libraryScreen) []string {
	var out []string
	for _, r := range l.rows {
		if r.heading != "" {
			out = append(out, "# "+r.heading+" | "+r.note)
		} else {
			out = append(out, filepath.Base(r.file.rel))
		}
	}
	return out
}

// tick ticks the filter's boxes for kinds, and only those.
func tick(l *libraryScreen, kinds ...string) {
	for _, k := range takeKinds {
		l.takes[k].SetChecked(slices.Contains(kinds, k))
	}
}

// holdTags keeps the tags from being read until the test ends or calls the
// function it returns.
func holdTags(t *testing.T, l *libraryScreen) func() {
	t.Helper()
	for range cap(l.tagged.slots) {
		l.tagged.slots <- struct{}{}
	}
	held := true
	release := func() {
		if held {
			held = false
			for range cap(l.tagged.slots) {
				<-l.tagged.slots
			}
		}
	}
	t.Cleanup(release)
	return release
}

// Keep anyway names a take "<title> (partial)" or "<title> (low quality)",
// with underscores for spaces when the setting says so, and a number after
// it when the name was taken; every other file is a complete take.
func TestTakesAreToldApartByTheirName(t *testing.T) {
	for name, want := range map[string]string{
		"M83 - Wait (partial).flac":                     takePartial,
		"M83 - Reunion (low quality).mp3":               takeLowQuality,
		"M83_-_Reunion_(low_quality).mp3":               takeLowQuality,
		"M83 - Wait (partial) 2.flac":                   takePartial,
		"004 M83 - Wait (Partial).m4a":                  takePartial,
		"Wait (partial) - M83.wav":                      takePartial,
		"M83/Hurry Up/M83 - Midnight City.flac":         takeComplete,
		"Partial/M83 - Midnight City.flac":              takeComplete,
		"M83 - Partial Eclipse.mp3":                     takeComplete,
		"M83 - Low Quality Dreams (Extended Mix).flac":  takeComplete,
		"M83 - Wait (partial)/M83 - Midnight City.flac": takeComplete,
	} {
		if got := takeKind(name); got != want {
			t.Errorf("takeKind(%q) = %q, want %q", name, got, want)
		}
	}
}

// The filter hides the partial takes, or shows them alone to clean them up,
// a box per kind of file; the count says how many files it leaves out. A
// take's row says its kind in a chip, out of its title.
func TestTheFilterHidesOrShowsThePartialTakes(t *testing.T) {
	order := []string{
		"M83 - Midnight City.flac",
		"M83 - Wait (partial).flac",
		"M83 - Reunion (low quality).mp3",
		"M83 - Outro.flac",
		"M83 - Wait (partial) 2.flac",
	}
	files := map[string]string{}
	for _, name := range order {
		files[name] = ""
	}
	a, lib, _, _ := libraryOf(t, files, order...)
	for _, c := range []struct {
		ticked []string
		label  string
		want   []string
		count  string
	}{
		{[]string{takeComplete, takePartial, takeLowQuality}, "All files", order, "5 files in"},
		{[]string{takeComplete}, "Complete", []string{"M83 - Midnight City.flac", "M83 - Outro.flac"}, "2 of 5 files in"},
		{[]string{takePartial, takeLowQuality}, "Partial, Low quality", []string{"M83 - Wait (partial).flac", "M83 - Reunion (low quality).mp3", "M83 - Wait (partial) 2.flac"}, "3 of 5 files in"},
		{[]string{takeLowQuality}, "Low quality", []string{"M83 - Reunion (low quality).mp3"}, "1 of 5 files in"},
	} {
		tick(lib, c.ticked...)
		if got := lib.kinds.Selected; got != c.label {
			t.Errorf("%q: the filter says %q, want %q", c.ticked, got, c.label)
		}
		if got := rowNames(lib); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, want %q", c.ticked, got, c.want)
		}
		if got := lib.count.Segments[0].(*widget.TextSegment).Text; !strings.HasPrefix(got, c.count) {
			t.Errorf("%q: the count says %q", c.ticked, got)
		}
	}
	// The filter and the search go together.
	tick(lib, takePartial, takeLowQuality)
	lib.search.SetText("wait")
	if got := rowNames(lib); !slices.Equal(got, []string{"M83 - Wait (partial).flac", "M83 - Wait (partial) 2.flac"}) {
		t.Errorf("the partial takes matching wait: %q", got)
	}
	lib.search.SetText("")
	a.render()
	shown := texts(lib.list)
	for _, want := range []string{"Wait", "Wait 2", "Reunion", takePartial, takeLowQuality} {
		if !slices.Contains(shown, want) {
			t.Errorf("the rows show %q, without %q", shown, want)
		}
	}
	if contains(shown, "(partial)") || contains(shown, "(low quality)") {
		t.Errorf("a title keeps its marker: %q", shown)
	}
	setCaption(lib.count, pluralFiles(3, 5, "/Users/listener/Music/Spytify"))
	snapshot(t, a, "library-partial-takes")

	// Its boxes, in a dropdown that stays open while they are ticked.
	tick(lib, takeComplete, takePartial, takeLowQuality)
	test.Tap(lib.kinds)
	top := a.win.Canvas().Overlays().Top()
	if top == nil || !contains(texts(top), takeLowQuality) {
		t.Fatal("the filter shows no boxes")
	}
	test.Tap(lib.takes[takeComplete])
	if a.win.Canvas().Overlays().Top() != top {
		t.Error("the dropdown closed on the first box")
	}
	if got := lib.kinds.Selected; got != "Partial, Low quality" || len(lib.rows) != 3 {
		t.Errorf("Complete unticked in the dropdown: the filter says %q, %d rows", got, len(lib.rows))
	}
	setCaption(lib.count, pluralFiles(3, 5, "/Users/listener/Music/Spytify"))
	snapshot(t, a, "library-filter")
	a.win.Canvas().Overlays().Remove(top)

	tick(lib)
	if !lib.empty.Visible() || lib.hint.Text != "Tick a kind of file to show." || lib.kinds.Selected != "No files" {
		t.Errorf("no box ticked: empty state shown %v, %q, the filter says %q", lib.empty.Visible(), lib.hint.Text, lib.kinds.Selected)
	}
	// Every file a partial take, the list hiding them says so.
	for _, name := range []string{"M83 - Midnight City.flac", "M83 - Outro.flac"} {
		if err := os.Remove(filepath.Join(lib.folder, name)); err != nil {
			t.Fatal(err)
		}
	}
	tick(lib, takeComplete)
	lib.scan()
	if !lib.empty.Visible() || lib.hint.Text != "Every file here is a take cut short or in low quality." {
		t.Errorf("no file left: empty state shown %v, %q", lib.empty.Visible(), lib.hint.Text)
	}
}

// The filter's dropdown takes the keys a select's list does: Space or an
// arrow opens it on its first box, the arrows go from box to box, Space
// ticks the box focused, Enter and Escape close it. Tab closes it and goes
// on: it went round the boxes, and the keyboard never got out.
func TestTheFilterWorksFromTheKeyboard(t *testing.T) {
	order := []string{"M83 - Midnight City.flac", "M83 - Wait (partial).flac"}
	files := map[string]string{}
	for _, name := range order {
		files[name] = ""
	}
	a, lib, _, _ := libraryOf(t, files, order...)
	c := a.win.Canvas()
	// The keys as Fyne 2.8.2's window sends them: Tab goes to a control
	// that takes it, else moves the focus; Space is the key, then its
	// character, to whatever has the focus by then.
	key := func(k fyne.KeyName) {
		if f, ok := c.Focused().(fyne.Tabbable); k == fyne.KeyTab && (!ok || !f.AcceptsTab()) {
			c.FocusNext()
			return
		}
		if f := c.Focused(); f != nil {
			f.TypedKey(&fyne.KeyEvent{Name: k})
		}
	}
	space := func() {
		key(fyne.KeySpace)
		if f := c.Focused(); f != nil {
			f.TypedRune(' ')
		}
	}
	box := func() string {
		if b, ok := c.Focused().(*menuBox); ok {
			return b.Text
		}
		return fmt.Sprintf("%T", c.Focused())
	}

	c.Focus(lib.kinds)
	space()
	if c.Overlays().Top() == nil || box() != takeComplete || !lib.takes[takeComplete].Checked {
		t.Fatalf("Space: dropdown shown %v, the focus on %s, Complete ticked %v", c.Overlays().Top() != nil, box(), lib.takes[takeComplete].Checked)
	}
	for _, step := range []struct {
		key  fyne.KeyName
		want string
	}{
		{fyne.KeyDown, takePartial},
		{fyne.KeyDown, takeLowQuality},
		{fyne.KeyDown, takeLowQuality},
		{fyne.KeyUp, takePartial},
	} {
		key(step.key)
		if box() != step.want {
			t.Fatalf("%s: the focus is on %s, want %s", step.key, box(), step.want)
		}
	}
	space()
	if lib.takes[takePartial].Checked || lib.kinds.Selected != takeComplete+", "+takeLowQuality || len(lib.rows) != 1 {
		t.Errorf("Space on Partial: ticked %v, the filter says %q, %d rows", lib.takes[takePartial].Checked, lib.kinds.Selected, len(lib.rows))
	}

	key(fyne.KeyTab)
	if c.Overlays().Top() != nil {
		t.Fatalf("Tab left the dropdown open, the focus on %s", box())
	}
	if b, ok := asButton(c.Focused().(fyne.CanvasObject)); !ok || b.Icon != theme.ViewRefreshIcon() {
		t.Errorf("Tab went to %s, want the rescan after the filter", box())
	}

	for _, k := range []fyne.KeyName{fyne.KeyEscape, fyne.KeyReturn, fyne.KeyEnter} {
		c.Focus(lib.kinds)
		key(fyne.KeyDown)
		if box() != takeComplete {
			t.Fatalf("Down on the filter: the focus is on %s", box())
		}
		key(k)
		if c.Overlays().Top() != nil || c.Focused() != lib.kinds {
			t.Errorf("%s: dropdown shown %v, the focus on %s", k, c.Overlays().Top() != nil, box())
		}
	}
}

// The Album view puts the files under their albums: the album folders the
// recorder files them in, under the artists' folders, at once; a file in no
// such folder, under the album its tags name once its row has read them.
// The files with no album go last, under a heading that says so.
func TestTheAlbumViewGroupsTheFilesByAlbum(t *testing.T) {
	a, lib, _, release := libraryOf(t, map[string]string{
		"M83/Hurry Up, We're Dreaming (2011)/M83 - Midnight City.flac": "",
		"M83/Hurry Up, We're Dreaming (2011)/M83 - Wait.flac":          "",
		"M83/Saturdays = Youth (2008)/M83 - Kim & Jessie.flac":         "",
		"Air/Moon Safari (1998)/Air - La femme d'argent.flac":          "",
		// The folders of albums with no name.
		"Air/Untitled/Air - Demo.flac":               "",
		"Phoenix/Untitled (2000)/Phoenix - Demo.mp3": "",
		// An item with no title, at the top of the folder.
		"Untitled 20261031101500.flac": "",
		// A file from a flat folder, whose tags name its album.
		"Daft Punk - One More Time.wav":      "",
		"Daft Punk - One More Time.wav.json": `{"title": "One More Time", "performers": ["Daft Punk"], "album": "Discovery", "album_artists": ["Daft Punk"]}`,
	})
	lib.sortBy.SetSelected(byAlbum)
	want := []string{
		"# Hurry Up, We're Dreaming (2011) | M83 · 2 files", "M83 - Midnight City.flac", "M83 - Wait.flac",
		"# Moon Safari (1998) | Air · 1 file", "Air - La femme d'argent.flac",
		"# Saturdays = Youth (2008) | M83 · 1 file", "M83 - Kim & Jessie.flac",
		"# No album | 4 files", "Air - Demo.flac", "Daft Punk - One More Time.wav", "Phoenix - Demo.mp3", "Untitled 20261031101500.flac",
	}
	if got := rowNames(lib); !slices.Equal(got, want) {
		t.Fatalf("before the tags:\n%q\nwant\n%q", got, want)
	}
	if len(lib.shown) != 8 || len(lib.unread) != 2 {
		t.Errorf("%d files shown, %d with their album unread", len(lib.shown), len(lib.unread))
	}

	// Scrolled to the end, the rows in view read their tags.
	lib.list.ScrollToBottom()
	release()
	eventually(t, "the tags are never read", func() bool {
		for _, f := range lib.shown {
			if _, read := lib.tagged.known(f.key()); filepath.Dir(f.rel) == "." && !read {
				return false
			}
		}
		return true
	})
	lib.loaded()
	want = append([]string{"# Discovery | Daft Punk · 1 file", "Daft Punk - One More Time.wav"}, want[:len(want)-5]...)
	want = append(want, "# No album | 3 files", "Air - Demo.flac", "Phoenix - Demo.mp3", "Untitled 20261031101500.flac")
	if got := rowNames(lib); !slices.Equal(got, want) {
		t.Fatalf("after the tags:\n%q\nwant\n%q", got, want)
	}
	if len(lib.unread) != 0 {
		t.Errorf("%d files with their album unread", len(lib.unread))
	}

	// Headings and files drawn as such.
	lib.list.ScrollToTop()
	a.render()
	drawn := drawnRows(t, lib.list)
	if _, ok := drawn["Discovery"]; ok {
		t.Error("a heading is drawn as a file's row")
	}
	if !slices.Equal(words(drawn["One More Time"].lines), []string{"One More Time", "Daft Punk"}) {
		t.Errorf("the file under its heading: %q", words(drawn["One More Time"].lines))
	}
	if all := texts(lib.list); !contains(all, "Discovery") || !contains(all, "Daft Punk · 1 file") {
		t.Errorf("no heading drawn: %q", all)
	}
	setCaption(lib.count, pluralFiles(8, 8, "/Users/listener/Music/Spytify"))
	snapshot(t, a, "library-albums")
}

// The Album view groups thousands of files at once, from their folders,
// without a tag read: ffmpeg takes tens of milliseconds a file, which for a
// library this size is minutes. It still draws the rows in view alone.
func TestTheAlbumViewGroupsThousandsOfFilesWithoutTheirTags(t *testing.T) {
	files := map[string]string{}
	for a := range 250 {
		for n := range 20 {
			files[fmt.Sprintf("Artist %03d/Album %03d (2001)/Artist %03d - Title %02d.mp3", a, a, a, n)] = ""
		}
	}
	for n := range 100 {
		files[fmt.Sprintf("Untitled 20261031%06d.mp3", n)] = ""
	}
	_, lib, _, _ := libraryOf(t, files)
	start := time.Now()
	lib.sortBy.SetSelected(byAlbum)
	took := time.Since(start)
	t.Logf("5,100 files in 251 groups in %v", took)
	if took > 2*time.Second {
		t.Errorf("grouping took %v", took)
	}
	headings := 0
	for _, r := range lib.rows {
		if r.heading != "" {
			headings++
		}
	}
	if len(lib.shown) != 5100 || headings != 251 || lib.rows[len(lib.rows)-101].heading != noAlbum {
		t.Fatalf("%d files under %d headings", len(lib.shown), headings)
	}
	lib.tagged.mu.Lock()
	read := len(lib.tagged.read)
	lib.tagged.mu.Unlock()
	if read != 0 || len(lib.unread) != 100 {
		t.Errorf("%d tags read to group the files, %d files waiting for theirs", read, len(lib.unread))
	}
	// A cover or the tags of a file in an album folder group nothing again;
	// the tags of a file in none do.
	rows := &lib.rows[0]
	lib.loaded()
	if &lib.rows[0] != rows {
		t.Error("the files were grouped again for nothing")
	}
	lib.tagged.mu.Lock()
	lib.tagged.read[lib.unread[0]] = trackTags{album: "Live"}
	lib.tagged.mu.Unlock()
	lib.loaded()
	if &lib.rows[0] == rows || lib.rows[0].heading != "Album 000 (2001)" || !slices.ContainsFunc(lib.rows, func(r libraryRow) bool { return r.heading == "Live" }) {
		t.Error("the files were not grouped again once a file's tags named its album")
	}
	drawn := map[widget.ListItemID]bool{}
	update := lib.list.UpdateItem
	lib.list.UpdateItem = func(id widget.ListItemID, o fyne.CanvasObject) {
		drawn[id] = true
		update(id, o)
	}
	lib.list.ScrollTo(3000)
	if len(drawn) == 0 || len(drawn) > 12 || !drawn[3000] {
		t.Errorf("scrolled to row 3,000, drew %d rows", len(drawn))
	}
}

// The trash button moves the file to the trash, a WAV with the .json beside
// it, without a question; the row goes. A trash that refuses says why, and
// the file keeps its row.
func TestTheTrashButtonMovesAFileToTheTrash(t *testing.T) {
	order := []string{
		"M83 - Splendor (low quality).wav",
		"M83 - Wait (partial).flac",
		"M83 - Midnight City.flac",
	}
	a, lib, dir, _ := libraryOf(t, map[string]string{
		order[0]:           "",
		order[0] + ".json": `{"title": "Splendor (low quality)", "performers": ["M83"]}`,
		order[1]:           "",
		order[2]:           "",
	}, order...)
	bin := t.TempDir()
	var moved []string
	var refuse error
	real := moveToTrash
	t.Cleanup(func() { moveToTrash = real })
	moveToTrash = func(path string) error {
		if _, err := os.Lstat(path); err != nil {
			return err
		}
		if refuse != nil {
			return refuse
		}
		moved = append(moved, path)
		return os.Rename(path, filepath.Join(bin, filepath.Base(path)))
	}
	trashButton := func(title string) *button {
		t.Helper()
		a.render()
		row, ok := drawnRows(t, lib.list)[title]
		if !ok {
			t.Fatalf("no row for %s", title)
		}
		return row.row.Objects[2].(*fyne.Container).Objects[3].(*button)
	}

	first := trashButton("Splendor")
	test.MoveMouse(a.win.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(first).AddXY(8, 8))
	setCaption(lib.count, pluralFiles(3, 3, "/Users/listener/Music/Spytify"))
	snapshot(t, a, "library-trash")
	test.Tap(first)
	wav := filepath.Join(dir, order[0])
	if !slices.Equal(moved, []string{wav, wav + ".json"}) {
		t.Fatalf("moved %q to the trash", moved)
	}
	if top := a.win.Canvas().Overlays().Top(); top != nil {
		t.Fatalf("a question or an error: %q", texts(top))
	}
	if got := rowNames(lib); !slices.Equal(got, order[1:]) {
		t.Fatalf("rows after the trash: %q", got)
	}

	refuse = &os.PathError{Op: "trash", Path: filepath.Join(dir, order[2]), Err: errors.New("its drive has no trash folder Spytify can use")}
	test.Tap(trashButton("Midnight City"))
	top := a.win.Canvas().Overlays().Top()
	if top == nil || !contains(texts(top), "no trash folder") {
		t.Fatal("a refused trash says nothing")
	}
	a.win.Canvas().Overlays().Remove(top)
	if got := rowNames(lib); !slices.Equal(got, order[1:]) {
		t.Fatalf("rows after a refused trash: %q", got)
	}

	// Gone already, from another program, the file leaves its row quietly.
	refuse = nil
	if err := os.Remove(filepath.Join(dir, order[1])); err != nil {
		t.Fatal(err)
	}
	test.Tap(trashButton("Wait"))
	if top := a.win.Canvas().Overlays().Top(); top != nil {
		t.Fatalf("a file gone already: %q", texts(top))
	}
	if got := rowNames(lib); !slices.Equal(got, order[2:]) {
		t.Fatalf("rows after the trash of a file gone: %q", got)
	}
}
