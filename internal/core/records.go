package core

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spytify/spytify/internal/core/naming"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
)

// records is what a finisher knows of the tracks already recorded: the
// takes it is saving, the index of the files it saved, and the files in
// the output folder. The session asks it whether a track is recorded, and
// where, before it records it, and the finisher whether a name is taken
// as it names a file.
type records struct {
	// mu guards claims: the session reads them while the finisher's jobs
	// run.
	mu sync.Mutex
	// claims are the tracks whose jobs were handed to the finisher and are
	// not done, by the ID of their take: their files are not there yet.
	claims map[string]string
	index  *recordIndex
}

// recordedFile is where a recorded track's file is: its path, or, while a
// job saves it, the ID of that job's take, whose track.finished names it.
type recordedFile struct {
	path string
	of   string
}

func openRecords(root string) *records {
	return &records{
		claims: map[string]string{},
		index:  openIndex(filepath.Join(root, ".spytify", "recorded.jsonl")),
	}
}

// claim marks a track as being saved by take id, from the moment its job
// is handed over, so the same track played again meanwhile is a duplicate.
func (r *records) claim(id string, info meta.TrackInfo) {
	r.mu.Lock()
	r.claims[trackKey(info.Artist(), info.Title)] = id
	r.mu.Unlock()
}

// release ends a claim once its job is done, whatever became of it.
func (r *records) release(info meta.TrackInfo) {
	r.mu.Lock()
	delete(r.claims, trackKey(info.Artist(), info.Title))
	r.mu.Unlock()
}

// find reports whether a track is already saved or being saved, and
// where: a job claimed it, the index has its file, or a file is where the
// settings put it, whatever its prefix.
func (r *records) find(info meta.TrackInfo, v settings.Values) (recordedFile, bool) {
	key := trackKey(info.Artist(), info.Title)
	r.mu.Lock()
	of, claimed := r.claims[key]
	r.mu.Unlock()
	if claimed {
		return recordedFile{of: of}, true
	}
	if path, ok := r.index.lookup(key); ok {
		return recordedFile{path: path}, true
	}
	o := namingOptions(v)
	local := tag.FromLocal(info, tagOptions(v))
	path, ok := naming.Recorded(v.Path(settings.OutputDir), naming.Build(namingTrack(local, info), o), o)
	return recordedFile{path: path}, ok
}

// taken reports whether a file is at path, the name the finisher picked
// for a take. It asks the file system, so a name in another case is taken
// where the file system ignores case.
func (r *records) taken(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// saved adds a track's file to the index.
func (r *records) saved(info meta.TrackInfo, path string) {
	r.index.add(trackKey(info.Artist(), info.Title), path)
}

func trackKey(artist, title string) string {
	return strings.ToLower(strings.TrimSpace(artist)) + "\x1f" + strings.ToLower(strings.TrimSpace(title))
}

// recordIndex remembers which artist and title went to which file, so a
// duplicate is found even when tags moved it to another folder. Lines are
// only added: one whose file is gone stays, as the file may come back, and
// another computer recording to the same folder may be adding to it.
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

// lookup is the file the index has for key, while it is there.
func (i *recordIndex) lookup(key string) (string, bool) {
	i.mu.Lock()
	p, ok := i.m[key]
	i.mu.Unlock()
	if !ok {
		return "", false
	}
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
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
