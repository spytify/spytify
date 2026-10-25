package ui

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/core/naming"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// settingsScreen is one section per group of the validation table: the
// settings most people change, then the rest folded under Advanced. The
// player, which few people change, is the App section's advanced part.
type settingsScreen struct {
	app      *App
	root     fyne.CanvasObject
	search   *widget.Entry
	sections *fyne.Container

	fields    map[string]*field
	rendering bool
	// example shows, under the file name template, the file a sample
	// track gets with the output settings as they are.
	example *widget.RichText
	player  *playerCard
	// advanced remembers which Advanced parts the user opened.
	advanced  map[string]*widget.Accordion
	open      map[string]bool
	lastQuery string
}

type field struct {
	def   *settings.Def
	set   func(v any)
	input fyne.CanvasObject
	item  *widget.FormItem
}

func newSettingsScreen(a *App) *settingsScreen {
	s := &settingsScreen{app: a, fields: map[string]*field{}, advanced: map[string]*widget.Accordion{}, open: map[string]bool{}}
	s.search = widget.NewEntry()
	s.search.SetPlaceHolder("Search settings")
	s.search.ActionItem = widget.NewIcon(theme.SearchIcon())
	s.search.OnChanged = func(string) { s.layout() }
	s.sections = container.NewVBox()
	s.player = newPlayerCard(a)
	for i := range settings.Table {
		d := &settings.Table[i]
		s.fields[d.Key] = s.newField(d)
	}
	s.layout()
	s.root = container.NewBorder(container.NewPadded(s.search), nil, nil, nil, container.NewVScroll(container.NewPadded(s.sections)))
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

func matches(q string, d settings.Def) bool {
	return q == "" || strings.Contains(strings.ToLower(d.Label), q) || strings.Contains(d.Key, q)
}

func (s *settingsScreen) layout() {
	q := strings.ToLower(strings.TrimSpace(s.search.Text))
	// Remember what the user opened, not what a search opened.
	if s.lastQuery == "" {
		for g, acc := range s.advanced {
			s.open[g] = acc.Items[0].Open
		}
	}
	s.lastQuery = q
	s.advanced = map[string]*widget.Accordion{}
	var sections []fyne.CanvasObject
	for _, g := range groups() {
		var main, adv []*widget.FormItem
		var keys []string
		for _, d := range settings.Table {
			if d.Group != g || !matches(q, d) {
				continue
			}
			if d.Advanced {
				adv = append(adv, s.fields[d.Key].item)
			} else {
				main = append(main, s.fields[d.Key].item)
			}
			keys = append(keys, d.Key)
		}
		player := g == "App" && (q == "" || strings.Contains("player process bundle mpris launch", q))
		if len(keys) == 0 && !player {
			continue
		}
		var advanced []fyne.CanvasObject
		if player {
			advanced = append(advanced, widget.NewLabelWithStyle("Player", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), s.player.form)
		}
		if len(adv) > 0 {
			advanced = append(advanced, widget.NewForm(adv...))
		}
		body := []fyne.CanvasObject{s.header(g, keys)}
		if len(main) > 0 {
			body = append(body, widget.NewForm(main...))
		}
		if len(advanced) > 0 {
			acc := widget.NewAccordion(widget.NewAccordionItem("Advanced", container.NewVBox(advanced...)))
			if q != "" || s.open[g] {
				acc.Open(0)
			}
			s.advanced[g] = acc
			body = append(body, acc)
		}
		sections = append(sections, container.NewVBox(body...), widget.NewSeparator())
	}
	s.sections.Objects = sections
	s.sections.Refresh()
}

func (s *settingsScreen) header(group string, keys []string) fyne.CanvasObject {
	if len(keys) == 0 {
		return heading(group)
	}
	reset := widget.NewButtonWithIcon("Reset", theme.HistoryIcon(), func() {
		s.app.noteAction("reset " + group)
		s.resetKeys(keys)
	})
	reset.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, heading(group), reset)
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
			err = fmt.Errorf("%s: %s", s.fields[key].def.Label, res.Rejected[key])
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
	// under goes below the input, in the same row.
	var under fyne.CanvasObject
	switch d.Kind {
	case settings.KindBool:
		t := newToggle(func(b bool) { s.apply(key, b) })
		f.set = func(v any) { b, _ := v.(bool); t.SetOn(b) }
		f.input = t
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
		f.input = sel
	case settings.KindPath:
		e := newApplyEntry(func(v string) { s.apply(key, v) })
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
				e.show(str)
			}
		}
		f.input = container.NewBorder(nil, nil, nil, browse, e)
	default:
		e := newApplyEntry(func(v string) { s.apply(key, v) })
		if d.Kind == settings.KindTemplate {
			e.SetPlaceHolder(d.Note)
			s.example = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true}})
			under = s.example
		}
		f.set = func(v any) {
			if !s.focused(e) {
				e.show(fmt.Sprint(number(v)))
			}
		}
		f.input = e
	}
	cell := f.input
	if under != nil {
		cell = container.NewVBox(f.input, under)
	}
	f.item = &widget.FormItem{Text: d.Label, Widget: cell}
	return f
}

// applyEntry sends its text on Enter or when it loses focus, if it changed.
type applyEntry struct {
	widget.Entry
	shown string
	done  func(string)
}

func newApplyEntry(done func(string)) *applyEntry {
	e := &applyEntry{done: done}
	e.ExtendBaseWidget(e)
	e.OnSubmitted = func(string) { e.submit() }
	return e
}

func (e *applyEntry) show(text string) {
	e.shown = text
	e.SetText(text)
}

func (e *applyEntry) submit() {
	if e.Text != e.shown {
		e.shown = e.Text
		e.done(e.Text)
	}
}

func (e *applyEntry) FocusLost() {
	e.Entry.FocusLost()
	e.submit()
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
	// FLAC and WAV are lossless: a bitrate means nothing for them.
	if sel, ok := s.fields["encode.bitrate_kbps"].input.(*widget.Select); ok {
		if format, _ := st.Settings["encode.format"].(string); format == "flac" || format == "wav" {
			sel.Disable()
		} else {
			sel.Enable()
		}
	}
	if s.example != nil {
		setCaption(s.example, "e.g. "+exampleName(st.Settings))
	}
	s.player.render(st)
}

// exampleName is the path a sample track gets with the output settings in
// v, a default standing in for any missing.
func exampleName(v map[string]any) string {
	get := func(key string) any {
		if x, ok := v[key]; ok {
			return number(x)
		}
		if d, ok := settings.Lookup(key); ok {
			return d.Default
		}
		return nil
	}
	str := func(key string) string { s, _ := get(key).(string); return s }
	on := func(key string) bool { b, _ := get(key).(bool); return b }
	whole := func(key string) int64 { n, _ := get(key).(int64); return n }
	p, _ := encode.PresetFor(str("encode.format"))
	return naming.Example(naming.Options{
		GroupBy:         str("output.group_by"),
		Prefix:          str("output.prefix"),
		Underscores:     on("output.underscores"),
		Template:        str("output.template"),
		CounterWidth:    int(whole("output.counter_width")),
		ASCII:           on("output.ascii_filenames"),
		ArtistSeparator: str("tags.artist_separator"),
		Extension:       p.Extension,
	}, whole("output.counter_start"))
}

// focused reports whether the user is typing in o; render leaves it alone.
func (s *settingsScreen) focused(o fyne.Focusable) bool {
	return s.app.win.Canvas().Focused() == o
}

// playerCard edits the SPYTIFY_TARGET_* variables in the user's .env file
// and restarts the daemon, which reads them only when it starts.
type playerCard struct {
	app     *App
	form    fyne.CanvasObject
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
	form := widget.NewForm(&widget.FormItem{Text: "Player process", Widget: p.process})
	switch runtime.GOOS {
	case "darwin":
		form.AppendItem(&widget.FormItem{Text: "Bundle id", Widget: p.bundle})
	case "linux":
		form.AppendItem(&widget.FormItem{Text: "MPRIS name", Widget: p.mpris})
	}
	form.AppendItem(&widget.FormItem{Text: "Launch command", Widget: p.launch})
	save := widget.NewButtonWithIcon("Save and restart spytifyd", theme.DocumentSaveIcon(), p.save)
	save.Importance = widget.HighImportance
	p.form = container.NewVBox(form, container.NewHBox(save))
	// Until the daemon answers, show what this process would start it with.
	t := target.Load().Target
	p.process.SetText(strings.Join(t.ProcessNames, ","))
	p.bundle.SetText(t.BundleID)
	p.mpris.SetText(os.Getenv("SPYTIFY_TARGET_MPRIS"))
	p.launch.SetText(t.Launch)
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
	p.app.noteAction("save the player and restart spytifyd")
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
