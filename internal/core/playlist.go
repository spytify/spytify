package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// playlistDir is the output folder's folder of session playlists.
const playlistDir = "Playlists"

// playlist writes a session's tracks to an .m3u8 in the output folder's
// Playlists, in the order they played (output.playlist). It follows the
// core's events: a track takes its place as it starts, and goes in once
// its file is known, the one saved, the one a track already recorded has,
// or the one track.keep saves after all. The file is written again after
// each, so a quit or a crash keeps what is done. Every session has one,
// written only while on says so: turned on mid-session, it has the tracks
// saved before too.
type playlist struct {
	out   func() string
	start time.Time
	on    func() bool
	log   *Logger

	mu sync.Mutex
	// root is the output folder as the file is named; tracks saved in
	// another after a change are listed by their full path.
	root   string
	path   string
	tracks []*listed
	byID   map[string]*listed
}

// listed is a track of the playlist: its file once known, or the take of
// this session whose file it waits for. out is a track skipped, which
// track.keep can still save.
type listed struct {
	id   string
	name string
	ms   int64
	path string
	of   string
	out  bool
}

func newPlaylist(out func() string, start time.Time, on func() bool, log *Logger) *playlist {
	return &playlist{out: out, start: start, on: on, log: log, byID: map[string]*listed{}}
}

// observe takes in an event the core sends.
func (p *playlist) observe(payload any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch e := payload.(type) {
	case proto.TrackStartedEvent:
		if p.byID[e.ID] != nil {
			return
		}
		name := e.Title
		if e.Artist != "" {
			name = e.Artist + " - " + e.Title
		}
		l := &listed{id: e.ID, name: name, ms: e.LengthMs}
		p.tracks = append(p.tracks, l)
		p.byID[e.ID] = l
	case proto.TrackFinishedEvent:
		l := p.byID[e.ID]
		if l == nil {
			return
		}
		l.path, l.of, l.out = e.Path, "", false
		if e.DurationMs > 0 {
			l.ms = e.DurationMs
		}
		// The tracks played again while this one was being saved have its
		// file now.
		for _, w := range p.tracks {
			if w.of == e.ID {
				w.path, w.of = e.Path, ""
			}
		}
		p.write()
	case proto.TrackSkippedEvent:
		l := p.byID[e.ID]
		if l == nil {
			return
		}
		if e.Reason != proto.SkipDuplicate {
			l.out = true
			return
		}
		l.path, l.of = e.Path, e.Of
		if o := p.byID[e.Of]; o != nil && o.path != "" {
			l.path, l.of = o.path, ""
		}
		if l.path != "" {
			p.write()
		}
	}
}

// name is the playlist's file as the session ends: the one written, or the
// one its tracks still being saved go to; "" when none of its tracks goes
// in, or when it is off. It is written once more: turned on after the last
// track was saved, it has not been yet.
func (p *playlist) name() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.on() {
		return ""
	}
	pending := false
	for _, l := range p.tracks {
		if l.path != "" {
			p.write()
			return p.path
		}
		pending = pending || !l.out
	}
	if pending {
		return p.file()
	}
	return ""
}

// file is where the playlist is written: the output folder's Playlists,
// named after the session's start, with " (2)" and up when another session
// of that minute wrote one. The folder and name are picked once.
func (p *playlist) file() string {
	if p.path != "" {
		return p.path
	}
	p.root = p.out()
	base := p.start.Format("2006-01-02 15.04")
	p.path = filepath.Join(p.root, playlistDir, base+".m3u8")
	for n := 2; exists(p.path); n++ {
		p.path = filepath.Join(p.root, playlistDir, fmt.Sprintf("%s (%d).m3u8", base, n))
	}
	return p.path
}

// write writes the tracks whose files are known, in the order they played,
// to a new file then put in place of the one before; nothing while off.
func (p *playlist) write() {
	if !p.on() {
		return
	}
	path := p.file()
	var b strings.Builder
	b.WriteString("#EXTM3U\n#PLAYLIST:" + strings.TrimSuffix(filepath.Base(path), ".m3u8") + "\n")
	for _, l := range p.tracks {
		if l.path == "" {
			continue
		}
		fmt.Fprintf(&b, "#EXTINF:%d,%s\n%s\n", (l.ms+500)/1000, oneLine(l.name), p.rel(l.path))
	}
	if err := writeAtomic(path, b.String()); err != nil {
		p.log.Errorf("playlist %s: %v", filepath.Base(path), err)
	}
}

// rel is a track's file from the playlist's folder, so the output folder
// can move whole; a file out of the output folder keeps its full path.
func (p *playlist) rel(path string) string {
	in, err := filepath.Rel(p.root, path)
	if err != nil || in == ".." || strings.HasPrefix(in, ".."+string(filepath.Separator)) {
		return path
	}
	r, err := filepath.Rel(filepath.Dir(p.path), path)
	if err != nil {
		return path
	}
	return r
}

// oneLine keeps a title on its #EXTINF line.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeAtomic writes data to a file beside path, then renames it to path,
// so a reader never finds half a file.
func writeAtomic(path, data string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".playlist-*")
	if err != nil {
		return err
	}
	// A temp file is the user's alone; the playlist is read as the
	// recordings are.
	_ = f.Chmod(0o644)
	if _, err := f.WriteString(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}
