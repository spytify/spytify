package ui

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// settingsScreen is one scrolling page of cards, one per group of the
// validation table, each field captioned with its TOML key so the UI and
// spytify-cli speak the same names.
type settingsScreen struct {
	app    *App
	root   fyne.CanvasObject
	search *widget.Entry
	cards  *fyne.Container

	fields    map[string]*field
	rendering bool
	player    *playerCard
}

type field struct {
	def    *settings.Def
	set    func(v any)
	row    fyne.CanvasObject
	parent string
}

func newSettingsScreen(a *App) *settingsScreen {
	s := &settingsScreen{app: a, fields: map[string]*field{}}
	s.search = widget.NewEntry()
	s.search.SetPlaceHolder("Search settings by name or key")
	s.search.OnChanged = func(string) { s.layout() }
	s.cards = container.NewVBox()
	s.player = newPlayerCard(a)
	for i := range settings.Table {
		d := &settings.Table[i]
		s.fields[d.Key] = s.newField(d)
	}
	s.layout()
	s.root = container.NewBorder(s.search, nil, nil, nil, container.NewVScroll(s.cards))
	return s
}

func (s *settingsScreen) content() fyne.CanvasObject { return s.root }

// groups in table order.
func groups() []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range settings.Table {
		if !seen[d.Group] {
			seen[d.Group] = true
			out = append(out, d.Group)
		}
	}
	return out
}

func (s *settingsScreen) layout() {
	q := strings.ToLower(strings.TrimSpace(s.search.Text))
	objs := []fyne.CanvasObject{}
	if q == "" || strings.Contains("player process bundle mpris target", q) {
		objs = append(objs, s.player.card)
	}
	for _, g := range groups() {
		var rows []fyne.CanvasObject
		var keys []string
		for _, d := range settings.Table {
			if d.Group != g {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(d.Label), q) && !strings.Contains(d.Key, q) {
				continue
			}
			rows = append(rows, s.fields[d.Key].row)
			keys = append(keys, d.Key)
		}
		if len(rows) == 0 {
			continue
		}
		reset := widget.NewButton("Reset group", func() { s.resetKeys(keys) })
		reset.Importance = widget.LowImportance
		if g == "Tags" {
			rows = append(rows, s.spotifyRow())
		}
		objs = append(objs, widget.NewCard(g, "", container.NewVBox(append(rows, container.NewHBox(reset))...)))
	}
	s.cards.Objects = objs
	s.cards.Refresh()
}

func (s *settingsScreen) spotifyRow() fyne.CanvasObject {
	connect := widget.NewButton("Connect to the Spotify Web API", func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var res proto.SpotifyConnectResult
			if err := s.app.link.Do(ctx, proto.MethodSpotifyConnect, nil, &res); err != nil {
				fyne.Do(func() { s.app.showError(err) })
				return
			}
			fyne.Do(func() { _ = openPath(res.AuthURL) })
		}()
	})
	return container.NewVBox(connect, muted("Needed for tags.source = spotify_api only. The API can revoke keys used to tag recordings; that risk is yours."))
}

func (s *settingsScreen) resetKeys(keys []string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, k := range keys {
			if err := s.app.link.Do(ctx, proto.MethodSettingsReset, proto.SettingsResetParams{Prefix: k}, nil); err != nil {
				fyne.Do(func() { s.app.showError(err) })
				return
			}
		}
	}()
}

// apply sends one value; a rejection is shown and the field goes back to
// the value the core has.
func (s *settingsScreen) apply(key string, v any) {
	if s.rendering {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var res proto.SettingsSetResult
		err := s.app.link.Do(ctx, proto.MethodSettingsSet, proto.SettingsSetParams{Values: map[string]any{key: v}}, &res)
		if err == nil && res.Rejected[key] != "" {
			err = fmt.Errorf("%s: %s", key, res.Rejected[key])
		}
		if err != nil {
			fyne.Do(func() {
				s.app.showError(err)
				s.render(s.app.m.Snapshot())
			})
		}
	}()
}

func (s *settingsScreen) newField(d *settings.Def) *field {
	f := &field{def: d}
	key := d.Key
	label := widget.NewLabel(d.Label)
	caption := d.Key
	if d.Note != "" {
		caption += " · " + d.Note
	}
	var input fyne.CanvasObject
	switch d.Kind {
	case settings.KindBool:
		c := widget.NewCheck("", func(b bool) { s.apply(key, b) })
		f.set = func(v any) { b, _ := v.(bool); c.SetChecked(b) }
		input = c
	case settings.KindEnum, settings.KindIntEnum:
		opts := d.Enum
		if d.Kind == settings.KindIntEnum {
			opts = nil
			for _, n := range d.Ints {
				opts = append(opts, strconv.FormatInt(n, 10))
			}
		}
		sel := widget.NewSelect(opts, func(v string) { s.apply(key, v) })
		f.set = func(v any) { sel.SetSelected(fmt.Sprint(number(v))) }
		input = sel
	case settings.KindSecret:
		e := widget.NewPasswordEntry()
		e.SetPlaceHolder("not set")
		save := widget.NewButton("Save", func() { s.apply(key, e.Text); e.SetText("") })
		clear := widget.NewButton("Clear", func() { s.apply(key, "") })
		f.set = func(v any) {
			if v == settings.Masked {
				e.SetPlaceHolder("stored in the OS keyring")
			} else {
				e.SetPlaceHolder("not set")
			}
		}
		input = container.NewBorder(nil, nil, nil, container.NewHBox(save, clear), e)
	case settings.KindPath:
		e := widget.NewEntry()
		e.OnSubmitted = func(v string) { s.apply(key, v) }
		browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
				if err == nil && u != nil {
					e.SetText(u.Path())
					s.apply(key, u.Path())
				}
			}, s.app.win)
		})
		f.set = func(v any) {
			if str, _ := v.(string); !s.focused(e) {
				e.SetText(str)
			}
		}
		input = container.NewBorder(nil, nil, nil, browse, e)
	default:
		e := widget.NewEntry()
		e.OnSubmitted = func(v string) { s.apply(key, v) }
		f.set = func(v any) {
			if !s.focused(e) {
				e.SetText(fmt.Sprint(number(v)))
			}
		}
		input = e
	}
	f.row = container.NewVBox(container.NewGridWithColumns(2, label, input), muted(caption))
	return f
}

// number prints JSON numbers without a decimal point when they are whole.
func number(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}

func (s *settingsScreen) render(st model.State) {
	s.rendering = true
	defer func() { s.rendering = false }()
	for key, f := range s.fields {
		if v, ok := st.Settings[key]; ok && f.set != nil {
			f.set(v)
		}
	}
	s.player.render(st)
}

// playerCard edits the SPYTIFY_TARGET_* variables in the user's .env file
// and restarts the daemon, which reads them only when it starts.
type playerCard struct {
	app     *App
	card    *widget.Card
	process *widget.Entry
	bundle  *widget.Entry
	mpris   *widget.Entry
	launch  *widget.Entry
	filled  bool
}

func newPlayerCard(a *App) *playerCard {
	p := &playerCard{app: a, process: widget.NewEntry(), bundle: widget.NewEntry(), mpris: widget.NewEntry(), launch: widget.NewEntry()}
	p.process.SetPlaceHolder("process name, or a comma list")
	p.bundle.SetPlaceHolder("bundle id")
	p.mpris.SetPlaceHolder("org.mpris.MediaPlayer2.<name> (default)")
	p.launch.SetPlaceHolder("command that starts the player (optional)")
	rows := []fyne.CanvasObject{
		container.NewGridWithColumns(2, widget.NewLabel("Player process"), p.process), muted("SPYTIFY_TARGET_PROCESS"),
	}
	switch runtime.GOOS {
	case "darwin":
		rows = append(rows, container.NewGridWithColumns(2, widget.NewLabel("Bundle id"), p.bundle),
			muted(`SPYTIFY_TARGET_BUNDLE_ID · osascript -e 'id of app "<Player name>"' prints it`))
	case "linux":
		rows = append(rows, container.NewGridWithColumns(2, widget.NewLabel("MPRIS name"), p.mpris), muted("SPYTIFY_TARGET_MPRIS"))
	}
	rows = append(rows, container.NewGridWithColumns(2, widget.NewLabel("Launch command"), p.launch), muted("SPYTIFY_TARGET_LAUNCH"))
	save := widget.NewButtonWithIcon("Save and restart spytifyd", theme.DocumentSaveIcon(), p.save)
	save.Importance = widget.HighImportance
	rows = append(rows, muted("Saved to "+target.UserEnvPath()), container.NewHBox(save))
	p.card = widget.NewCard("Player", "The application spytify records. Nothing in spytify names a player; you choose it here.", container.NewVBox(rows...))
	// Until the daemon answers, show what this process would start it with.
	p.process.SetText(os.Getenv("SPYTIFY_TARGET_PROCESS"))
	p.bundle.SetText(os.Getenv("SPYTIFY_TARGET_BUNDLE_ID"))
	p.mpris.SetText(os.Getenv("SPYTIFY_TARGET_MPRIS"))
	p.launch.SetText(os.Getenv("SPYTIFY_TARGET_LAUNCH"))
	return p
}

func (p *playerCard) render(s model.State) {
	if p.filled || !s.HaveInfo {
		return
	}
	p.filled = true
	p.process.SetText(s.Info.Target.Process)
	p.bundle.SetText(s.Info.Target.BundleID)
	p.launch.SetText(s.Info.Target.Launch)
	if mprisName := s.Info.Target.MPRIS; mprisName != "" && !strings.Contains(mprisName, ",") && mprisName != "org.mpris.MediaPlayer2."+strings.ToLower(s.Info.Target.Process) {
		p.mpris.SetText(mprisName)
	}
}

func (p *playerCard) save() {
	values := map[string]string{
		"SPYTIFY_TARGET_PROCESS":   p.process.Text,
		"SPYTIFY_TARGET_BUNDLE_ID": p.bundle.Text,
		"SPYTIFY_TARGET_MPRIS":     p.mpris.Text,
		"SPYTIFY_TARGET_LAUNCH":    p.launch.Text,
	}
	if strings.TrimSpace(values["SPYTIFY_TARGET_PROCESS"]) == "" {
		p.app.showError(fmt.Errorf("the player process name is required"))
		return
	}
	restart := func() {
		if err := target.SaveUserTarget(values); err != nil {
			p.app.showError(err)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if p.app.link.Do(ctx, proto.MethodCoreShutdown, nil, nil) == nil {
				time.Sleep(time.Second)
			}
			if err := p.app.link.StartDaemon(); err != nil {
				fyne.Do(func() { p.app.showError(err) })
			}
			fyne.Do(func() { p.filled = false })
		}()
	}
	if p.app.last.Recording() {
		dialog.ShowConfirm("Restart spytifyd?", "A session is recording; restarting stops it now.", func(ok bool) {
			if ok {
				restart()
			}
		}, p.app.win)
		return
	}
	restart()
}

// sortedKeys is used by tests to walk the fields in a stable order.
func (s *settingsScreen) sortedKeys() []string {
	keys := make([]string, 0, len(s.fields))
	for k := range s.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// focused reports whether the user is typing in o; render leaves it alone.
func (s *settingsScreen) focused(o fyne.Focusable) bool {
	return s.app.win.Canvas().Focused() == o
}
