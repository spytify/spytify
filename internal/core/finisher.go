package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/core/naming"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/pkg/proto"
)

// A track that played this much less than its declared length was cut
// short (a skip, a seek, a stop) and is not kept.
const completeToleranceMs = 2000

// An item with no declared length cannot be told whole, so one shorter
// than this is taken for a bit of one. A whole track is kept whatever its
// length.
const minUnknownLengthMs = 30_000

type finishJob struct {
	id      string
	info    meta.TrackInfo
	tmp     string
	format  pcm.Format
	played  int64
	length  int64
	partial bool
	// rejected is why the session already skipped this take, when it did:
	// the file is not made, but the audio is held for track.keep.
	rejected proto.SkipReason
	// keep saves a held take after all, whatever part of it played.
	keep bool
}

// finisher turns closed temp WAVs into tagged files, one at a time and in
// order, so counters and duplicate suffixes are assigned deterministically.
type finisher struct {
	values func() settings.Values
	ffmpeg *encode.FFmpeg
	emit   func(method string, payload any)
	log    *Logger

	jobs chan finishJob
	wg   sync.WaitGroup

	mu     sync.Mutex
	cnt    proto.Counters
	claims map[string]bool
	index  *recordIndex
	// advance stores the counter's next number once a file took one; full
	// stops the recording when the counter has no number left.
	advance func(next int64)
	full    func()
	// keep holds the last take cut short, for track.keep.
	keep *keeper
}

func newFinisher(values func() settings.Values, ff *encode.FFmpeg, emit func(string, any), log *Logger) *finisher {
	v := values()
	f := &finisher{
		values: values,
		ffmpeg: ff,
		emit:   emit,
		log:    log,
		jobs:   make(chan finishJob, 256),
		claims: map[string]bool{},
		index:  openIndex(filepath.Join(v.Path("output.dir"), ".spytify", "recorded.jsonl")),
	}
	go func() {
		for j := range f.jobs {
			f.process(j)
		}
	}()
	return f
}

func (f *finisher) enqueue(j finishJob) {
	// A partial take is discarded, so it does not make the track a
	// duplicate of itself.
	if !j.partial && j.rejected == "" {
		f.mu.Lock()
		f.claims[trackKey(j.info.Artist(), j.info.Title)] = true
		f.mu.Unlock()
	}
	f.wg.Add(1)
	f.jobs <- j
}

func (f *finisher) wait() { f.wg.Wait() }

func (f *finisher) close() {
	f.wg.Wait()
	close(f.jobs)
}

func (f *finisher) counters() proto.Counters {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cnt
}

func (f *finisher) addOverruns(n int64) {
	f.mu.Lock()
	f.cnt.Overruns += n
	f.mu.Unlock()
}

func (f *finisher) skip(id string, reason proto.SkipReason) {
	f.mu.Lock()
	f.cnt.Skipped++
	if reason == proto.SkipAd {
		f.cnt.Ads++
	}
	f.mu.Unlock()
	f.emit(proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: id, Reason: reason})
}

// recorded reports whether a track is already saved or being saved.
func (f *finisher) recorded(info meta.TrackInfo, v settings.Values) bool {
	key := trackKey(info.Artist(), info.Title)
	f.mu.Lock()
	claimed := f.claims[key]
	f.mu.Unlock()
	if claimed || f.index.has(key) {
		return true
	}
	o := namingOptions(v)
	local := tag.FromLocal(info, tagOptions(v))
	return naming.Recorded(v.Path("output.dir"), naming.Build(namingTrack(local, info), o), o)
}

func trackKey(artist, title string) string {
	return strings.ToLower(strings.TrimSpace(artist)) + "\x1f" + strings.ToLower(strings.TrimSpace(title))
}

func namingOptions(v settings.Values) naming.Options {
	p, _ := encode.PresetFor(v.String("encode.format"))
	return naming.Options{
		GroupBy:         v.String("output.group_by"),
		Prefix:          v.String("output.prefix"),
		Underscores:     v.Bool("output.underscores"),
		Template:        v.String("output.template"),
		CounterWidth:    int(v.Int("output.counter_width")),
		ASCII:           v.Bool("output.ascii_filenames"),
		ArtistSeparator: v.String("tags.artist_separator"),
		Extension:       p.Extension,
	}
}

func tagOptions(v settings.Values) tag.Options {
	return tag.Options{
		ExtraToSubtitle: v.Bool("tags.extra_to_subtitle"),
		ArtistSeparator: v.String("tags.artist_separator"),
		CoverSize:       v.String("tags.cover_size"),
		Cover:           v.Bool("tags.cover"),
	}
}

func namingTrack(t tag.Track, info meta.TrackInfo) naming.Track {
	title := tag.JoinTitle(t.Title, t.Subtitle, tag.SepDash)
	artists := t.Performers
	if len(artists) == 0 {
		artists = info.Artists
	}
	return naming.Track{
		Artists:       artists,
		AlbumArtists:  t.AlbumArtists,
		Title:         title,
		Album:         t.Album,
		TrackNumber:   t.Track,
		Year:          t.Year,
		Unknown:       strings.TrimSpace(title) == "",
		FallbackStamp: "Untitled " + time.Now().Format("20060102150405"),
	}
}

func (f *finisher) process(j finishJob) {
	defer f.wg.Done()
	defer func() {
		if j.partial || j.rejected != "" {
			return
		}
		f.mu.Lock()
		delete(f.claims, trackKey(j.info.Artist(), j.info.Title))
		f.mu.Unlock()
	}()
	held := false
	defer func() {
		if !held {
			os.Remove(j.tmp)
		}
	}()

	began := time.Now()
	since := func(t time.Time) int64 { return time.Since(t).Milliseconds() }
	v := f.values()
	playedMs := j.format.MsFromFrames(j.played)
	if j.rejected != "" {
		held = f.keep.hold(j)
		return
	}
	if !j.keep && (j.partial || (j.length > 0 && j.played < j.length-j.format.FramesFromMs(completeToleranceMs))) {
		f.log.Infof("%q: only %d of %d ms played, not kept", j.info.Title, playedMs, j.format.MsFromFrames(j.length))
		f.skip(j.id, proto.SkipIncomplete)
		held = f.keep.hold(j)
		return
	}
	tooShort := func(frames int64) bool {
		return j.length == 0 && !j.keep && j.format.MsFromFrames(frames) < minUnknownLengthMs
	}
	if tooShort(j.played) {
		f.log.Infof("%q: %d ms of an item with no length, under %d s", j.info.Title, playedMs, minUnknownLengthMs/1000)
		f.skip(j.id, proto.SkipTooShort)
		return
	}

	var trimStart, trimEnd int64
	keptFrames := j.played
	timings := &proto.FinishTimings{Format: v.String("encode.format")}
	if v.Bool("recorder.trim_silence") {
		step := time.Now()
		s, e, ok, err := scanTrim(j.tmp, v.Float("recorder.silence_db"))
		timings.TrimMs = since(step)
		if err != nil {
			f.fail(j, "read", err)
			return
		}
		if !ok {
			f.skip(j.id, proto.SkipTooShort)
			return
		}
		trimStart, trimEnd, keptFrames = s, e, e-s
		if tooShort(keptFrames) {
			f.skip(j.id, proto.SkipTooShort)
			return
		}
	}

	topts := tagOptions(v)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tags := tag.FromLocal(j.info, topts)
	if topts.Cover && tags.CoverURL != "" {
		step := time.Now()
		data, mime, err := tag.FetchCover(ctx, nil, tags.CoverURL)
		timings.CoverMs = since(step)
		if err == nil {
			tags.Cover, tags.CoverMIME = data, mime
		} else {
			f.log.Infof("%q: cover not embedded: %v", j.info.Title, err)
		}
	}

	o := namingOptions(v)
	// The counter numbers the saved files in the order they play, from one
	// recording to the next: it is a setting, which each file moves on. Past
	// the last number its digits allow, the file is not saved and the
	// recording stops, as v1's did.
	var counter int64
	if o.Prefix == "counter" {
		counter = v.Int("output.counter_next")
		if limit := naming.CounterLimit(o.CounterWidth); counter > limit {
			f.fail(j, "counter_full", fmt.Errorf("the counter is past %d, the last number of %d digits: raise Counter digits or set Next counter number back", limit, o.CounterWidth))
			if f.full != nil {
				f.full()
			}
			return
		}
	}
	plan := naming.Build(namingTrack(tags, j.info), o)
	root := v.Path("output.dir")
	dir := filepath.Join(root, plan.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.fail(j, "output_dir", err)
		return
	}
	name := naming.FileName(plan, o, counter, tags.Track)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	duplicates := v.String("recorder.duplicates")
	if j.keep {
		// A partial file never replaces a file, nor is it skipped.
		duplicates = "suffix"
	}
	switch duplicates {
	case "suffix":
		name = naming.FreeSuffix(dir, name, o.Underscores, exists)
	case "skip":
		if exists(filepath.Join(dir, name)) {
			f.skip(j.id, proto.SkipDuplicate)
			return
		}
	}
	final := filepath.Join(dir, name)
	part := final + ".part"
	format := timings.Format
	step := time.Now()
	err := f.ffmpeg.Encode(ctx, encode.Job{
		In:          j.tmp,
		Out:         part,
		Format:      format,
		BitrateKbps: int(v.Int("encode.bitrate_kbps")),
		Source:      j.format,
		TrimStart:   trimStart,
		TrimEnd:     trimEnd,
		Normalize:   v.Bool("recorder.normalize"),
		TargetLUFS:  v.Float("recorder.target_lufs"),
	})
	timings.EncodeMs = since(step)
	if err != nil {
		os.Remove(part)
		f.fail(j, "encode", err)
		return
	}
	tagged := true
	step = time.Now()
	err = (tag.Writer{FFmpeg: f.ffmpeg.Path}).Write(ctx, part, format, tags, topts)
	timings.TagMs = since(step)
	if err != nil {
		f.log.Errorf("%q: tags not written: %v", j.info.Title, err)
		tagged = false
	}
	if format == "wav" {
		if err := os.Rename(part+".json", final+".json"); err != nil && !os.IsNotExist(err) {
			f.log.Errorf("%q: sidecar: %v", j.info.Title, err)
		}
	}
	if err := os.Rename(part, final); err != nil {
		os.Remove(part)
		f.fail(j, "rename", err)
		return
	}
	var size int64
	if st, err := os.Stat(final); err == nil {
		size = st.Size()
	}
	f.mu.Lock()
	f.cnt.Recorded++
	f.cnt.Bytes += size
	f.mu.Unlock()
	if o.Prefix == "counter" && f.advance != nil {
		f.advance(counter + 1)
	}
	if !j.keep {
		// A partial file does not make the whole track a duplicate.
		f.index.add(trackKey(j.info.Artist(), j.info.Title), final)
		// The take held for track.keep goes once the next track is saved.
		f.keep.drop()
	}
	timings.TotalMs = since(began)
	f.log.Infof("saved %s", final)
	f.emit(proto.EventTrackFinished, proto.TrackFinishedEvent{
		ID:         j.id,
		Counter:    counter,
		Path:       final,
		DurationMs: j.format.MsFromFrames(keptFrames),
		Bytes:      size,
		Tagged:     tagged,
		TaggedBy:   tags.Source,
		Timings:    timings,
		Partial:    j.keep,
	})
}

func (f *finisher) fail(j finishJob, code string, err error) {
	f.log.Errorf("%q: %s: %v", j.info.Title, code, err)
	f.emit(proto.EventError, proto.ErrorEvent{Code: code, Msg: fmt.Sprintf("%s: %v", j.info.Title, err)})
	f.skip(j.id, proto.SkipIncomplete)
}

// scanTrim returns the frame range [start, end) of a WAV file between its
// first and last sample above db. A sample only counts when the 10 ms
// around it is above db too, so an isolated click does not stop the trim.
func scanTrim(path string, db float64) (start, end int64, ok bool, err error) {
	w, err := pcm.OpenWAV(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer w.Close()
	format := w.Format()
	fw := &frameWindow{w: w, size: format.FramesFromMs(4000)}
	block := format.FramesFromMs(10)
	total := w.Frames()
	thr := math.Pow(10, db/20)
	loudFrame := func(i int64) bool {
		b := fw.frames(i, i+1)
		for c := 0; c < format.Channels; c++ {
			if math.Abs(format.SampleAt(b, c)) > thr {
				return true
			}
		}
		return false
	}
	loudSpan := func(from, to int64) bool {
		_, rms := format.Level(fw.frames(from, to))
		return rms > db
	}
	start = -1
	for i := int64(0); i < total; i++ {
		if loudFrame(i) && loudSpan(i, i+block) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false, fw.err
	}
	end = start + 1
	for j := total - 1; j >= start; j-- {
		if loudFrame(j) && loudSpan(j+1-block, j+1) {
			end = j + 1
			break
		}
	}
	// silence_db finds the sound; the fade around it is kept down to 12 dB
	// under it, until 1 ms of quieter samples.
	floor := thr / 4
	quietRun := format.FramesFromMs(1)
	maxExtend := format.FramesFromMs(100)
	above := func(i int64) bool {
		b := fw.frames(i, i+1)
		for c := 0; c < format.Channels; c++ {
			if math.Abs(format.SampleAt(b, c)) > floor {
				return true
			}
		}
		return false
	}
	for i, quiet := start-1, int64(0); i >= 0 && start-i <= maxExtend && quiet < quietRun; i-- {
		if above(i) {
			start, quiet = i, 0
		} else {
			quiet++
		}
	}
	for i, quiet := end, int64(0); i < total && i-end <= maxExtend && quiet < quietRun; i++ {
		if above(i) {
			end, quiet = i+1, 0
		} else {
			quiet++
		}
	}
	return start, end, true, fw.err
}

// frameWindow keeps a few seconds of a WAV file around the frames asked
// for, loading ahead or behind as the scan moves, so trimming a long take
// does not load it whole.
type frameWindow struct {
	w    *pcm.WAVFile
	size int64
	from int64
	data []byte
	err  error
}

// frames returns frames [from, to) of the file, clamped to it.
func (fw *frameWindow) frames(from, to int64) []byte {
	from, to = max(from, 0), min(to, fw.w.Frames())
	if to <= from {
		return nil
	}
	fb := int64(fw.w.Format().FrameBytes())
	if from < fw.from || to > fw.from+int64(len(fw.data))/fb {
		lo := from
		if from < fw.from {
			// Moving back: the window ends where the scan is.
			lo = min(from, to-fw.size)
		}
		lo = max(lo, 0)
		data, err := fw.w.ReadFrames(lo, max(to, lo+fw.size))
		if err != nil {
			if fw.err == nil {
				fw.err = err
			}
			lo, data = from, make([]byte, (to-from)*fb)
		}
		fw.from, fw.data = lo, data
	}
	return fw.data[(from-fw.from)*fb : (to-fw.from)*fb]
}

// recordIndex remembers which artist and title went to which file, so a
// duplicate is found even when tags moved it to another folder.
type recordIndex struct {
	mu   sync.Mutex
	path string
	m    map[string]string
}

func openIndex(path string) *recordIndex {
	idx := &recordIndex{path: path, m: map[string]string{}}
	f, err := os.Open(path)
	if err != nil {
		return idx
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e struct{ Key, Path string }
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			idx.m[e.Key] = e.Path
		}
	}
	return idx
}

func (i *recordIndex) has(key string) bool {
	i.mu.Lock()
	p, ok := i.m[key]
	i.mu.Unlock()
	if !ok {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func (i *recordIndex) add(key, path string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.m[key] = path
	if err := os.MkdirAll(filepath.Dir(i.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(i.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(struct{ Key, Path string }{key, path})
	f.Write(append(b, '\n'))
}
