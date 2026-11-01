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
// the output folder. The session asks it whether a track is recorded
// before it records it, and the finisher whether a name is taken as it
// names a file.
type records struct {
	// mu guards claims: the session reads them while the finisher's jobs
	// run.
	mu sync.Mutex
	// claims are the tracks whose jobs were handed to the finisher and are
	// not done: their files are not there yet.
	claims map[string]bool
	index  *recordIndex
}

func openRecords(root string) *records {
	return &records{
		claims: map[string]bool{},
		index:  openIndex(filepath.Join(root, ".spytify", "recorded.jsonl")),
	}
}

// claim marks a track as being saved, from the moment its job is handed
// over, so the same track played again meanwhile is a duplicate.
func (r *records) claim(info meta.TrackInfo) {
	r.mu.Lock()
	r.claims[trackKey(info.Artist(), info.Title)] = true
	r.mu.Unlock()
}

// release ends a claim once its job is done, whatever became of it.
func (r *records) release(info meta.TrackInfo) {
	r.mu.Lock()
	delete(r.claims, trackKey(info.Artist(), info.Title))
	r.mu.Unlock()
}

// recorded reports whether a track is already saved or being saved: a job
// claimed it, the index has its file, or a file is where the settings put
// it, whatever its prefix.
func (r *records) recorded(info meta.TrackInfo, v settings.Values) bool {
	key := trackKey(info.Artist(), info.Title)
	r.mu.Lock()
	claimed := r.claims[key]
	r.mu.Unlock()
	if claimed || r.index.has(key) {
		return true
	}
	o := namingOptions(v)
	local := tag.FromLocal(info, tagOptions(v))
	return naming.Recorded(v.Path(settings.OutputDir), naming.Build(namingTrack(local, info), o), o)
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
