package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/tag"
)

// tags is a file's title and artists, as its tags say once they are read,
// or for WAV, which has no tags, the .json beside it. Until then, and for a
// file without them, they are its name's, "Artist - Title" as the naming
// template writes it by default.
func (l *libraryScreen) tags(f libraryFile) (title, artist string) {
	path := f.path
	if t := l.tagged.get(f.key(), func(ctx context.Context) trackTags { return l.readTags(ctx, path) }); t.title != "" {
		return t.title, t.artist
	}
	name := strings.TrimSuffix(filepath.Base(f.rel), filepath.Ext(f.rel))
	if artist, title, ok := tag.SplitArtistTitle(name); ok {
		return title, artist
	}
	return name, ""
}

func (l *libraryScreen) readTags(ctx context.Context, path string) trackTags {
	if strings.EqualFold(filepath.Ext(path), ".wav") {
		var side tag.Track
		if b, err := os.ReadFile(path + ".json"); err == nil && json.Unmarshal(b, &side) == nil {
			artist := strings.Join(side.Performers, ", ")
			return trackTags{title: side.Title, artist: artist, album: side.Album, albumArtist: cmp.Or(strings.Join(side.AlbumArtists, ", "), artist)}
		}
		return trackTags{}
	}
	ff := l.ffmpeg()
	if ff == nil {
		return trackTags{}
	}
	tags, err := ff.Tags(ctx, path)
	if err != nil {
		return trackTags{}
	}
	return trackTags{title: tags["title"], artist: tags["artist"], album: tags["album"], albumArtist: cmp.Or(tags["album_artist"], tags["artist"])}
}

// trackTags is a file's title and artists, and its album and the album's
// artist, its own artists when the album has none; none for a file without
// tags.
type trackTags struct{ title, artist, album, albumArtist string }

// tagCache reads each file's tags once, off the toolkit's goroutine and two
// at a time: a library holds thousands of files, and only the rows in view
// ask for theirs.
type tagCache struct {
	mu      sync.Mutex
	read    map[string]trackTags
	pending map[string]bool
	loaded  func()
	slots   chan struct{}
}

// get returns the tags read under key, or starts read and returns none until
// it is done; loaded runs then, so the screen can draw them.
func (c *tagCache) get(key string, read func(context.Context) trackTags) trackTags {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.read[key]; ok {
		return t
	}
	if !c.pending[key] {
		c.pending[key] = true
		go func() {
			c.slots <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			t := read(ctx)
			cancel()
			<-c.slots
			c.mu.Lock()
			delete(c.pending, key)
			c.read[key] = t
			c.mu.Unlock()
			if c.loaded != nil {
				c.loaded()
			}
		}()
	}
	return trackTags{}
}

// known returns the tags read under key, and whether they are; it starts no
// read.
func (c *tagCache) known(key string) (trackTags, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.read[key]
	return t, ok
}
