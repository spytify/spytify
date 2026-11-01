package core

import (
	"context"
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
	// rejected is why the take was skipped, when the session already did
	// or once it is held: the file is not made, but the audio is held for
	// track.keep.
	rejected proto.SkipReason
	// keep saves a held take after all, whatever part of it played.
	keep bool
	// suffix ends a kept take's title tag and file name: " (partial)" or
	// " (low quality)".
	suffix string
	// cover is a held take's file cover, read while the file was there.
	cover     []byte
	coverMIME string
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

	mu  sync.Mutex
	cnt proto.Counters
	// records tells whether a track is recorded, and keeps the claims of
	// the jobs handed over and the index of the files saved.
	records *records
	// advance stores the counter's next number once a file took one; full
	// stops the recording when the counter has no number left, and lost
	// when the output folder cannot be reached.
	advance func(next int64)
	full    func()
	lost    func()
	// keep holds the last take cut short or in low quality, for
	// track.keep.
	keep *keeper
}

func newFinisher(values func() settings.Values, ff *encode.FFmpeg, emit func(string, any), log *Logger) *finisher {
	v := values()
	f := &finisher{
		values:  values,
		ffmpeg:  ff,
		emit:    emit,
		log:     log,
		jobs:    make(chan finishJob, 256),
		records: openRecords(v.Path(settings.OutputDir)),
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
		f.records.claim(j.info)
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

// recorded reports whether a track is already saved or being saved: the
// session asks before it records one.
func (f *finisher) recorded(info meta.TrackInfo, v settings.Values) bool {
	return f.records.recorded(info, v)
}

func namingOptions(v settings.Values) naming.Options {
	p, _ := encode.PresetFor(v.String(settings.EncodeFormat))
	return naming.Options{
		GroupBy:         v.String(settings.OutputGroupBy),
		Prefix:          v.String(settings.OutputPrefix),
		Underscores:     v.Bool(settings.OutputUnderscores),
		Template:        v.String(settings.OutputTemplate),
		CounterWidth:    int(v.Int(settings.OutputCounterWidth)),
		ASCII:           v.Bool(settings.OutputAsciiFilenames),
		ArtistSeparator: v.String(settings.TagsArtistSeparator),
		Extension:       p.Extension,
	}
}

func tagOptions(v settings.Values) tag.Options {
	return tag.Options{
		ExtraToSubtitle: v.Bool(settings.TagsExtraToSubtitle),
		ArtistSeparator: v.String(settings.TagsArtistSeparator),
		CoverSize:       v.String(settings.TagsCoverSize),
		Cover:           v.Bool(settings.TagsCover),
	}
}

func namingTrack(t tag.Track, info meta.TrackInfo) naming.Track {
	// The subtitle option moves the extra title in the tags only: the file
	// name puts it back the way the title had it.
	sep := t.SubtitleSep
	if sep == tag.SepNone {
		sep = tag.SepDash
	}
	title := tag.JoinTitle(t.Title, t.Subtitle, sep)
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

// take is a job on its way to its file: what a step of process finds, for
// the steps after it.
type take struct {
	finishJob
	v       settings.Values
	o       naming.Options
	topts   tag.Options
	began   time.Time
	timings *proto.FinishTimings
	// held is set once the audio is held for track.keep, which then owns
	// the temp WAV.
	held bool

	trimStart, trimEnd int64
	keptFrames         int64
	tags               tag.Track
	counter            int64
	final, part        string
	tagged             bool
}

// process makes j's file, one step after the other. A step that reports
// false ended the job: it skipped, held or failed the take.
func (f *finisher) process(j finishJob) {
	defer f.wg.Done()
	defer func() {
		if j.partial || j.rejected != "" {
			return
		}
		f.records.release(j.info)
	}()
	t := &take{finishJob: j}
	defer func() {
		if !t.held {
			os.Remove(j.tmp)
		}
	}()

	t.began, t.v = time.Now(), f.values()
	t.o, t.topts = namingOptions(t.v), tagOptions(t.v)
	t.timings = &proto.FinishTimings{Format: t.v.String(settings.EncodeFormat)}
	if !f.complete(t) || !f.trim(t) || !f.goodQuality(t) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f.fetchCover(ctx, t)
	if !f.takeNumber(t) || !f.pickName(t) || !f.encode(ctx, t) {
		return
	}
	f.writeTags(ctx, t)
	f.save(t)
}

func msSince(t time.Time) int64 { return time.Since(t).Milliseconds() }

// tooShort reports whether frames of an item with no declared length are
// too few to be taken for a whole track.
func (t *take) tooShort(frames int64) bool {
	return t.length == 0 && !t.keep && t.format.MsFromFrames(frames) < minUnknownLengthMs
}

// complete reports whether the take is to be saved. One the session
// rejected is held for track.keep, one cut short is skipped and held, and
// one too short is skipped.
func (f *finisher) complete(t *take) bool {
	playedMs := t.format.MsFromFrames(t.played)
	if t.rejected != "" {
		t.held = f.keep.hold(t.finishJob, t.rejected)
		return false
	}
	if !t.keep && (t.partial || (t.length > 0 && t.played < t.length-t.format.FramesFromMs(completeToleranceMs))) {
		f.log.Infof("%q: only %d of %d ms played, not kept", t.info.Title, playedMs, t.format.MsFromFrames(t.length))
		f.skip(t.id, proto.SkipIncomplete)
		t.held = f.keep.hold(t.finishJob, proto.SkipIncomplete)
		return false
	}
	if t.tooShort(t.played) {
		f.log.Infof("%q: %d ms of an item with no length, under %d s", t.info.Title, playedMs, minUnknownLengthMs/1000)
		f.skip(t.id, proto.SkipTooShort)
		return false
	}
	return true
}

// trim finds where the sound starts and ends, when recorder.trim_silence is
// on. What is left may be too short to keep.
func (f *finisher) trim(t *take) bool {
	t.keptFrames = t.played
	if !t.v.Bool(settings.RecorderTrimSilence) {
		return true
	}
	step := time.Now()
	s, e, ok, err := scanTrim(t.tmp, t.v.Float(settings.RecorderSilenceDb))
	t.timings.TrimMs = msSince(step)
	if err != nil {
		f.fail(t.finishJob, "read", err)
		return false
	}
	if !ok {
		f.skip(t.id, proto.SkipTooShort)
		return false
	}
	t.trimStart, t.trimEnd, t.keptFrames = s, e, e-s
	if t.tooShort(t.keptFrames) {
		f.skip(t.id, proto.SkipTooShort)
		return false
	}
	return true
}

// goodQuality skips a take recorded too poorly for music, and holds it for
// track.keep. Low quality skips a take that nothing before skipped, before
// it takes a cover, a counter number or a name.
func (f *finisher) goodQuality(t *take) bool {
	if !t.keep && poorFormat(t.format) {
		f.log.Infof("%q: recorded at %s, too poor for music, not kept", t.info.Title, t.format)
		f.skip(t.id, proto.SkipLowQuality)
		t.held = f.keep.hold(t.finishJob, proto.SkipLowQuality)
		return false
	}
	return true
}

// fetchCover makes the take's tags, with the cover when tags.cover is on. A
// cover that cannot be had is left out.
func (f *finisher) fetchCover(ctx context.Context, t *take) {
	t.tags = tag.FromLocal(t.info, t.topts)
	if !t.topts.Cover || t.tags.CoverURL == "" {
		return
	}
	step := time.Now()
	data, mime := t.cover, t.coverMIME
	var err error
	if data == nil {
		data, mime, err = tag.FetchCover(ctx, nil, t.tags.CoverURL)
	}
	t.timings.CoverMs = msSince(step)
	if err == nil {
		t.tags.Cover, t.tags.CoverMIME = data, mime
	} else {
		f.log.Infof("%q: cover not embedded: %v", t.info.Title, err)
	}
}

// takeNumber gives the take the counter's next number, when file names
// start with it. The counter numbers the saved files in the order they
// play, from one recording to the next: it is a setting, which each file
// moves on. Past the last number its digits allow, the file is not saved
// and the recording stops, as v1's did.
func (f *finisher) takeNumber(t *take) bool {
	if t.o.Prefix != "counter" {
		return true
	}
	t.counter = t.v.Int(settings.OutputCounterNext)
	if limit := naming.CounterLimit(t.o.CounterWidth); t.counter > limit {
		f.fail(t.finishJob, "counter_full", fmt.Errorf("the counter is past %d, the last number of %d digits: raise Counter digits or set Next counter number back", limit, t.o.CounterWidth))
		if f.full != nil {
			f.full()
		}
		return false
	}
	return true
}

// pickName makes the file's folder and picks its name. A file already
// there takes a suffix, skips the take or is replaced, as
// recorder.duplicates says.
func (f *finisher) pickName(t *take) bool {
	// A kept take's suffix ends its file name, wherever the template puts
	// the title, and its title tag.
	nt := namingTrack(t.tags, t.info)
	nt.Suffix = t.suffix
	plan := naming.Build(nt, t.o)
	t.tags.Title += t.suffix
	root := t.v.Path(settings.OutputDir)
	dir := filepath.Join(root, plan.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.fail(t.finishJob, "output_dir", err)
		return false
	}
	name := naming.FileName(plan, t.o, t.counter, t.tags.Track)
	duplicates := t.v.String(settings.RecorderDuplicates)
	if t.keep {
		// A partial file never replaces a file, nor is it skipped.
		duplicates = "suffix"
	}
	switch duplicates {
	case "suffix":
		name = naming.FreeSuffix(dir, name, t.o.Underscores, f.records.taken)
	case "skip":
		if f.records.taken(filepath.Join(dir, name)) {
			f.skip(t.id, proto.SkipDuplicate)
			return false
		}
	}
	t.final = filepath.Join(dir, name)
	t.part = t.final + ".part"
	return true
}

// encode writes the take to the .part file in the output format, trimmed
// and normalized as set.
func (f *finisher) encode(ctx context.Context, t *take) bool {
	step := time.Now()
	short, err := f.ffmpeg.Encode(ctx, encode.Job{
		In:          t.tmp,
		Out:         t.part,
		Format:      t.timings.Format,
		BitrateKbps: int(t.v.Int(settings.EncodeBitrateKbps)),
		Source:      t.format,
		TrimStart:   t.trimStart,
		TrimEnd:     t.trimEnd,
		Normalize:   t.v.Bool(settings.RecorderNormalize),
		TargetLUFS:  t.v.Float(settings.RecorderTargetLufs),
	})
	t.timings.EncodeMs = msSince(step)
	if err != nil {
		os.Remove(t.part)
		f.fail(t.finishJob, "encode", err)
		return false
	}
	if short != nil {
		// A gain that shows as +0.0 dB left the track at its level.
		if math.Round(short.GainDB*10) > 0 {
			f.log.Infof("%q: normalized %+.1f dB; its peaks kept it %.1f dB under %g LUFS", t.info.Title, short.GainDB, short.Under(), short.TargetLUFS)
		} else {
			f.log.Infof("%q: left at its level; its peaks already reach %g dBTP, %.1f dB under %g LUFS", t.info.Title, encode.TruePeak, short.Under(), short.TargetLUFS)
		}
	}
	return true
}

// writeTags tags the .part file. A file whose tags could not be written is
// saved all the same.
func (f *finisher) writeTags(ctx context.Context, t *take) {
	t.tagged = true
	step := time.Now()
	err := (tag.Writer{FFmpeg: f.ffmpeg.Path}).Write(ctx, t.part, t.timings.Format, t.tags, t.topts)
	t.timings.TagMs = msSince(step)
	if err != nil {
		f.log.Errorf("%q: tags not written: %v", t.info.Title, err)
		t.tagged = false
	}
}

// save renames the .part file, and a WAV's sidecar, to the final name, then
// counts the file and reports it saved.
func (f *finisher) save(t *take) {
	if t.timings.Format == "wav" {
		if err := os.Rename(t.part+".json", t.final+".json"); err != nil && !os.IsNotExist(err) {
			f.log.Errorf("%q: sidecar: %v", t.info.Title, err)
		}
	}
	if err := os.Rename(t.part, t.final); err != nil {
		os.Remove(t.part)
		f.fail(t.finishJob, "rename", err)
		return
	}
	var size int64
	if st, err := os.Stat(t.final); err == nil {
		size = st.Size()
	}
	f.mu.Lock()
	f.cnt.Recorded++
	f.cnt.Bytes += size
	f.mu.Unlock()
	if t.o.Prefix == "counter" && f.advance != nil {
		f.advance(t.counter + 1)
	}
	if !t.keep {
		// A partial file does not make the whole track a duplicate.
		f.records.saved(t.info, t.final)
		// The take held for track.keep goes once the next track is saved.
		f.keep.drop()
	}
	t.timings.TotalMs = msSince(t.began)
	f.log.Infof("saved %s", t.final)
	f.emit(proto.EventTrackFinished, proto.TrackFinishedEvent{
		ID:         t.id,
		Counter:    t.counter,
		Path:       t.final,
		DurationMs: t.format.MsFromFrames(t.keptFrames),
		Bytes:      size,
		Tagged:     t.tagged,
		TaggedBy:   t.tags.Source,
		Timings:    t.timings,
		Partial:    t.keep,
	})
}

func (f *finisher) fail(j finishJob, code string, err error) {
	f.log.Errorf("%q: %s: %v", j.info.Title, code, err)
	msg := fmt.Sprintf("%s: %v", j.info.Title, err)
	// Only once a file failed is the output folder looked at: gone with
	// its drive, nothing more can be saved, and the recording stops.
	lost := false
	if _, gone := os.Stat(f.values().Path(settings.OutputDir)); gone != nil {
		code, msg, lost = "output_dir", "the output folder cannot be reached, so the recording stopped: "+msg, true
	}
	f.emit(proto.EventError, proto.ErrorEvent{Code: code, Msg: msg})
	f.skip(j.id, proto.SkipIncomplete)
	if lost && f.lost != nil {
		f.lost()
	}
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
