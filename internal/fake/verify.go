package fake

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

// The plan's acceptance rule for a recorded track: after trimming both to
// their first and last sample above -60 dBFS, the cross-correlation peak is
// within 2 ms of zero and the residual is under -50 dBFS.
const (
	TrimDB     = -60
	SearchMs   = 10
	MaxLagMs   = 2
	MaxErrorDB = -50
)

// Verify checks a run against its manifest: every item expected saved has a
// WAV file whose audio is the item and nothing else, every item expected
// skipped was skipped for the expected reason, and the output folder holds
// exactly files audio files (pass -1 to count the saved items).
func Verify(tl *Timeline, expect []Expect, files int, outDir string, events []proto.Event) []string {
	var problems []string
	type seg struct {
		track    proto.Track
		finished *proto.TrackFinishedEvent
		skipped  proto.SkipReason
	}
	var order []string
	segs := map[string]*seg{}
	for _, ev := range events {
		switch ev.Method {
		case proto.EventTrackStarted:
			var t proto.TrackStartedEvent
			if ev.Decode(&t) == nil {
				segs[t.ID] = &seg{track: t.Track}
				order = append(order, t.ID)
			}
		case proto.EventTrackFinished:
			var f proto.TrackFinishedEvent
			if ev.Decode(&f) == nil && segs[f.ID] != nil {
				segs[f.ID].finished = &f
			}
		case proto.EventTrackSkipped:
			var s proto.TrackSkippedEvent
			if ev.Decode(&s) == nil && segs[s.ID] != nil {
				segs[s.ID].skipped = s.Reason
			}
		}
	}

	// Segments are announced in play order; match them to items by title.
	byItem := map[int]*seg{}
	cursor := 0
	for _, id := range order {
		s := segs[id]
		for i := cursor; i < len(tl.Items); i++ {
			if tl.Items[i].Title == s.track.Title {
				byItem[i] = s
				cursor = i + 1
				break
			}
		}
	}

	// With recorder.duplicates = overwrite a later item replaces an earlier
	// item's file; only the last writer of a path is compared.
	lastWriter := map[string]int{}
	for _, e := range expect {
		if s := byItem[e.Item]; e.Saved && s != nil && s.finished != nil {
			lastWriter[s.finished.Path] = e.Item
		}
	}

	saved := 0
	for _, e := range expect {
		it := tl.Items[e.Item]
		s := byItem[e.Item]
		label := fmt.Sprintf("item %d %q", e.Item, it.Title)
		if s == nil {
			problems = append(problems, label+": never started")
			continue
		}
		if !e.Saved {
			if s.finished != nil {
				problems = append(problems, fmt.Sprintf("%s: saved to %s, expected skipped (%s)", label, s.finished.Path, e.Reason))
			} else if string(s.skipped) != e.Reason {
				problems = append(problems, fmt.Sprintf("%s: skipped for %q, expected %q", label, s.skipped, e.Reason))
			}
			continue
		}
		saved++
		if s.finished == nil {
			problems = append(problems, fmt.Sprintf("%s: not saved (skipped: %q)", label, s.skipped))
			continue
		}
		if lastWriter[s.finished.Path] != e.Item {
			continue
		}
		gotF, got, err := pcm.ReadWAV(s.finished.Path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		wantF, want, err := tl.ItemAudio(e.Item)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		if gotF.Rate != wantF.Rate || gotF.Channels != wantF.Channels {
			problems = append(problems, fmt.Sprintf("%s: file is %s, source is %s", label, gotF, wantF))
			continue
		}
		c := pcm.Compare(wantF, want, gotF, got, TrimDB, SearchMs)
		if !c.Matches(MaxLagMs, MaxErrorDB) {
			problems = append(problems, fmt.Sprintf("%s: %s does not match its source: lag %.2f ms, residual %.1f dBFS, %+d frames",
				label, filepath.Base(s.finished.Path), c.LagMs, c.ErrorDBFS, c.LengthDelta))
		}
	}

	if files < 0 {
		files = saved
	}
	found := AudioFiles(outDir)
	if len(found) != files {
		problems = append(problems, fmt.Sprintf("output holds %d audio files, expected %d: %v", len(found), files, found))
	}
	return problems
}

// AudioFiles lists the audio files a run left, ignoring the temp folder,
// sidecars and partial files.
func AudioFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".spytify" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav", ".flac", ".mp3", ".m4a":
			rel, _ := filepath.Rel(root, path)
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}
