package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
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

// advancedItems are the form items folded under a group's Advanced.
func advancedItems(s *settingsScreen, group string) map[*widget.FormItem]bool {
	out := map[*widget.FormItem]bool{}
	acc := s.advanced[group]
	if acc == nil {
		return out
	}
	for _, o := range acc.Items[0].Detail.(*fyne.Container).Objects {
		if f, ok := o.(*widget.Form); ok {
			for _, it := range f.Items {
				out[it] = true
			}
		}
	}
	return out
}

// App comes first, with the player as its advanced part; every setting is
// in its section, the rarely changed ones folded under Advanced, and no
// field has a caption under it.
func TestSettingsAreLaidOutBySection(t *testing.T) {
	_, _, s := settingsApp(t)
	if g := groups(); g[0] != "App" {
		t.Errorf("first section %q, want App", g[0])
	}
	player := false
	if app := s.advanced["App"]; app != nil {
		for _, o := range app.Items[0].Detail.(*fyne.Container).Objects {
			player = player || o == s.player.form
		}
	}
	if !player {
		t.Error("the player is not in App's Advanced part")
	}
	for _, d := range settings.Table {
		f := s.fields[d.Key]
		if f == nil {
			t.Errorf("no field for %s", d.Key)
			continue
		}
		if folded := advancedItems(s, d.Group)[f.item]; folded != d.Advanced {
			t.Errorf("%s: under Advanced %v, want %v", d.Key, folded, d.Advanced)
		}
		if f.item.HintText != "" {
			t.Errorf("%s has a caption: %q", d.Key, f.item.HintText)
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
	values["recorder.min_seconds"] = 45
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
	resets := buttons(s.sections.Objects[0].(*fyne.Container), "Reset")
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
