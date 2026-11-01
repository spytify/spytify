package ui

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// settingsApp is the app on its Settings tab, with the defaults applied.
func settingsApp(t *testing.T) (*App, *model.Model, *settingsScreen) {
	t.Helper()
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.showTab(tabSettings)
	a.render()
	return a, m, a.screens[tabSettings].(*settingsScreen)
}

// holds reports whether o is in root's tree, through containers and
// widgets alike.
func holds(root, o fyne.CanvasObject) bool {
	if root == o {
		return true
	}
	switch w := root.(type) {
	case nil:
		return false
	case *fyne.Container:
		for _, c := range w.Objects {
			if holds(c, o) {
				return true
			}
		}
		return false
	case fyne.Widget:
		for _, c := range test.WidgetRenderer(w).Objects() {
			if holds(c, o) {
				return true
			}
		}
	}
	return false
}

// folded reports whether o is under a group's Advanced.
func folded(s *settingsScreen, group string, o fyne.CanvasObject) bool {
	acc := s.advanced[group]
	return acc != nil && holds(acc.Items[0].Detail, o)
}

// App comes first, with the player as its advanced part; every setting is
// in its section, the rarely changed ones folded under Advanced.
func TestSettingsAreLaidOutBySection(t *testing.T) {
	_, _, s := settingsApp(t)
	if g := groups(); g[0] != "App" {
		t.Errorf("first section %q, want App", g[0])
	}
	if !folded(s, "App", s.player.box) {
		t.Error("the player is not in App's Advanced part")
	}
	for _, d := range settings.Table {
		f := s.fields[d.Key]
		if f == nil {
			t.Errorf("no field for %s", d.Key)
			continue
		}
		if folded(s, d.Group, f.row) != d.Advanced {
			t.Errorf("%s: under Advanced %v, want %v", d.Key, !d.Advanced, d.Advanced)
		}
	}
	for _, acc := range s.advanced {
		if acc.Items[0].Open {
			t.Error("an Advanced part is open before anyone opened it")
		}
	}
}

// Settings removed on purpose stay removed: the app is in English, the
// log level is in Logs, and tags come from the player, not web services.
func TestRemovedSettingsStayRemoved(t *testing.T) {
	a, _, _ := settingsApp(t)
	all := strings.ToLower(strings.Join(texts(a.win.Content()), " "))
	for _, gone := range []string{"language", "log level", "last.fm", "client id", "client secret"} {
		if strings.Contains(all, gone) {
			t.Errorf("Settings shows %q", gone)
		}
	}
	for _, d := range settings.Table {
		if strings.HasPrefix(d.Key, "app.language") || strings.HasPrefix(d.Key, "app.log") || strings.Contains(d.Key, "lastfm") || strings.Contains(d.Key, "api") {
			t.Errorf("setting %s is back", d.Key)
		}
	}
}

// Showing the core's values sends nothing back: a field filled by render
// must not look like the user changing it.
func TestShowingSettingsSendsNothingBack(t *testing.T) {
	a, m, core := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	core.mu.Lock()
	before := len(core.sets)
	core.mu.Unlock()
	values := defaults()
	values["output.underscores"] = true
	values["encode.format"] = "mp3"
	values["recorder.boundary_window_ms"] = 450
	m.SetSettings(values)
	a.render()
	time.Sleep(200 * time.Millisecond)
	core.mu.Lock()
	after := len(core.sets)
	core.mu.Unlock()
	if after != before {
		t.Fatalf("showing the settings sent %d settings.set", after-before)
	}
}

// FLAC and WAV are lossless: the bitrate only applies to MP3 and AAC.
func TestBitrateOnlyForLossyFormats(t *testing.T) {
	a, m, s := settingsApp(t)
	sel := s.fields["encode.bitrate_kbps"].input.(*widget.Select)
	for format, enabled := range map[string]bool{"flac": false, "wav": false, "mp3": true, "aac": true} {
		values := defaults()
		values["encode.format"] = format
		m.SetSettings(values)
		a.render()
		if sel.Disabled() == enabled {
			t.Errorf("%s: bitrate enabled %v, want %v", format, !sel.Disabled(), enabled)
		}
	}
}

// Every setting with a note shows it under its label, whole on one line at
// the window's default width; the file name template lists its
// placeholders there too.
func TestEveryNoteShowsUnderItsLabel(t *testing.T) {
	a, _, s := settingsApp(t)
	if w := a.win.Canvas().Size().Width; w != 920 {
		t.Fatalf("window %v wide, the default is 920", w)
	}
	snapshot(t, a, "settings-notes")
	for _, acc := range s.advanced {
		acc.Open(0)
	}
	relayout(s.scroll)
	drv := fyne.CurrentApp().Driver()
	for _, d := range settings.Table {
		if d.Note == "" {
			continue
		}
		row := s.fields[d.Key].row
		label, note := richText(row, d.Label), richText(row, d.Note)
		if label == nil || note == nil {
			t.Errorf("%s: no note under its label", d.Key)
			continue
		}
		at, under := drv.AbsolutePositionForObject(label), drv.AbsolutePositionForObject(note)
		if under.X != at.X || under.Y < at.Y+label.Size().Height {
			t.Errorf("%s: note at %v, not under its label at %v", d.Key, under, at)
		}
		if got := shown(note); got != d.Note {
			t.Errorf("%s: the note shows %q, cut at the default width", d.Key, got)
		}
	}
	// The whole page, every note on it, at the default width.
	size := a.win.Canvas().Size()
	a.win.Resize(fyne.NewSize(920, drv.AbsolutePositionForObject(s.scroll).Y+s.scroll.Content.MinSize().Height))
	snapshot(t, a, "settings-notes-all")
	a.win.Resize(size)
	s.search.SetText("tag")
	a.render()
	snapshot(t, a, "settings-notes-tags")
}

// richText is the rich text in o's containers that says text.
func richText(o fyne.CanvasObject, text string) *widget.RichText {
	switch w := o.(type) {
	case *widget.RichText:
		if ts, ok := w.Segments[0].(*widget.TextSegment); ok && ts.Text == text {
			return w
		}
	case *fyne.Container:
		for _, c := range w.Objects {
			if r := richText(c, text); r != nil {
				return r
			}
		}
	}
	return nil
}

// shown is the text a rich text draws, an ellipsis included when it is
// cut.
func shown(r *widget.RichText) string {
	var out []string
	for _, o := range test.WidgetRenderer(r).Objects() {
		if t, ok := o.(*canvas.Text); ok {
			out = append(out, t.Text)
		}
	}
	return strings.Join(out, "\n")
}

// Each bitrate in the list says what a minute of it takes; picking one
// sends its number, the value config.toml keeps.
func TestPickingABitrateSendsItsNumber(t *testing.T) {
	a, m, core := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	values := defaults()
	values["encode.format"] = "mp3"
	m.SetSettings(values)
	a.render()
	sel := s.fields["encode.bitrate_kbps"].input.(*widget.Select)
	if sel.Selected != "160 kbps · ~ 1.2 MB/min" {
		t.Errorf("the bitrate shows %q", sel.Selected)
	}
	// The row is below the fold: scroll it into view, then open its list.
	drv := fyne.CurrentApp().Driver()
	s.scroll.ScrollToOffset(fyne.NewPos(0, drv.AbsolutePositionForObject(sel).Y-drv.AbsolutePositionForObject(s.scroll).Y-200))
	test.Tap(sel)
	top := a.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("the bitrate list did not open")
	}
	// The overlay is Fyne's internal container: its Content is the list.
	list := reflect.ValueOf(top).Elem().FieldByName("Content").Interface().(*widget.PopUpMenu)
	snapshot(t, a, "settings-bitrate")
	var pick fyne.Tappable
	for _, item := range list.Items {
		if contains(texts(item), "256 kbps · ~ 1.9 MB/min") {
			pick = item.(fyne.Tappable)
		}
	}
	if pick == nil {
		t.Fatalf("no 256 kbps in the list: %v", texts(list.Menu))
	}
	test.Tap(pick)
	// Once the core answered, it has the number and no error shows.
	a.calls.Wait()
	core.mu.Lock()
	sets := slices.Clone(core.sets)
	core.mu.Unlock()
	if len(sets) == 0 {
		t.Fatal("picking a bitrate sent nothing")
	}
	d, _ := settings.Lookup("encode.bitrate_kbps")
	if n, err := d.Coerce(sets[0]["encode.bitrate_kbps"]); err != nil || n != int64(256) {
		t.Fatalf("picking 256 kbps sent %v", sets)
	}
	if top := a.win.Canvas().Overlays().Top(); top != nil {
		t.Errorf("picking a bitrate showed %v", texts(top))
	}
}

// A search opens the Advanced parts it finds something in; clearing it
// puts back what the user had opened.
func TestSettingsSearch(t *testing.T) {
	a, _, s := settingsApp(t)
	s.advanced["Output"].Open(0)
	s.search.SetText("bitrate")
	a.render()
	all := texts(s.sections)
	if !contains(all, "Bitrate") || contains(all, "Output folder") {
		t.Errorf("searching bitrate shows %v", all)
	}
	s.search.SetText("")
	a.render()
	if !s.advanced["Output"].Items[0].Open {
		t.Error("the Advanced part the user opened closed after a search")
	}
	if s.advanced["Encoding"] != nil && s.advanced["Encoding"].Items[0].Open {
		t.Error("an Advanced part a search opened stayed open")
	}
}

// Under the file name template, the file a sample track gets with the
// output settings as they are: underscores show once they are on.
func TestTheTemplateShowsAnExample(t *testing.T) {
	a, m, s := settingsApp(t)
	example := func() string { return s.example.Segments[0].(*widget.TextSegment).Text }
	v := defaults()
	v["encode.format"] = "flac"
	m.SetSettings(v)
	a.render()
	// By default the files go in artist and album folders.
	if got, want := example(), "e.g. "+filepath.Join("Neon Harbor", "Harbor Lights", "Neon Harbor - Café Lumière.flac"); got != want {
		t.Errorf("example %q, want %q", got, want)
	}
	v["output.underscores"] = true
	v["output.prefix"] = "counter"
	v["output.ascii_filenames"] = true
	m.SetSettings(v)
	s.search.SetText("output")
	a.render()
	if got, want := example(), "e.g. "+filepath.Join("Neon_Harbor", "Harbor_Lights", "001_Neon_Harbor_-_Cafe_Lumiere.flac"); got != want {
		t.Errorf("with underscores, the counter and no diacritics, example %q, want %q", got, want)
	}
	if all := texts(s.sections); !contains(all, "Underscores instead of spaces") {
		t.Errorf("no Underscores instead of spaces in %v", all)
	}
	snapshot(t, a, "settings-file-name")
}

// The counter's settings wake up with what uses the counter, and say the
// number the next file gets.
func TestTheCounterSettings(t *testing.T) {
	a, m, s := settingsApp(t)
	next := s.fields["output.counter_next"].input.(*applyEntry)
	digits := s.fields["output.counter_width"].input.(*applyEntry)
	if !next.Disabled() || !digits.Disabled() {
		t.Error("the counter's settings are on while nothing uses the counter")
	}
	if sel := s.fields["output.prefix"].input.(*widget.Select); sel.Selected != "None" || !contains(sel.Options, "Counter (001)") {
		t.Errorf("the prefix shows %q of %v", sel.Selected, sel.Options)
	}
	// The next number is the setting itself, which the recorder moves on.
	v := defaults()
	v["output.prefix"] = "counter"
	v["output.counter_next"] = int64(5)
	m.SetSettings(v)
	s.search.SetText("counter")
	a.render()
	if next.Disabled() || digits.Disabled() || next.Text != "5" {
		t.Errorf("with the counter prefix: next %q disabled %v, digits disabled %v", next.Text, next.Disabled(), digits.Disabled())
	}
	// The prefix is found by its choices' words too.
	if all := texts(s.sections); !contains(all, "File name prefix") || !contains(all, "Next counter number") {
		t.Errorf("searching counter shows %v", all)
	}
	snapshot(t, a, "settings-counter")
}

// A player change saves at once, as the other settings do; only then does
// the card offer to restart spytifyd, which reads it when it starts.
func TestThePlayerSavesWithoutAButton(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, k := range target.TargetKeys {
		t.Setenv(k, "")
	}
	a, _, s := settingsApp(t)
	s.advanced["App"].Open(0)
	s.layout()
	if _, err := os.Stat(target.UserEnvPath()); err == nil {
		t.Fatal("showing the player wrote the .env file")
	}
	if all := texts(a.win.Content()); !contains(all, "Player process") || contains(all, "Restart spytifyd") {
		t.Errorf("before any change the player shows %v", all)
	}
	s.player.process.SetText("otherplayer")
	s.player.process.submit()
	if env, err := os.ReadFile(target.UserEnvPath()); err != nil || !strings.Contains(string(env), "otherplayer") {
		t.Errorf(".env after the change: %q, %v", env, err)
	}
	all := texts(a.win.Content())
	if !contains(all, "Restart spytifyd") || contains(all, "Save and restart spytifyd") {
		t.Errorf("after the change the player shows %v", all)
	}
	snapshot(t, a, "settings-player")
}

// A section's Reset resets each of its settings, and only those.
func TestResetResetsItsSection(t *testing.T) {
	a, _, core := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	var want []string
	for _, d := range settings.Table {
		if d.Group == groups()[0] {
			want = append(want, d.Key)
		}
	}
	resets := buttons(s.sections.Objects[0], "Reset")
	if len(resets) != 1 {
		t.Fatalf("%d Reset buttons in the first section", len(resets))
	}
	test.Tap(resets[0])
	deadline := time.Now().Add(5 * time.Second)
	for {
		core.mu.Lock()
		n := 0
		for _, c := range core.calls {
			if c == proto.MethodSettingsReset {
				n++
			}
		}
		core.mu.Unlock()
		if n == len(want) {
			break
		}
		if n > len(want) || time.Now().After(deadline) {
			t.Fatalf("%d settings.reset calls, want %d (%v)", n, len(want), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Minimize to tray is off unless chosen: closing the window quits.
func TestMinimizeToTrayIsOffByDefault(t *testing.T) {
	for _, d := range settings.Table {
		if d.Key == "app.minimize_to_tray" && d.Default != false {
			t.Fatalf("app.minimize_to_tray defaults to %v", d.Default)
		}
	}
}

func TestYesNoSettingsAreSwitches(t *testing.T) {
	a, m, s := settingsApp(t)
	for _, d := range settings.Table {
		if _, ok := s.fields[d.Key].input.(*toggle); ok != (d.Kind == settings.KindBool) {
			t.Errorf("%s: switch %v for kind %v", d.Key, ok, d.Kind)
		}
	}
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "settings-light")

	var got []bool
	sw := newToggle(func(on bool) { got = append(got, on) })
	test.NewTempWindow(t, sw)
	sw.SetOn(true)
	if !sw.on || len(got) != 0 {
		t.Fatalf("SetOn: on %v, reported %v", sw.on, got)
	}
	test.Tap(sw)
	sw.TypedRune(' ')
	sw.TypedRune('x')
	if !sw.on || len(got) != 2 || got[0] || !got[1] {
		t.Fatalf("tap then space: on %v, reported %v", sw.on, got)
	}
	// The switch fills its form row; a tap beside it is not on it.
	sw.Resize(fyne.NewSize(400, sw.MinSize().Height))
	test.TapAt(sw, fyne.NewPos(300, 5))
	if len(got) != 2 {
		t.Fatalf("a tap beside the switch flipped it: %v", got)
	}
}

// The folder picker opens at the output folder or, when it is gone, at the
// nearest folder above it that exists: for a USB key that is not plugged
// in, the folder the drives are in.
func TestTheFolderPickerStartsAtTheOutputFolder(t *testing.T) {
	root := t.TempDir()
	volumes := filepath.Join(root, "Volumes")
	music := filepath.Join(root, "Music", "Spytify")
	for _, d := range []string{volumes, music} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range map[string]string{
		music:                           music,
		filepath.Join(music, "not yet"): music,
		filepath.Join(volumes, "KINGSTON", "Music", "Spytify"): volumes,
	} {
		got := startFolder(path)
		if got == nil {
			t.Errorf("%s: no start folder", path)
			continue
		}
		if p, _ := filepath.EvalSymlinks(got.Path()); p != mustEval(t, want) {
			t.Errorf("%s: starts at %s, want %s", path, got.Path(), want)
		}
	}
	if got := startFolder(""); got != nil {
		t.Errorf("no output folder: starts at %s", got.Path())
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The picker is a third larger than Fyne's, centred in the window, and
// shows the output folder.
func TestTheFolderPickerIsLargerAndCentred(t *testing.T) {
	a, m, s := settingsApp(t)
	out := filepath.Join(t.TempDir(), "Spytify Output")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	v := defaults()
	v["output.dir"] = out
	m.SetSettings(v)
	a.render()
	var browse *widget.Button
	for _, o := range s.fields["output.dir"].input.(*fyne.Container).Objects {
		if b, ok := o.(*widget.Button); ok {
			browse = b
		}
	}
	if browse == nil {
		t.Fatal("no browse button next to the output folder")
	}
	test.Tap(browse)
	top := a.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("the picker did not open")
	}
	// The overlay is Fyne's internal container: its Content is the dialog.
	dlg := reflect.ValueOf(top).Elem().FieldByName("Content").Interface().(fyne.CanvasObject)
	win := a.win.Canvas().Size()
	if dlg.Size() != pickerSize {
		t.Errorf("picker %v, want %v", dlg.Size(), pickerSize)
	}
	want := fyne.NewPos((win.Width-pickerSize.Width)/2, (win.Height-pickerSize.Height)/2)
	if p := dlg.Position(); math.Abs(float64(p.X-want.X)) > 1 || math.Abs(float64(p.Y-want.Y)) > 1 {
		t.Errorf("picker at %v, centred is %v", p, want)
	}
	if !contains(texts(top), "Spytify Output") {
		t.Errorf("the picker does not show the output folder: %v", texts(top))
	}
	snapshot(t, a, "folder-picker")
}
