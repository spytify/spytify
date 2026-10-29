package core

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/openfiles"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

// The settings timeline has accents, characters paths forbid, a title over
// the length limit, extra parts in titles, a featured artist, track
// numbers and years, a 1400 px cover, 1.5 s of silence at the start of a
// track, and a short item with no album.
const (
	accents  = "Ça plane (Live)"
	slash    = "Back - Remastered 2011"
	feat     = "Song (feat. Someone)"
	shortAd  = "Short Clip"
	longItem = 2
)

func mergeSettings(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// results maps each item's title to its saved file, and to its skip reason
// for the others.
func (s *scenario) results() (saved map[string]string, skipped map[string]proto.SkipReason) {
	titles := map[string]string{}
	saved, skipped = map[string]string{}, map[string]proto.SkipReason{}
	for _, ev := range s.events.all() {
		switch ev.Method {
		case proto.EventTrackStarted:
			var e proto.TrackStartedEvent
			if ev.Decode(&e) == nil {
				titles[e.Track.ID] = e.Track.Title
			}
		case proto.EventTrackFinished:
			var e proto.TrackFinishedEvent
			if ev.Decode(&e) == nil {
				saved[titles[e.ID]] = e.Path
			}
		case proto.EventTrackSkipped:
			var e proto.TrackSkippedEvent
			if ev.Decode(&e) == nil {
				skipped[titles[e.ID]] = e.Reason
			}
		}
	}
	return saved, skipped
}

func runSettings(t *testing.T, extra map[string]any) *scenario {
	t.Helper()
	tl := loadTimeline(t, "settings")
	s := newScenario(t, tl, extra)
	s.run(t, tl)
	return s
}

var digitsPrefix = regexp.MustCompile(`^(\d+)[ _]`)

// Every folder grouping and file name prefix, with and without underscores
// and diacritics, saves each track where the settings say, and a second
// play of the same tracks is caught as a duplicate in each layout.
func TestOutputLayoutForEverySetting(t *testing.T) {
	type layout struct {
		group, prefix      string
		underscores, ascii bool
	}
	var layouts []layout
	for _, g := range []string{"none", "artist", "artist/album", "artists", "artists/album"} {
		for _, p := range []string{"none", "counter", "album_track"} {
			layouts = append(layouts, layout{g, p, false, false})
		}
		layouts = append(layouts, layout{g, "counter", true, true})
	}
	for _, l := range layouts {
		name := fmt.Sprintf("%s/%s/underscores=%v/ascii=%v", l.group, l.prefix, l.underscores, l.ascii)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			settings := map[string]any{"output.group_by": l.group, "output.prefix": l.prefix, "output.underscores": l.underscores, "output.ascii_filenames": l.ascii}
			tl := loadTimeline(t, "settings")
			s := newScenario(t, tl, settings)
			s.run(t, tl)
			if problems := fake.Verify(tl, tl.Expect, -1, s.out, s.events.all()); len(problems) > 0 {
				t.Fatalf("%v\nlog:\n%s", problems, s.logs)
			}
			saved, _ := s.results()
			depth := map[string]int{"none": 0, "artist": 1, "artist/album": 2, "artists": 1, "artists/album": 2}[l.group]
			counters := map[string]int{accents: 1, slash: 2, tl.Items[longItem].Title: 3, feat: 4}
			tracks := map[string]int{accents: 7, slash: 1, tl.Items[longItem].Title: 2, feat: 3}
			for title, path := range saved {
				rel, _ := filepath.Rel(s.out, path)
				parts := strings.Split(filepath.ToSlash(rel), "/")
				if len(parts)-1 != depth {
					t.Errorf("%s in %d folders, want %d", rel, len(parts)-1, depth)
				}
				base := parts[len(parts)-1]
				m := digitsPrefix.FindStringSubmatch(base)
				switch l.prefix {
				case "none":
					if m != nil {
						t.Errorf("%s has a prefix", rel)
					}
				case "counter":
					if m == nil || m[1] != fmt.Sprintf("%03d", counters[title]) {
						t.Errorf("%s: counter prefix, want %03d", rel, counters[title])
					}
				case "album_track":
					if m == nil || m[1] != fmt.Sprintf("%02d", tracks[title]) {
						t.Errorf("%s: album track prefix, want %02d", rel, tracks[title])
					}
				}
				if l.underscores && strings.Contains(rel, " ") {
					t.Errorf("%s has a space", rel)
				}
				if l.ascii && strings.ContainsAny(rel, "çëÅÉéûà") {
					t.Errorf("%s has an accent", rel)
				}
			}

			again := newScenario(t, tl, mergeSettings(settings, map[string]any{"output.dir": s.out, "recorder.duplicates": "skip"}))
			again.run(t, tl)
			resaved, skipped := again.results()
			if len(resaved) != 0 {
				t.Errorf("the second play saved %v", resaved)
			}
			for title := range saved {
				if skipped[title] != proto.SkipDuplicate {
					t.Errorf("%q played again: %q, want a duplicate", title, skipped[title])
				}
			}
		})
	}
}

// Paths as the settings timeline gives them, read and pinned.
func TestOutputPathsOfTheSettingsTimeline(t *testing.T) {
	for _, c := range []struct {
		settings map[string]any
		want     map[string]string
	}{
		{map[string]any{"output.group_by": "artist/album"}, map[string]string{
			accents: "Zoë Ångström/Œuvres complètes (1977)/Zoë Ångström - Ça plane (Live).wav",
			slash:   "ACDC/Back in Black (1980)/ACDC - Back - Remastered 2011.wav",
			feat:    "Quiet Start/Lead (2024)/Quiet Start - Song (feat. Someone).wav",
		}},
		{map[string]any{"output.group_by": "none", "output.prefix": "counter", "output.underscores": true, "output.ascii_filenames": true}, map[string]string{
			accents: "001_Zoe_Angstrom_-_Ca_plane_(Live).wav",
			slash:   "002_ACDC_-_Back_-_Remastered_2011.wav",
			feat:    "004_Quiet_Start_-_Song_(feat._Someone).wav",
		}},
		{map[string]any{"output.group_by": "artist", "output.prefix": "album_track", "tags.extra_to_subtitle": true}, map[string]string{
			accents: "Zoë Ångström/07 Zoë Ångström - Ça plane - Live.wav",
			slash:   "ACDC/01 ACDC - Back - Remastered 2011.wav",
			feat:    "Quiet Start/03 Quiet Start - Song - feat. Someone.wav",
		}},
		{map[string]any{"output.group_by": "none", "output.template": "{track} {title} ({year})"}, map[string]string{
			accents: "07 Ça plane (Live) (1977).wav",
			slash:   "01 Back - Remastered 2011 (1980).wav",
			feat:    "03 Song (feat. Someone) (2024).wav",
		}},
	} {
		s := runSettings(t, c.settings)
		saved, _ := s.results()
		for title, want := range c.want {
			rel, _ := filepath.Rel(s.out, saved[title])
			if filepath.ToSlash(rel) != want {
				t.Errorf("%v: %q saved as %q, want %q", c.settings, title, filepath.ToSlash(rel), want)
			}
		}
		long, _ := filepath.Rel(s.out, saved[loadTimeline(t, "settings").Items[longItem].Title])
		if len(filepath.Base(long)) > 255 {
			t.Errorf("%v: the long title made a %d-byte name", c.settings, len(filepath.Base(long)))
		}
	}
}

type probed struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		BitRate   string `json:"bit_rate"`
		Width     int    `json:"width"`
	} `json:"streams"`
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

func ffprobe(t *testing.T, path string) probed {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_name,codec_type,bit_rate,width:format_tags", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var p probed
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{}
	for k, v := range p.Format.Tags {
		tags[strings.ToLower(k)] = v
	}
	p.Format.Tags = tags
	return p
}

func (p probed) stream(kind string) (codec string, bitRate int, width int, ok bool) {
	for _, s := range p.Streams {
		if s.CodecType == kind {
			br, _ := strconv.Atoi(s.BitRate)
			return s.CodecName, br, s.Width, true
		}
	}
	return "", 0, 0, false
}

// Each format and bitrate is what the file holds, with its tags: the extra
// part of the title as a subtitle (in the title for AAC), the artists
// joined with the separator, the track number and year from the player,
// and the cover at its size.
func TestEncodingSettingsReachTheFiles(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	tagged := map[string]any{"tags.extra_to_subtitle": true, "tags.artist_separator": " & ", "tags.cover": true, "tags.cover_size": "small"}
	for _, c := range []struct {
		format  string
		bitrate int
		codec   string
		title   string
		sub     string
	}{
		{"mp3", 128, "mp3", "Ça plane", "tit3"},
		{"mp3", 320, "mp3", "Ça plane", "tit3"},
		{"aac", 160, "aac", "Ça plane (Live)", ""},
		{"flac", 0, "flac", "Ça plane", "subtitle"},
	} {
		t.Run(fmt.Sprintf("%s %d", c.format, c.bitrate), func(t *testing.T) {
			t.Parallel()
			settings := mergeSettings(tagged, map[string]any{"encode.format": c.format})
			if c.bitrate > 0 {
				settings["encode.bitrate_kbps"] = c.bitrate
			}
			s := runSettings(t, settings)
			saved, _ := s.results()
			p := ffprobe(t, saved[accents])
			codec, br, _, _ := p.stream("audio")
			if codec != c.codec {
				t.Errorf("codec %s, want %s", codec, c.codec)
			}
			if c.bitrate > 0 && math.Abs(float64(br)/1000-float64(c.bitrate)) > 0.2*float64(c.bitrate) {
				t.Errorf("bitrate %d kb/s, want %d", br/1000, c.bitrate)
			}
			tags := p.Format.Tags
			if tags["title"] != c.title || (c.sub != "" && tags[c.sub] != "Live") {
				t.Errorf("title %q, subtitle %q; tags %v", tags["title"], tags[c.sub], tags)
			}
			if tags["artist"] != "Zoë Ångström & Élodie Brûlé & Guest" || !strings.HasPrefix(tags["track"], "7") || tags["date"] != "1977" {
				t.Errorf("artist %q, track %q, date %q", tags["artist"], tags["track"], tags["date"])
			}
			if _, _, width, ok := p.stream("video"); !ok || width != 300 {
				t.Errorf("cover: present %v, %d px wide, want 300", ok, width)
			}
			if got := ffprobe(t, saved[feat]).Format.Tags["artist"]; got != "Quiet Start & Someone" {
				t.Errorf("featured artist: %q", got)
			}
		})
	}

	t.Run("wav", func(t *testing.T) {
		s := runSettings(t, mergeSettings(tagged, map[string]any{"encode.format": "wav"}))
		saved, _ := s.results()
		b, err := os.ReadFile(saved[accents] + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var side struct {
			Title, Subtitle string
			Track, Year     int
		}
		if err := json.Unmarshal(b, &side); err != nil || side.Title != "Ça plane" || side.Subtitle != "Live" || side.Track != 7 || side.Year != 1977 {
			t.Errorf("sidecar %s: %v", b, err)
		}
	})

	t.Run("cover sizes and no cover", func(t *testing.T) {
		for size, want := range map[string]int{"medium": 640, "large": 1400} {
			s := runSettings(t, map[string]any{"encode.format": "mp3", "tags.cover": true, "tags.cover_size": size})
			saved, _ := s.results()
			if _, _, width, _ := ffprobe(t, saved[accents]).stream("video"); width != want {
				t.Errorf("%s: cover %d px, want %d", size, width, want)
			}
		}
		s := runSettings(t, map[string]any{"encode.format": "mp3", "tags.cover": false})
		saved, _ := s.results()
		if _, _, _, ok := ffprobe(t, saved[accents]).stream("video"); ok {
			t.Error("a cover with tags.cover off")
		}
	})
}

// loudness is the integrated loudness of a file, in LUFS.
func loudness(t *testing.T, path string) float64 {
	t.Helper()
	out, _ := exec.Command("ffmpeg", "-nostats", "-hide_banner", "-i", path, "-af", "ebur128", "-f", "null", "-").CombinedOutput()
	m := regexp.MustCompile(`(?s)Integrated loudness:\s*I:\s*(-?[\d.]+) LUFS`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("no loudness for %s:\n%s", path, out)
	}
	v, _ := strconv.ParseFloat(string(m[1]), 64)
	return v
}

// Normalizing brings a track to the target loudness; off, the track keeps
// the loudness it played at.
func TestLoudnessSettings(t *testing.T) {
	tl := loadTimeline(t, "settings")
	f, audio, err := tl.ItemAudio(1)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "source.wav")
	w, err := pcm.CreateWAV(src, f)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(audio)
	w.Close()
	source := loudness(t, src)

	off := runSettings(t, map[string]any{"encode.format": "flac"})
	saved, _ := off.results()
	if got := loudness(t, saved[slash]); math.Abs(got-source) > 0.5 {
		t.Errorf("normalize off: %.1f LUFS, the source is %.1f", got, source)
	}
	for _, target := range []float64{-20, -10} {
		on := runSettings(t, map[string]any{"encode.format": "flac", "recorder.normalize": true, "recorder.target_lufs": target})
		saved, _ := on.results()
		if got := loudness(t, saved[slash]); math.Abs(got-target) > 1 {
			t.Errorf("normalize to %.0f: %.1f LUFS (source %.1f)", target, got, source)
		}
	}
}

// With Normalize on, the Logs tab says once, in an info line, how far
// under the target a track ends when its peaks keep it there: raised as
// far as they allow, or left at its level when they already reach
// -1.5 dBTP. It says nothing of a track that reaches the target or is
// lowered to it.
func TestNormalizeSaysWhenItLeavesATrackUnderTheTarget(t *testing.T) {
	tl := loadTimeline(t, "basic")
	// The last track peaks at -0.5 dBFS, some 7 dB over its loudness.
	f, last := tl.Format(), &tl.Items[len(tl.Items)-1]
	last.WAV = filepath.Join(t.TempDir(), "peaks.wav")
	w, err := pcm.CreateWAV(last.WAV, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(pcm.Synth{LevelDB: -0.5, FadeInMs: 30, FadeOut: 30}.Render(f, f.FramesFromMs(last.LengthMs))); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`^"(.+)": (normalized \+\d+\.\d dB; its peaks kept it|left at its level; its peaks already reach -1\.5 dBTP,) \d+\.\d dB under (-\d+) LUFS$`)
	for _, c := range []struct {
		target float64
		// want is the lines of each track, by title, each by its first
		// word after the title.
		want map[string][]string
	}{
		{-5, map[string][]string{tl.Items[0].Title: {"normalized"}, tl.Items[1].Title: {"normalized"}, last.Title: {"left"}}},
		{-14, nil},
		{-30, nil},
	} {
		t.Run(fmt.Sprintf("%g LUFS", c.target), func(t *testing.T) {
			s := newScenario(t, tl, map[string]any{"recorder.normalize": true, "recorder.target_lufs": c.target})
			s.run(t, tl)
			got := map[string][]string{}
			for _, ev := range s.events.all() {
				var l proto.LogEvent
				switch {
				case ev.Method == proto.EventError:
					t.Errorf("error event %s", ev.Params)
				case ev.Method != proto.EventLog || ev.Decode(&l) != nil:
				case strings.Contains(l.Msg, " LUFS"):
					t.Logf("%s %s", l.Level, l.Msg)
					m := line.FindStringSubmatch(l.Msg)
					if m == nil || l.Level != "info" || m[3] != fmt.Sprint(c.target) {
						t.Errorf("%s line %q", l.Level, l.Msg)
						continue
					}
					got[m[1]] = append(got[m[1]], strings.Fields(m[2])[0])
				}
			}
			saved, _ := s.results()
			for _, it := range tl.Items {
				if saved[it.Title] == "" {
					t.Errorf("%q not saved; log:\n%s", it.Title, s.logs)
				}
				if !slices.Equal(got[it.Title], c.want[it.Title]) {
					t.Errorf("%q: lines %v, want %v", it.Title, got[it.Title], c.want[it.Title])
				}
			}
		})
	}
}

// leadMs is how long a WAV file stays under threshold dBFS at its start.
func leadMs(t *testing.T, path string, threshold float64) int64 {
	t.Helper()
	f, data, err := pcm.ReadWAV(path)
	if err != nil {
		t.Fatal(err)
	}
	frame := f.FramesFromMs(5) * int64(f.FrameBytes())
	for at := int64(0); at+frame <= int64(len(data)); at += frame {
		if peak, _ := f.Level(data[at : at+frame]); peak > threshold {
			return f.MsFromFrames(at / int64(f.FrameBytes()))
		}
	}
	return f.MsFromFrames(int64(len(data)) / int64(f.FrameBytes()))
}

// Trimming takes the silence off the start of a track; off, the 1.5 s of
// silence the track starts with stays.
func TestSilenceSettings(t *testing.T) {
	on := runSettings(t, map[string]any{"recorder.trim_silence": true, "recorder.silence_db": -50.0})
	saved, _ := on.results()
	if got := leadMs(t, saved[feat], -50); got > 100 {
		t.Errorf("trim on: %d ms of silence left at the start", got)
	}
	off := runSettings(t, map[string]any{"recorder.trim_silence": false})
	saved, _ = off.results()
	// The track starts in the gap before it: 600 ms, then its own 1.5 s.
	if got := leadMs(t, saved[feat], -50); got < 1500 || got > 2300 {
		t.Errorf("trim off: %d ms of silence at the start, want 1.5 to 2.1 s", got)
	}
}

// A short item with no album is taken for an ad, unless it is longer than
// the limit or the limit is 0.
func TestShortItemsWithNoAlbum(t *testing.T) {
	for limit, wantSaved := range map[int]bool{60: false, 2: true, 0: true} {
		s := runSettings(t, map[string]any{"recorder.skip_short_untitled": limit})
		saved, skipped := s.results()
		if _, ok := saved[shortAd]; ok != wantSaved {
			t.Errorf("limit %d s: the 3 s clip saved %v (skipped %q)", limit, ok, skipped[shortAd])
		}
		if !wantSaved && skipped[shortAd] != proto.SkipAd {
			t.Errorf("limit %d s: the clip skipped as %q, want ad", limit, skipped[shortAd])
		}
	}
}

// The session timer stops the session after it, once the track playing
// then is saved.
func TestTheSessionTimerEndsTheSession(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenarioAt(t, tl, map[string]any{"recorder.session_timer": "2s"}, true)
	start := time.Now()
	s.run(t, tl)
	saved, _ := s.results()
	if len(saved) != 1 || saved[tl.Items[0].Title] == "" {
		t.Errorf("saved %v, want the first track only", saved)
	}
	if took := time.Since(start); took > 9*time.Second {
		t.Errorf("the session ran %s with a 2 s timer", took)
	}
}

// heldUnder fails the test when this process holds anything open under
// dir: a drive with a file open cannot be ejected safely.
func heldUnder(t *testing.T, what, dir string) {
	t.Helper()
	held, err := openfiles.Held(os.Getpid(), dir)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if len(held) > 0 {
		t.Errorf("%s: the output folder is held open: %v", what, held)
	}
}

// When it is not recording, spytify holds nothing open in the output
// folder, which may be on a USB drive the user ejects: not before the
// first session, not after one that saved tracks (encoding, loudness,
// trimming, tags and covers all ran), not with a take kept for track.keep.
func TestTheOutputFolderIsLetGoWhenNotRecording(t *testing.T) {
	tl := loadTimeline(t, "settings")
	s := newScenario(t, tl, map[string]any{"encode.format": "mp3", "tags.cover": true, "recorder.normalize": true, "recorder.trim_silence": true})
	if err := os.MkdirAll(s.out, 0o755); err != nil {
		t.Fatal(err)
	}
	s.core.Startup()
	heldUnder(t, "before recording", s.out)
	s.run(t, tl)
	if saved, _ := s.results(); len(saved) != 4 {
		t.Fatalf("saved %v", saved)
	}
	heldUnder(t, "after a session", s.out)

	basic := loadTimeline(t, "basic")
	live := newScenarioAt(t, basic, map[string]any{"output.dir": s.out}, true)
	if _, err := live.core.StartSession(SessionOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	// While it records, the take's temp file is open: the check sees it.
	if held, err := openfiles.Held(os.Getpid(), s.out); err != nil || len(held) == 0 {
		t.Errorf("while recording, nothing seen held (%v): the check would miss a leak", err)
	}
	if _, err := live.core.StopSession(true); err != nil {
		t.Fatal(err)
	}
	live.core.Wait()
	keepable := false
	for _, ev := range live.events.all() {
		var e proto.TrackKeepableEvent
		keepable = keepable || (ev.Method == proto.EventTrackKeepable && ev.Decode(&e) == nil && e.Keepable)
	}
	if !keepable {
		t.Fatal("stopping mid-track left no take to keep")
	}
	heldUnder(t, "with a take kept for track.keep", s.out)
}

// decoded is a file decoded back to 16-bit PCM by ffmpeg, at its own rate.
func decoded(t *testing.T, path string) (pcm.Format, []byte) {
	t.Helper()
	wav := filepath.Join(t.TempDir(), "decoded.wav")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-i", path, "-c:a", "pcm_s16le", wav).CombinedOutput(); err != nil {
		t.Fatalf("decode %s: %v %s", path, err, out)
	}
	f, data, err := pcm.ReadWAV(wav)
	if err != nil {
		t.Fatal(err)
	}
	return f, data
}

// The bitrate setting is the bitrate of the file whatever the player
// delivers (rate, channels, 16-bit or float), a take in low quality kept
// after all included; the file keeps the source's rate up to 48 kHz; and a
// higher bitrate is closer to the source once decoded.
func TestBitrateWithEverySource(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	type source struct {
		rate, channels int
		sample         string
	}
	for _, src := range []source{{44100, 2, "s16"}, {48000, 2, "f32"}, {44100, 1, "s16"}, {88200, 2, "s16"}, {96000, 2, "f32"}} {
		t.Run(fmt.Sprintf("%d Hz %d ch %s", src.rate, src.channels, src.sample), func(t *testing.T) {
			t.Parallel()
			residual := map[string]float64{}
			for _, c := range []struct {
				format  string
				bitrate int
			}{{"mp3", 128}, {"mp3", 320}, {"aac", 256}} {
				enc := fmt.Sprintf("%s %d", c.format, c.bitrate)
				tl := loadTimeline(t, "basic")
				tl.Rate, tl.Channels, tl.Sample = src.rate, src.channels, src.sample
				s := newScenario(t, tl, map[string]any{"encode.format": c.format, "encode.bitrate_kbps": c.bitrate})
				s.run(t, tl)
				saved, _ := s.results()
				item := 1
				path := saved[tl.Items[item].Title]
				if poorFormat(tl.Format()) {
					// Mono is saved only once kept: the last take is held.
					item = len(tl.Items) - 1
					id := idOf(s, tl.Items[item].Title)
					if err := s.core.Keep(id); err != nil {
						t.Fatalf("%s: %v", enc, err)
					}
					path = finished(t, s, id).Path
				}
				if path == "" {
					t.Errorf("%s: not saved; log:\n%s", enc, s.logs)
					continue
				}
				_, br, _, _ := ffprobe(t, path).stream("audio")
				// AAC spends fewer bits than asked on simple content, mono most.
				low, high := 0.8, 1.2
				if c.format == "aac" {
					low = 0.7
				}
				if kbps := float64(br) / 1000; kbps < low*float64(c.bitrate) || kbps > high*float64(c.bitrate) {
					t.Errorf("%s: %.0f kb/s", enc, kbps)
				}
				gotF, got := decoded(t, path)
				wantRate := src.rate
				if src.rate > 48000 {
					wantRate = map[int]int{88200: 44100, 96000: 48000}[src.rate]
				}
				if gotF.Rate != wantRate || gotF.Channels != src.channels {
					t.Errorf("%s: file at %d Hz, %d channels; want %d Hz, %d", enc, gotF.Rate, gotF.Channels, wantRate, src.channels)
				}
				if gotF.Rate == src.rate {
					wantF, want, err := tl.ItemAudio(item)
					if err != nil {
						t.Fatal(err)
					}
					residual[enc] = pcm.Compare(wantF, want, gotF, got, fake.TrimDB, fake.SearchMs).ErrorDBFS
				}
			}
			if r128, ok := residual["mp3 128"]; ok {
				if r320 := residual["mp3 320"]; r128 > -40 || r320 > r128-5 {
					t.Errorf("decoded against the source: mp3 128 at %.1f dBFS, 320 at %.1f; want 320 at least 5 dB closer", r128, r320)
				}
			}
		})
	}
}
