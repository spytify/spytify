package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/pkg/proto"
	"golang.org/x/text/unicode/norm"
)

// The answers to "is this track recorded, and where". Two places ask: the
// session, through the finisher's recorded, before it records a track and
// in skip mode only; and the finisher, at the file name it picked, in every
// mode. These tests pin what both answer.

// ferry is the track most cases ask about; ferryName is its file with the
// test settings, and ferryKey its key in the index.
var ferry = meta.TrackInfo{Title: "Midnight Ferry", Artists: []string{"Neon Harbor"}, Album: "Harbor Lights"}

const (
	ferryName = "Neon Harbor - Midnight Ferry.wav"
	ferryKey  = "neon harbor\x1fmidnight ferry"
)

// cafe has accents, written composed as the player gives them.
var (
	cafe     = meta.TrackInfo{Title: norm.NFC.String("Café Lumière"), Artists: []string{norm.NFC.String("Zoë")}}
	cafeName = norm.NFC.String("Zoë - Café Lumière.wav")
)

var cd = pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}

// bench is a finisher over a fresh output folder. Files and index lines go
// there before open: a finisher reads the index as it opens.
type bench struct {
	t     *testing.T
	out   string
	store *settings.Store
	rec   *recorder

	mu   sync.Mutex
	gate chan struct{}
}

func newBench(t *testing.T, extra map[string]any) *bench {
	t.Helper()
	dir := t.TempDir()
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if rej := store.Override(mergeSettings(baseSettings, extra, map[string]any{"output.dir": out})); len(rej) > 0 {
		t.Fatalf("settings rejected: %v", rej)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return &bench{t: t, out: out, store: store, rec: &recorder{}}
}

// values is the finisher's settings: a job asks for them first, so a held
// gate keeps it in flight.
func (b *bench) values() settings.Values {
	b.mu.Lock()
	g := b.gate
	b.mu.Unlock()
	if g != nil {
		<-g
	}
	return b.store.Values()
}

// hold keeps the jobs from starting until release.
func (b *bench) hold() (release func()) {
	g := make(chan struct{})
	b.mu.Lock()
	b.gate = g
	b.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			b.mu.Lock()
			b.gate = nil
			b.mu.Unlock()
			close(g)
		})
	}
	b.t.Cleanup(release)
	return release
}

// open makes a finisher, which reads the index as it is now. ff is only
// needed to save a file.
func (b *bench) open(ff *encode.FFmpeg) *finisher {
	b.t.Helper()
	log, _ := NewLogger("", testWriter{b.t})
	emit := func(method string, payload any) {
		raw, _ := json.Marshal(payload)
		b.rec.add(method, json.RawMessage(raw))
	}
	fin := newFinisher(b.values, ff, emit, log)
	b.t.Cleanup(fin.close)
	return fin
}

func (b *bench) recorded(fin *finisher, info meta.TrackInfo) bool {
	return fin.recorded(info, b.store.Values())
}

// file writes data at rel, under the output folder.
func (b *bench) file(rel, data string) {
	b.t.Helper()
	p := filepath.Join(b.out, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *bench) read(rel string) string {
	b.t.Helper()
	data, err := os.ReadFile(filepath.Join(b.out, rel))
	if err != nil {
		b.t.Fatal(err)
	}
	return string(data)
}

func (b *bench) indexPath() string { return filepath.Join(b.out, ".spytify", "recorded.jsonl") }

// writeIndex writes the index as an earlier run left it.
func (b *bench) writeIndex(lines ...string) {
	b.t.Helper()
	if err := os.MkdirAll(filepath.Dir(b.indexPath()), 0o755); err != nil {
		b.t.Fatal(err)
	}
	data := ""
	for _, l := range lines {
		data += l
	}
	if err := os.WriteFile(b.indexPath(), []byte(data), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

// indexLine is a line of the index as every version writes it.
func indexLine(key, path string) string {
	k, _ := json.Marshal(key)
	p, _ := json.Marshal(path)
	return `{"Key":` + string(k) + `,"Path":` + string(p) + "}\n"
}

// wholeTake is a second of sound for info, played to its end.
func (b *bench) wholeTake(id string, info meta.TrackInfo) finishJob {
	b.t.Helper()
	path := filepath.Join(b.t.TempDir(), id+".wav")
	w, err := pcm.CreateWAV(path, cd)
	if err != nil {
		b.t.Fatal(err)
	}
	frames := cd.FramesFromMs(1000)
	if _, err := w.Write(pcm.Synth{Seed: 1}.Render(cd, frames)); err != nil {
		b.t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		b.t.Fatal(err)
	}
	return finishJob{id: id, info: info, tmp: path, format: cd, played: frames, length: frames}
}

// outcome is the file take id was saved to, relative to the output folder,
// or why it was skipped.
func (b *bench) outcome(id string) (saved string, skipped proto.SkipReason) {
	for _, ev := range b.rec.all() {
		var f proto.TrackFinishedEvent
		if ev.Method == proto.EventTrackFinished && ev.Decode(&f) == nil && f.ID == id {
			saved, _ = filepath.Rel(b.out, f.Path)
		}
		var s proto.TrackSkippedEvent
		if ev.Method == proto.EventTrackSkipped && ev.Decode(&s) == nil && s.ID == id {
			skipped = s.Reason
		}
	}
	return saved, skipped
}

func needFFmpeg(t *testing.T) *encode.FFmpeg {
	t.Helper()
	ff, err := encode.Find("ffmpeg")
	if err != nil {
		t.Skip(err)
	}
	return ff
}

// caseBlind reports whether the file system under dir finds a name in
// another case, as macOS and Windows do by default and Linux does not.
func caseBlind(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "PROBE"))
	return err == nil
}

// accentBlind reports whether the file system finds a name whose accents
// are decomposed on disk under their composed form, as macOS does.
func accentBlind(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, norm.NFD.String("é")), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, norm.NFC.String("é")))
	return err == nil
}

type indexEntry struct{ key, rel string }

// What the session hears from the finisher's recorded, for the files and
// the index an earlier run or the user left in the output folder.
func TestRecordedAnswers(t *testing.T) {
	guest := ferry
	guest.Artists = []string{"Neon Harbor", "Lumen"}
	shouting := meta.TrackInfo{Title: " MIDNIGHT FERRY ", Artists: []string{"neon harbor "}}
	for _, c := range []struct {
		name     string
		settings map[string]any
		info     meta.TrackInfo
		files    []string
		index    []indexEntry
		want     bool
	}{
		{name: "nothing saved"},
		{name: "its file, saved before the index or by hand", files: []string{ferryName}, want: true},
		{name: "in the index, its file elsewhere", files: []string{"Old/Ferry.flac"}, index: []indexEntry{{ferryKey, "Old/Ferry.flac"}}, want: true},
		{name: "in the index, its file deleted", index: []indexEntry{{ferryKey, "Old/Ferry.flac"}}},
		{name: "in the index, its file deleted, one at its name", files: []string{ferryName}, index: []indexEntry{{ferryKey, "Old/Ferry.flac"}}, want: true},
		{name: "the index's last line for a track wins", files: []string{"Old/Ferry.flac"}, index: []indexEntry{{ferryKey, "Old/Ferry.flac"}, {ferryKey, "Old/Ferry 2.flac"}}},
		{name: "a line that is not JSON is passed over", files: []string{"Old/Ferry.flac"}, index: []indexEntry{{"", "not json"}, {ferryKey, "Old/Ferry.flac"}}, want: true},
		{name: "by hand, in another folder", files: []string{filepath.Join("Elsewhere", ferryName)}},
		{name: "in another format", files: []string{"Neon Harbor - Midnight Ferry.flac"}},
		{name: "a counter prefix", files: []string{"007 " + ferryName}, want: true},
		{name: "a long counter and an underscore", files: []string{"000000012_" + ferryName}, want: true},
		{name: "an album track prefix", files: []string{"04 " + ferryName}, want: true},
		{name: "a prefix with a letter", files: []string{"A7 " + ferryName}},
		{name: "digits and no separator", files: []string{"007" + ferryName}},
		{name: "digits and a dash", files: []string{"007-" + ferryName}},
		{name: "a duplicate suffix alone", files: []string{"Neon Harbor - Midnight Ferry 2.wav"}},
		{name: "counter on, its file without one", settings: map[string]any{"output.prefix": "counter"}, files: []string{ferryName}, want: true},
		{name: "counter on, its file under another number", settings: map[string]any{"output.prefix": "counter", "output.counter_next": 9}, files: []string{"007 " + ferryName}, want: true},
		{name: "underscores on, its file with spaces", settings: map[string]any{"output.underscores": true}, files: []string{ferryName}},
		{name: "underscores on, its file with a counter", settings: map[string]any{"output.underscores": true}, files: []string{"007_Neon_Harbor_-_Midnight_Ferry.wav"}, want: true},
		{name: "grouped, in its folder", settings: map[string]any{"output.group_by": "artist/album"}, files: []string{filepath.Join("Neon Harbor", "Harbor Lights", ferryName)}, want: true},
		{name: "grouped, at the root", settings: map[string]any{"output.group_by": "artist/album"}, files: []string{ferryName}},
		// The file is named after the album artist, the first artist when
		// the player gives none.
		{name: "a guest artist, its file", info: guest, files: []string{ferryName}, want: true},
		{name: "the index names the first artist only", info: guest, files: []string{"Old/Ferry.flac"}, index: []indexEntry{{ferryKey, "Old/Ferry.flac"}}, want: true},
		{name: "the index ignores case and spaces around", info: shouting, files: []string{"Old/Ferry.flac"}, index: []indexEntry{{ferryKey, "Old/Ferry.flac"}}, want: true},
		{name: "the index keeps accents", info: cafe, files: []string{"Old/Cafe.flac"}, index: []indexEntry{{"zoe\x1fcafe lumiere", "Old/Cafe.flac"}}},
		{name: "accents decomposed on disk", info: cafe, files: []string{norm.NFD.String(cafeName)}, want: true},
		{name: "diacritics removed since", settings: map[string]any{"output.ascii_filenames": true}, info: cafe, files: []string{cafeName}},
		{name: "diacritics removed since, in the index", settings: map[string]any{"output.ascii_filenames": true}, info: cafe, files: []string{cafeName},
			index: []indexEntry{{norm.NFC.String("zoë\x1fcafé lumière"), cafeName}}, want: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBench(t, c.settings)
			for _, f := range c.files {
				b.file(f, "audio")
			}
			var lines []string
			for _, e := range c.index {
				if e.key == "" {
					lines = append(lines, e.rel+"\n")
					continue
				}
				lines = append(lines, indexLine(e.key, filepath.Join(b.out, e.rel)))
			}
			if len(lines) > 0 {
				b.writeIndex(lines...)
			}
			info := c.info
			if info.Title == "" {
				info = ferry
			}
			if got := b.recorded(b.open(nil), info); got != c.want {
				t.Errorf("recorded %v, want %v", got, c.want)
			}
		})
	}
}

// The folder is looked at by name: a file whose name differs only in case
// counts where the file system ignores case.
func TestRecordedFileInAnotherCase(t *testing.T) {
	b := newBench(t, nil)
	b.file("neon harbor - midnight ferry.wav", "audio")
	if got, want := b.recorded(b.open(nil), ferry), caseBlind(t); got != want {
		t.Errorf("recorded %v, want %v, as the file system finds the name", got, want)
	}
}

// A job handed to the finisher claims its track until it is done, whatever
// becomes of it: meanwhile the session takes the track for recorded, as the
// file is not there yet.
func TestAJobInFlightClaimsItsTrack(t *testing.T) {
	b := newBench(t, nil)
	fin := b.open(nil)
	// Another finisher, such as track.keep's when no session runs, has
	// claims of its own.
	other := b.open(nil)
	release := b.hold()
	// Cut short, it is skipped once it runs, and nothing is saved.
	fin.enqueue(finishJob{id: "s-1", info: ferry, format: cd, played: cd.FramesFromMs(1000), length: cd.FramesFromMs(60_000)})
	if !b.recorded(fin, ferry) {
		t.Error("in flight: not recorded")
	}
	if !b.recorded(fin, meta.TrackInfo{Title: "MIDNIGHT ferry ", Artists: []string{" Neon HARBOR"}}) {
		t.Error("in flight, asked in another case: not recorded")
	}
	if b.recorded(fin, cafe) {
		t.Error("another track recorded")
	}
	if b.recorded(other, ferry) {
		t.Error("recorded for another finisher")
	}
	release()
	fin.wait()
	if _, why := b.outcome("s-1"); why != proto.SkipIncomplete {
		t.Fatalf("the job was %q, want skipped as incomplete", why)
	}
	if b.recorded(fin, ferry) {
		t.Error("still recorded once the job was skipped")
	}
}

// A take cut short, one the session skipped and one saved with track.keep
// claim nothing: none of them makes the whole track a duplicate.
func TestJobsThatClaimNothing(t *testing.T) {
	for name, j := range map[string]finishJob{
		"cut short":       {partial: true},
		"session skipped": {rejected: proto.SkipAd},
		"kept":            {keep: true, partial: true},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBench(t, nil)
			fin := b.open(nil)
			release := b.hold()
			// A kept take whose audio cannot be read fails before it is
			// encoded.
			j.id, j.info, j.format, j.played, j.length = "s-1", ferry, cd, cd.FramesFromMs(1000), cd.FramesFromMs(1000)
			j.tmp = filepath.Join(b.out, "missing.wav")
			fin.enqueue(j)
			if b.recorded(fin, ferry) {
				t.Error("in flight: recorded")
			}
			release()
			fin.wait()
		})
	}
}

// In skip mode the finisher looks for a file at the name it picked: one
// there skips the take, though the session did not find it before it
// recorded. The file stays as it was, and so does the index.
func TestTheFinisherSkipsATakeWhoseFileIsThere(t *testing.T) {
	for _, c := range []struct {
		name     string
		settings map[string]any
		file     string
	}{
		{"no prefix", nil, ferryName},
		{"the same counter number", map[string]any{"output.prefix": "counter", "output.counter_next": 7}, "007 " + ferryName},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBench(t, mergeSettings(map[string]any{"recorder.duplicates": "skip"}, c.settings))
			b.file(c.file, "before")
			fin := b.open(nil)
			fin.advance = func(n int64) { t.Errorf("the counter moved on to %d", n) }
			j := b.wholeTake("s-1", ferry)
			fin.enqueue(j)
			fin.wait()
			if saved, why := b.outcome("s-1"); saved != "" || why != proto.SkipDuplicate {
				t.Fatalf("saved %q, skipped %q; want skipped as a duplicate", saved, why)
			}
			if got := b.read(c.file); got != "before" {
				t.Errorf("the file there became %q", got)
			}
			if files := fake.AudioFiles(b.out); !slices.Equal(files, []string{c.file}) {
				t.Errorf("files %v", files)
			}
			if _, err := os.Stat(b.indexPath()); !os.IsNotExist(err) {
				t.Errorf("index written: %v", err)
			}
			if cnt := fin.counters(); cnt.Skipped != 1 || cnt.Recorded != 0 {
				t.Errorf("counters %+v", cnt)
			}
			if _, err := os.Stat(j.tmp); !os.IsNotExist(err) {
				t.Errorf("the take's temp file is left: %v", err)
			}
		})
	}
}

// The finisher compares the whole name: a file under another counter
// number does not skip the take, which is saved under its own number. The
// session is the one that finds such a file, before it records.
func TestTheFinisherSkipComparesTheWholeName(t *testing.T) {
	ff := needFFmpeg(t)
	b := newBench(t, map[string]any{"recorder.duplicates": "skip", "output.prefix": "counter", "output.counter_next": 8})
	b.file("007 "+ferryName, "before")
	fin := b.open(ff)
	var next int64
	fin.advance = func(n int64) { next = n }
	fin.enqueue(b.wholeTake("s-1", ferry))
	fin.wait()
	if saved, why := b.outcome("s-1"); saved != "008 "+ferryName {
		t.Fatalf("saved %q, skipped %q", saved, why)
	}
	if files := fake.AudioFiles(b.out); !slices.Equal(files, []string{"007 " + ferryName, "008 " + ferryName}) {
		t.Errorf("files %v", files)
	}
	if next != 9 {
		t.Errorf("the counter moved on to %d, want 9", next)
	}
}

// The finisher's look at its name goes through the file system: a file
// whose name differs in case or in how its accents are written is the
// same file where the file system says so.
func TestTheFinisherSkipFollowsTheFileSystem(t *testing.T) {
	ff := needFFmpeg(t)
	for _, c := range []struct {
		name, file string
		info       meta.TrackInfo
		same       bool
	}{
		{"another case", "neon harbor - midnight ferry.wav", ferry, caseBlind(t)},
		{"accents decomposed", norm.NFD.String(cafeName), cafe, accentBlind(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBench(t, map[string]any{"recorder.duplicates": "skip"})
			b.file(c.file, "before")
			fin := b.open(ff)
			fin.enqueue(b.wholeTake("s-1", c.info))
			fin.wait()
			saved, why := b.outcome("s-1")
			if skipped := why == proto.SkipDuplicate; skipped != c.same || (saved == "") != c.same {
				t.Errorf("saved %q, skipped %q; the file system takes them for the same file: %v", saved, why, c.same)
			}
			if got := b.read(c.file); got != "before" {
				t.Errorf("the file there became %q", got)
			}
		})
	}
}

// In suffix mode the take gets the first free number, from 2; the files
// there stay as they were.
func TestSuffixModeTakesTheFirstFreeNumber(t *testing.T) {
	ff := needFFmpeg(t)
	for _, c := range []struct {
		name     string
		settings map[string]any
		files    []string
		want     string
	}{
		{"spaces", nil, []string{ferryName, "Neon Harbor - Midnight Ferry 2.wav", "Neon Harbor - Midnight Ferry 4.wav"}, "Neon Harbor - Midnight Ferry 3.wav"},
		{"underscores", map[string]any{"output.underscores": true}, []string{"Neon_Harbor_-_Midnight_Ferry.wav"}, "Neon_Harbor_-_Midnight_Ferry_2.wav"},
		// A file under another counter number is not in the way.
		{"another counter", map[string]any{"output.prefix": "counter", "output.counter_next": 8}, []string{"007 " + ferryName}, "008 " + ferryName},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBench(t, mergeSettings(map[string]any{"recorder.duplicates": "suffix"}, c.settings))
			for _, f := range c.files {
				b.file(f, "before")
			}
			fin := b.open(ff)
			fin.enqueue(b.wholeTake("s-1", ferry))
			fin.wait()
			if saved, why := b.outcome("s-1"); saved != c.want {
				t.Fatalf("saved %q, skipped %q; want %q", saved, why, c.want)
			}
			for _, f := range c.files {
				if got := b.read(f); got != "before" {
					t.Errorf("%s became %q", f, got)
				}
			}
			if got, want := b.read(filepath.Join(".spytify", "recorded.jsonl")), indexLine(ferryKey, filepath.Join(b.out, c.want)); got != want {
				t.Errorf("index %q, want %q", got, want)
			}
		})
	}
}

// In overwrite mode the take replaces the file of its name, and only that
// one: a file under another counter number stays.
func TestOverwriteModeReplacesTheFileOfItsName(t *testing.T) {
	ff := needFFmpeg(t)
	for _, c := range []struct {
		name     string
		settings map[string]any
		file     string
		want     string
		files    []string
	}{
		{"its name", nil, ferryName, ferryName, []string{ferryName}},
		{"another counter", map[string]any{"output.prefix": "counter", "output.counter_next": 8}, "007 " + ferryName, "008 " + ferryName, []string{"007 " + ferryName, "008 " + ferryName}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBench(t, mergeSettings(map[string]any{"recorder.duplicates": "overwrite"}, c.settings))
			b.file(c.file, "before")
			fin := b.open(ff)
			fin.enqueue(b.wholeTake("s-1", ferry))
			fin.wait()
			if saved, why := b.outcome("s-1"); saved != c.want {
				t.Fatalf("saved %q, skipped %q; want %q", saved, why, c.want)
			}
			if files := fake.AudioFiles(b.out); !slices.Equal(files, c.files) {
				t.Errorf("files %v, want %v", files, c.files)
			}
			replaced := b.read(c.file) != "before"
			if want := c.file == c.want; replaced != want {
				t.Errorf("%s replaced: %v, want %v", c.file, replaced, want)
			}
		})
	}
}

// A take saved with track.keep never skips nor replaces a file: it takes a
// suffix in every mode. It goes in no index, so it never makes the whole
// track a duplicate.
func TestAKeptTakeTakesASuffixInEveryMode(t *testing.T) {
	ff := needFFmpeg(t)
	partial := ferry
	partial.Title += " (partial)"
	for _, mode := range []string{"skip", "suffix", "overwrite"} {
		t.Run(mode, func(t *testing.T) {
			b := newBench(t, map[string]any{"recorder.duplicates": mode})
			b.file("Neon Harbor - Midnight Ferry (partial).wav", "before")
			fin := b.open(ff)
			j := b.wholeTake("s-1", partial)
			j.keep, j.partial = true, true
			fin.enqueue(j)
			fin.wait()
			if saved, why := b.outcome("s-1"); saved != "Neon Harbor - Midnight Ferry (partial) 2.wav" {
				t.Fatalf("saved %q, skipped %q", saved, why)
			}
			if got := b.read("Neon Harbor - Midnight Ferry (partial).wav"); got != "before" {
				t.Errorf("the file there became %q", got)
			}
			if _, err := os.Stat(b.indexPath()); !os.IsNotExist(err) {
				t.Errorf("index written: %v", err)
			}
			if b.recorded(fin, ferry) {
				t.Error("the whole track is recorded")
			}
		})
	}
}

// The index is a line of JSON per saved file, added at its end: the key is
// the first artist and the title, trimmed and in lower case, joined by
// U+001F, and the path is the file's. Older and newer versions read each
// other's lines, and a line whose file is gone stays.
func TestTheIndexFileFormat(t *testing.T) {
	ff := needFFmpeg(t)
	b := newBench(t, nil)
	gone := `{"Key":"copper lanterns\u001fhollow moon","Path":` + quoted(filepath.Join(b.out, "Gone.wav")) + "}\n"
	b.writeIndex(gone)
	fin := b.open(ff)
	fin.enqueue(b.wholeTake("s-1", meta.TrackInfo{Title: " Midnight Ferry", Artists: []string{"NEON Harbor ", "Lumen"}, Album: "Harbor Lights"}))
	fin.wait()
	saved, why := b.outcome("s-1")
	if saved == "" {
		t.Fatalf("skipped %q", why)
	}
	want := gone + `{"Key":"neon harbor\u001fmidnight ferry","Path":` + quoted(filepath.Join(b.out, saved)) + "}\n"
	if got := b.read(filepath.Join(".spytify", "recorded.jsonl")); got != want {
		t.Errorf("index\n%s\nwant\n%s", got, want)
	}
}

func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// A track saved is recorded at once, and for the next session too, which
// finds it through the index after the settings moved its name. Once its
// file is deleted, it is not.
func TestASavedTrackStaysRecorded(t *testing.T) {
	ff := needFFmpeg(t)
	b := newBench(t, nil)
	fin := b.open(ff)
	fin.enqueue(b.wholeTake("s-1", ferry))
	fin.wait()
	if saved, why := b.outcome("s-1"); saved != ferryName {
		t.Fatalf("saved %q, skipped %q", saved, why)
	}
	if !b.recorded(fin, ferry) {
		t.Error("not recorded once saved")
	}
	if rej := b.store.Override(map[string]any{"output.group_by": "artist/album", "encode.format": "flac"}); len(rej) > 0 {
		t.Fatal(rej)
	}
	next := b.open(nil)
	if !b.recorded(next, ferry) {
		t.Error("the next session, its name moved: not recorded")
	}
	if err := os.Remove(filepath.Join(b.out, ferryName)); err != nil {
		t.Fatal(err)
	}
	if b.recorded(next, ferry) {
		t.Error("recorded once its file was deleted")
	}
}
