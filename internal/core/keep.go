package core

import (
	"os"
	"sync"

	"github.com/spytify/spytify/pkg/proto"
)

// keeper holds the audio of the last take cut short, stopped or moved along
// its track bar, so that whoever saw it skipped can save it after all with
// track.keep. It lasts until the next track is saved, from one session to
// the next.
type keeper struct {
	emit func(method string, payload any)

	mu   sync.Mutex
	take *finishJob
}

// hold keeps j's audio in place of the take held before; it tells whether
// it did. A nil keeper holds nothing.
func (k *keeper) hold(j finishJob) bool {
	if k == nil || j.played <= 0 {
		return false
	}
	k.mu.Lock()
	old := k.take
	k.take = &j
	k.mu.Unlock()
	k.release(old)
	k.emit(proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: j.id, Keepable: true})
	return true
}

// drop lets the held take go.
func (k *keeper) drop() {
	if k == nil {
		return
	}
	k.mu.Lock()
	old := k.take
	k.take = nil
	k.mu.Unlock()
	k.release(old)
}

func (k *keeper) release(j *finishJob) {
	if j == nil {
		return
	}
	_ = os.Remove(j.tmp)
	k.emit(proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: j.id})
}

// claim hands the held take over to be saved: the one named id, or the one
// there is when id is empty.
func (k *keeper) claim(id string) (finishJob, bool) {
	k.mu.Lock()
	j := k.take
	if j == nil || (id != "" && j.id != id) {
		k.mu.Unlock()
		return finishJob{}, false
	}
	k.take = nil
	k.mu.Unlock()
	k.emit(proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: j.id})
	return *j, true
}
