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
	loaded  func()
}

func newCoverCopies(loaded func()) *coverCopies {
	return &coverCopies{paths: map[string]string{}, pending: map[string]bool{}, loaded: loaded}
}

// path is the copy of the cover of track id in dir, "" while there is none.
// A copy made by an earlier window, for a list it replayed, is found on disk.
func (c *coverCopies) path(dir, id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.paths[id]; ok || dir == "" {
		return p
	}
	for _, ext := range []string{".jpg", ".png"} {
		p := filepath.Join(dir, coverFolder, copyName(id)+ext)
		if _, err := os.Stat(p); err == nil {
			c.paths[id] = p
			return p
		}
	}
	return ""
}

// keep copies the cover at url of track id into dir once, off the toolkit's
// goroutine.
func (c *coverCopies) keep(dir, id, url string) {
	if dir == "" || id == "" || url == "" || c.path(dir, id) != "" {
		return
	}
	c.mu.Lock()
	if _, tried := c.paths[id]; tried || c.pending[id] {
		c.mu.Unlock()
		return
	}
	c.pending[id] = true
	c.mu.Unlock()
	go func() {
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
