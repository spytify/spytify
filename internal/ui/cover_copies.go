package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/hidden"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/internal/ui/model"
)

// coverFolder is the hidden folder of the output folder that holds the
// Record list's covers.
const coverFolder = ".covers"

// coverCopies keeps a copy of the cover the player showed for each track of
// the Record list, in the output folder's .covers, named after the track's
// id: on Windows the player's cover is a file deleted at the next track, and
// a row the list draws again, scrolled back to or after another tab, reads
// its cover again. The list shows what the player sent, so saved tracks read
// their copy too, not their file. Copies of tracks the list no longer holds
// are deleted.
type coverCopies struct {
	mu sync.Mutex
	// paths holds each track tried: its copy, or "" when none was made.
	paths   map[string]string
	pending map[string]bool
	// found holds each track looked for on disk and not tried: the copy an
	// earlier window made, or "" for none. looking holds those being
	// looked for.
	found   map[string]string
	looking map[string]bool
	loaded  func()
}

func newCoverCopies(loaded func()) *coverCopies {
	return &coverCopies{paths: map[string]string{}, pending: map[string]bool{}, found: map[string]string{}, looking: map[string]bool{}, loaded: loaded}
}

// path is the copy of the cover of track id in dir, "" while there is none.
// A copy made by an earlier window, for a list it replayed, is looked for
// on disk once, off the toolkit's goroutine: rows are drawn on it at every
// event, and the output folder can be on a slow drive (#842). loaded runs
// once a copy is found.
func (c *coverCopies) path(dir, id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.paths[id]; ok || dir == "" {
		return p
	}
	if p, ok := c.found[id]; ok || c.looking[id] {
		return p
	}
	c.looking[id] = true
	go func() {
		p := onDisk(dir, id)
		c.mu.Lock()
		delete(c.looking, id)
		c.found[id] = p
		c.mu.Unlock()
		if p != "" && c.loaded != nil {
			c.loaded()
		}
	}()
	return ""
}

// onDisk is the copy of the cover of track id in dir, "" when there is none.
func onDisk(dir, id string) string {
	for _, ext := range []string{".jpg", ".png"} {
		p := filepath.Join(dir, coverFolder, copyName(id)+ext)
		if _, err := statCopy(p); err == nil {
			return p
		}
	}
	return ""
}

// keep copies the cover at url of track id into dir once, off the toolkit's
// goroutine, unless an earlier window made the copy.
func (c *coverCopies) keep(dir, id, url string) {
	if dir == "" || id == "" || url == "" {
		return
	}
	c.mu.Lock()
	_, tried := c.paths[id]
	if tried || c.pending[id] || c.found[id] != "" {
		c.mu.Unlock()
		return
	}
	c.pending[id] = true
	c.mu.Unlock()
	go func() {
		if p := onDisk(dir, id); p != "" {
			c.mu.Lock()
			delete(c.pending, id)
			c.paths[id] = p
			c.mu.Unlock()
			if c.loaded != nil {
				c.loaded()
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		data, mime, err := tag.FetchCover(ctx, nil, url)
		cancel()
		p := ""
		if err == nil {
			ext := ".jpg"
			if mime == "image/png" {
				ext = ".png"
			}
			folder := filepath.Join(dir, coverFolder)
			p = filepath.Join(folder, copyName(id)+ext)
			if hidden.MkdirAll(folder) != nil || os.WriteFile(p, data, 0o644) != nil {
				p = ""
			}
		}
		c.mu.Lock()
		delete(c.pending, id)
		c.paths[id] = p
		c.mu.Unlock()
		if p != "" && c.loaded != nil {
			c.loaded()
		}
	}()
}

// prune deletes, off the toolkit's goroutine, the copies in dir of tracks
// not in held.
func (c *coverCopies) prune(dir string, held map[string]bool) {
	c.mu.Lock()
	for id := range c.paths {
		if !held[id] && !c.pending[id] {
			delete(c.paths, id)
		}
	}
	for id := range c.found {
		if !held[id] {
			delete(c.found, id)
		}
	}
	c.mu.Unlock()
	if dir == "" {
		return
	}
	names := make(map[string]bool, len(held))
	for id := range held {
		names[copyName(id)] = true
	}
	go func() {
		folder := filepath.Join(dir, coverFolder)
		entries, err := os.ReadDir(folder)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			c.mu.Lock()
			keep := names[name] || c.pending[name]
			c.mu.Unlock()
			if !keep && !e.IsDir() {
				_ = os.Remove(filepath.Join(folder, e.Name()))
			}
		}
	}()
}

// statCopy is os.Stat; tests replace it.
var statCopy = os.Stat

// copyName is a track id fit for a file name.
func copyName(id string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, id)
}

// keepCovers copies the playing track's cover whatever the tab shown, as
// only the shown tab draws, and deletes the copies of tracks the Record list
// let go: a new session empties it, so does Clear list, and a window opening
// holds only what it replayed. It waits for the replay, which comes before
// the link says connected.
func (a *App) keepCovers(s model.State) {
	dir := coverDir(s)
	if s.Conn != model.Connected || dir == "" {
		return
	}
	held := make(map[string]bool, len(s.Rows)+1)
	if c := s.Current; c != nil {
		held[c.ID] = true
		a.covers.copies.keep(dir, c.ID, c.CoverURL)
	}
	for _, r := range s.Rows {
		held[r.ID] = true
	}
	letGo := a.coverIDs == nil
	for id := range a.coverIDs {
		if !held[id] {
			letGo = true
			break
		}
	}
	a.coverIDs = held
	if letGo {
		a.covers.copies.prune(dir, held)
	}
}

// rowCover is the cover the player showed for row: its copy once made, the
// player's own until then.
func (a *App) rowCover(row model.Row) thumbs {
	if p := a.covers.copies.path(coverDir(a.last), row.ID); p != "" {
		return a.covers.load(p, func(context.Context) ([]byte, error) { return os.ReadFile(p) })
	}
	return a.covers.get(row.CoverURL)
}

// coverDir is the output folder, whose .covers holds the copies.
func coverDir(s model.State) string {
	dir, _ := s.Settings[settings.OutputDir.String()].(string)
	return settings.ExpandHome(dir)
}
