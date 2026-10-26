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
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/core/naming"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// settingsScreen is one card per group of the validation table: the
// settings most people change, then the rest folded under Advanced in a
// greyer block. The player, which few people change, is the App card's
// advanced part.
type settingsScreen struct {
	app      *App
	root     fyne.CanvasObject
	search   *widget.Entry
	sections *fyne.Container
	scroll   *container.Scroll

	fields    map[string]*field
	rendering bool
	// example shows, at the end of the file name template's block, the
	// file a sample track gets with the output settings as they are.
	example  *widget.RichText
	template *settings.Def
	player   *playerCard
	// advanced remembers which Advanced parts the user opened.
	advanced  map[string]*widget.Accordion
	open      map[string]bool
	lastQuery string
}

type field struct {
	def   *settings.Def
	set   func(v any)
	input fyne.CanvasObject
	// row is the label on the left of the input, as a card shows it.
	row fyne.CanvasObject
}

func newSettingsScreen(a *App) *settingsScreen {
	s := &settingsScreen{app: a, fields: map[string]*field{}, advanced: map[string]*widget.Accordion{}, open: map[string]bool{}}
	s.search = widget.NewEntry()
	s.search.SetPlaceHolder("Search settings")
	s.search.ActionItem = widget.NewIcon(theme.SearchIcon())
	s.search.OnChanged = func(string) { s.layout() }
	s.sections = container.New(layout.NewCustomPaddedVBoxLayout(theme.Padding() * 3))
	s.scroll = container.NewVScroll(container.NewPadded(s.sections))
	s.player = newPlayerCard(a)
	s.player.relayout = func() { relayout(s.scroll) }
	for i := range settings.Table {
		d := &settings.Table[i]
		s.fields[d.Key] = s.newField(d)
	}
	s.layout()
	// The search is a white field on the page, as the cards are.
	search := container.NewThemeOverride(container.NewPadded(s.search), cardTheme{input: colorCard})
	// Lighter shadows at the scrolled edges, as in the cards.
	s.root = container.NewBorder(search, nil, nil, nil, container.NewThemeOverride(s.scroll, cardTheme{}))
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
	return q == "" || strings.Contains(strings.ToLower(d.Label), q) || strings.Contains(d.Key, q) ||
		strings.Contains(strings.ToLower(strings.Join(d.Choices, " ")), q)
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
		var main, adv []fyne.CanvasObject
		var keys []string
		for _, d := range settings.Table {
			if d.Group != g || !matches(q, d) {
				continue
			}
			if d.Advanced {
				adv = append(adv, s.fields[d.Key].row)
			} else {
				main = append(main, s.fields[d.Key].row)
			}
			keys = append(keys, d.Key)
		}
		player := g == "App" && (q == "" || strings.Contains("player process bundle mpris launch", q))
		if len(keys) == 0 && !player {
			continue
		}
		var block []fyne.CanvasObject
		if player {
			block = append(block, s.player.box)
		}
		block = lines(append(block, adv...))
		// The example follows the settings that make it.
		if s.template != nil && s.template.Group == g && len(adv) > 0 {
			block = append(block, s.example)
		}
		rows := main
		if len(block) > 0 {
			acc := widget.NewAccordion(widget.NewAccordionItem("Advanced", newBlock(block...)))
			if q != "" || s.open[g] {
				acc.Open(0)
			}
			s.advanced[g] = acc
			rows = append(rows, acc)
		}
		sections = append(sections, newCard(s.header(g, keys), indent(lines(rows)...)))
	}
	s.sections.Objects = sections
	s.sections.Refresh()
}

func (s *settingsScreen) header(group string, keys []string) fyne.CanvasObject {
	if len(keys) == 0 {
		return sectionTitle(group)
	}
	reset := widget.NewButtonWithIcon("Reset", theme.HistoryIcon(), func() {
		s.app.noteAction("reset " + group)
		s.resetKeys(keys)
	})
	reset.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, sectionTitle(group), reset)
}

// sectionTitle is a card's title, the strongest text of the screen.
func sectionTitle(text string) *widget.RichText {
	return widget.NewRichText(&widget.TextSegment{Text: text, Style: widget.RichTextStyle{
		ColorName: colorTitle,
		SizeName:  theme.SizeNameSubHeadingText,
		TextStyle: fyne.TextStyle{Bold: true},
		Inline:    true,
	}})
}

// newCard is a settings section on the page: its title, then its rows.
func newCard(objs ...fyne.CanvasObject) fyne.CanvasObject {
	pad := theme.Padding()
	box := newPanel(colorCard, colorCardLine, 10, fyne.NewSize(pad*3, pad*2), container.NewVBox(objs...))
	return container.NewThemeOverride(box, cardTheme{input: colorCardInput, surface: colorCard})
}

// newBlock holds a card's advanced settings: greyer, in smaller text.
func newBlock(objs ...fyne.CanvasObject) fyne.CanvasObject {
	pad := theme.Padding()
	box := newPanel(colorAdvanced, "", 8, fyne.NewSize(pad*3, pad), container.NewVBox(objs...))
	return container.NewThemeOverride(box, cardTheme{input: colorAdvancedInput, surface: colorAdvanced, small: true})
}

// indent sets a card's rows in from its title.
func indent(objs ...fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(0, 0, theme.Padding()*3, 0), container.NewVBox(objs...))
}

// lines puts a thin line between rows.
func lines(rows []fyne.CanvasObject) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	for i, r := range rows {
		if i > 0 {
			out = append(out, widget.NewSeparator())
		}
		out = append(out, r)
	}
	return out
}

// settingRow is a setting's label, with under beneath it when set, on the
// left of its control.
func settingRow(label string, control, under fyne.CanvasObject) fyne.CanvasObject {
	var left fyne.CanvasObject = widget.NewRichText(&widget.TextSegment{Text: label, Style: widget.RichTextStyle{ColorName: colorLabel, Inline: true}})
	if under != nil {
		left = container.New(layout.NewCustomPaddedVBoxLayout(0), left, under)
	}
	return container.New(rowLayout{}, left, control)
}

// rowLayout puts a label on the left and a control against the right
// edge. Switches and buttons keep their size; other controls share one
// width, so they line up down a card.
type rowLayout struct{}

const minControlWidth = 240

func natural(control fyne.CanvasObject) bool {
	switch control.(type) {
	case *toggle, *widget.Button:
		return true
	}
	return false
}

func (rowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	label, control := objs[0].MinSize(), objs[1].MinSize()
	w := control.Width
	if !natural(objs[1]) {
		w = fyne.Max(w, minControlWidth)
	}
	pad := theme.Padding()
	return fyne.NewSize(label.Width+pad*4+w, fyne.Max(label.Height, control.Height)+pad*2)
}

func (rowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	label, control := objs[0], objs[1]
	gap := theme.Padding() * 4
	lm, cm := label.MinSize(), control.MinSize()
	w := cm.Width
	if !natural(control) {
		w = fyne.Max(cm.Width, fyne.Min(fyne.Max(minControlWidth, size.Width*0.45), size.Width-lm.Width-gap))
	}
	control.Resize(fyne.NewSize(w, cm.Height))
	control.Move(fyne.NewPos(size.Width-w, (size.Height-cm.Height)/2))
	label.Resize(fyne.NewSize(size.Width-w-gap, lm.Height))
	label.Move(fyne.NewPos(0, (size.Height-lm.Height)/2))
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
	// under goes below the label, in the same row.
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
		// The list shows a choice's words, the setting keeps its value.
		shown := opts
		if len(d.Choices) == len(opts) {
			shown = d.Choices
		}
		swap := func(s string, from, to []string) string {
			for i, x := range from {
				if x == s {
					return to[i]
				}
			}
			return s
		}
		sel := widget.NewSelect(shown, func(c string) { s.apply(key, swap(c, shown, opts)) })
		f.set = func(v any) { sel.SetSelected(swap(fmt.Sprint(number(v)), opts, shown)) }
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
			s.example = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: colorSubtle, Inline: true}})
			s.example.Truncation = fyne.TextTruncateEllipsis
			s.template = d
		}
		f.set = func(v any) {
			if !s.focused(e) {
				e.show(fmt.Sprint(number(v)))
			}
		}
		f.input = e
	}
	f.row = settingRow(d.Label, f.input, under)
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
	// The counter's settings mean something only with the counter prefix.
	prefix, _ := st.Settings["output.prefix"].(string)
	s.enable("output.counter_next", prefix == "counter")
	s.enable("output.counter_width", prefix == "counter")
	s.player.render(st)
}

// enable turns a setting's input on or off.
func (s *settingsScreen) enable(key string, on bool) {
	f, ok := s.fields[key]
	if !ok {
		return
	}
	if d, ok := f.input.(fyne.Disableable); ok {
		if on {
			d.Enable()
		} else {
			d.Disable()
		}
	}
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
	}, whole("output.counter_next"))
}

// focused reports whether the user is typing in o; render leaves it alone.
func (s *settingsScreen) focused(o fyne.Focusable) bool {
	return s.app.win.Canvas().Focused() == o
}

// playerCard edits the SPYTIFY_TARGET_* variables in the user's .env file,
// saving each change as the other settings do. The daemon reads them only
// when it starts, so after a change the card offers to restart it.
type playerCard struct {
	app     *App
	box     fyne.CanvasObject
	process *applyEntry
	bundle  *applyEntry
	mpris   *applyEntry
	launch  *applyEntry
	notice  fyne.CanvasObject
	filled  bool
	// pending is set from a saved change until spytifyd restarts.
	pending bool
	// relayout lays out the screen showing the card once the notice
	// showed or hid.
	relayout func()
}

func newPlayerCard(a *App) *playerCard {
	p := &playerCard{app: a, relayout: func() {}}
	entry := func(placeholder string) *applyEntry {
		e := newApplyEntry(func(string) { p.save() })
		e.SetPlaceHolder(placeholder)
		return e
	}
	p.process = entry("process name, or a comma list")
	p.bundle = entry("bundle id")
	p.mpris = entry("org.mpris.MediaPlayer2.<name> (default)")
	p.launch = entry("command that starts the player (optional)")
	rows := []fyne.CanvasObject{settingRow("Player process", p.process, nil)}
	switch runtime.GOOS {
	case "darwin":
		rows = append(rows, settingRow("Bundle id", p.bundle, nil))
	case "linux":
		rows = append(rows, settingRow("MPRIS name", p.mpris, nil))
	}
	rows = append(rows, settingRow("Launch command", p.launch, nil))
	restart := widget.NewButtonWithIcon("Restart spytifyd", theme.ViewRefreshIcon(), p.restart)
	note := widget.NewRichText(&widget.TextSegment{Text: "Saved. spytifyd records this player once it restarts.", Style: widget.RichTextStyle{ColorName: colorSubtle, Inline: true}})
	p.notice = container.New(rowLayout{}, note, restart)
	p.notice.Hide()
	p.box = container.NewVBox(append(lines(rows), p.notice)...)
	// Until the daemon answers, show what this process would start it with.
	t := target.Load().Target
	p.process.show(strings.Join(t.ProcessNames, ","))
	p.bundle.show(t.BundleID)
	p.mpris.show(os.Getenv("SPYTIFY_TARGET_MPRIS"))
	p.launch.show(t.Launch)
	return p
}

func (p *playerCard) render(s model.State) {
	p.showNotice(p.pending && s.Conn == model.Connected)
	if p.filled || !s.HaveInfo {
		return
	}
	p.filled = true
	p.process.show(s.Info.Target.Process)
	p.bundle.show(s.Info.Target.BundleID)
	p.launch.show(s.Info.Target.Launch)
	if mprisName := s.Info.Target.MPRIS; mprisName != "" && !strings.Contains(mprisName, ",") && mprisName != "org.mpris.MediaPlayer2."+strings.ToLower(s.Info.Target.Process) {
		p.mpris.show(mprisName)
	}
}

func (p *playerCard) showNotice(on bool) {
	if on == p.notice.Visible() {
		return
	}
	if on {
		p.notice.Show()
	} else {
		p.notice.Hide()
	}
	p.relayout()
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
	p.app.noteAction("save the player")
	if err := target.SaveUserTarget(values); err != nil {
		p.app.showError(err)
		return
	}
	p.pending = true
	p.showNotice(p.app.last.Conn == model.Connected)
}

func (p *playerCard) restart() {
	p.app.noteAction("restart spytifyd for the player")
	restart := func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if p.app.link.Do(ctx, proto.MethodCoreShutdown, nil, nil) == nil {
				time.Sleep(time.Second)
			}
			err := p.app.link.StartDaemon()
			fyne.Do(func() {
				p.filled = false
				if err != nil {
					p.app.showError(err)
					return
				}
				p.pending = false
				p.showNotice(false)
			})
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
