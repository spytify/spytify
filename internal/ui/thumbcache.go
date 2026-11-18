package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// The Library's thumbnails on disk are kept while used in the last
// thumbAge, and the newest thumbBytes of them.
const (
	thumbAge   = 30 * 24 * time.Hour
	thumbBytes = 50 << 20
)

// thumbCache keeps the Library's row thumbnails on disk, outside the output
// folder, so ffmpeg reads a file's cover once for good: not again at every
// launch, nor for a row scrolled back to past the hundred kept in memory
// (#843). A file with no cover leaves an empty thumbnail, so ffmpeg does
// not look again. Each is written whole then renamed, and none stays open.
// With no folder it keeps nothing: the memory alone holds the covers.
type thumbCache struct{ dir string }

// DefaultCoverCache is where the Library keeps its thumbnails: the user's
// cache folder, never synced, which the system may empty. "" keeps none.
func DefaultCoverCache() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "spytify", "library-covers")
}

// path is the thumbnail of key, a libraryFile's: a new size or time, a
// file written again, makes another.
func (c thumbCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:16])+".png")
}

// read returns the thumbnail kept for key, empty for a file with no cover;
// ok is false when none is kept or it cannot be read, a picture that does
// not decode being deleted so the cover is read again. A thumbnail read is
// marked used, for prune.
func (c thumbCache) read(key string) (data []byte, ok bool) {
	if c.dir == "" {
		return nil, false
	}
	p := c.path(key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	if len(data) > 0 {
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			_ = os.Remove(p)
			return nil, false
		}
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now)
	return data, true
}

// write keeps data, a thumbnail's PNG or nothing for a file with no cover,
// under key. A folder that cannot be written keeps nothing.
func (c thumbCache) write(key string, data []byte) {
	if c.dir == "" || os.MkdirAll(c.dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dir, ".thumb-*")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), c.path(key))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
}

// prune deletes the thumbnails not used in thumbAge, then the oldest while
// those left weigh more than thumbBytes.
func (c thumbCache) prune(now time.Time) {
	if c.dir == "" {
		return
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type kept struct {
		path string
		used time.Time
		size int64
	}
	var left []kept
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		p := filepath.Join(c.dir, e.Name())
		if now.Sub(info.ModTime()) > thumbAge {
			_ = os.Remove(p)
			continue
		}
		left = append(left, kept{p, info.ModTime(), info.Size()})
		total += info.Size()
	}
	slices.SortFunc(left, func(a, b kept) int { return a.used.Compare(b.used) })
	for _, k := range left {
		if total <= thumbBytes {
			break
		}
		if os.Remove(k.path) == nil {
			total -= k.size
		}
	}
}

// rowPNG is the row thumbnail of a cover as a file holds it, as a PNG:
// newRowThumbs makes the same picture from either.
func rowPNG(data []byte) ([]byte, error) {
	t, err := newRowThumbs(data)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, t.row); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
