package meta

import (
	"sync/atomic"
	"time"
)

// restartWindow is how long a restart Spytify asked for waits for the
// player to make it: past it, the player went back late or not at all,
// and a jump back is judged as any other.
const restartWindow = 5 * time.Second

// restartBackMs is how far back the position must fall to be the restart
// Spytify asked for: further than a position read a poll late.
const restartBackMs = 250

// restartAsk is a restart Spytify asked of the player, until the player
// makes it or restartWindow ends. Asked, a jump back to the start counts
// however short, as from a track barely begun, which the seek slack hides
// otherwise. Only a playing player's counts: a paused one the session
// starts over itself, nothing playing until the player is back.
type restartAsk struct{ by atomic.Int64 }

func (a *restartAsk) arm(now time.Time) { a.by.Store(now.Add(restartWindow).UnixNano()) }

func (a *restartAsk) disarm() { a.by.Store(0) }

// made tells whether pos, at t, is the restart asked for: back under
// restartToMs from expected, where the player was, by more than
// restartBackMs. It disarms the ask when so. An unknown expected is -1.
func (a *restartAsk) made(t time.Time, pos, expected int64) bool {
	if a == nil {
		return false
	}
	by := a.by.Load()
	if by == 0 || t.UnixNano() >= by || pos < 0 || pos >= restartToMs || expected-pos <= restartBackMs {
		return false
	}
	a.disarm()
	return true
}
