package backend

import (
	"slices"
	"sync"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
)

type hintKey struct {
	finder   int
	location int64
}

// hintTracker decides which hints are new. Every hint shows up in both the
// finder's and the receiver's list, so hints are keyed by finder and
// location. State survives reconnects to the same seed, so hints made while
// disconnected are still announced; a new seed or a fresh process starts
// silent and only announces hints made after the first snapshot.
type hintTracker struct {
	seed  string
	ready bool
	known map[hintKey]struct{}
}

func (t *hintTracker) start(seed string) {
	if t.known != nil && t.seed == seed {
		return
	}
	t.seed = seed
	t.ready = false
	t.known = make(map[hintKey]struct{})
}

// snapshot records the full hint lists from Retrieved and returns hints that
// appeared since the last snapshot of this seed, if there was one.
func (t *hintTracker) snapshot(lists [][]archipelago.Hint) []archipelago.Hint {
	var out []archipelago.Hint
	for _, hints := range lists {
		out = append(out, t.update(hints)...)
	}
	t.ready = true
	return out
}

// update records a slot's hint list and returns unfound hints not seen
// before.
func (t *hintTracker) update(hints []archipelago.Hint) []archipelago.Hint {
	var out []archipelago.Hint
	for _, h := range hints {
		key := hintKey{finder: h.FindingPlayer, location: h.Location}
		if _, ok := t.known[key]; ok {
			continue
		}
		t.known[key] = struct{}{}
		if t.ready && !h.Found {
			out = append(out, h)
		}
	}
	return out
}

const maxRecentDeaths = 16

type deathKey struct {
	source string
	time   float64
}

// deathTracker drops repeats of the same death, which can happen when
// several clients share a slot.
type deathTracker struct {
	recent []deathKey
}

func (t *deathTracker) isNew(d archipelago.DeathLink) bool {
	key := deathKey{source: d.Source, time: d.Time}
	if slices.Contains(t.recent, key) {
		return false
	}
	t.recent = append(t.recent, key)
	if len(t.recent) > maxRecentDeaths {
		t.recent = t.recent[1:]
	}
	return true
}

const maxPendingEchoes = 32

// echoTracker remembers text this backend sent with Say so the server's
// Chat broadcast of it isn't fed back into seabird. A real player connected
// to the same slot still gets through, unless they say exactly the same
// thing before the echo arrives.
type echoTracker struct {
	mu      sync.Mutex
	pending []string
}

func (t *echoTracker) add(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pending = append(t.pending, text)
	if len(t.pending) > maxPendingEchoes {
		t.pending = t.pending[1:]
	}
}

// take removes text from the pending list, reporting whether it was there.
func (t *echoTracker) take(text string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	i := slices.Index(t.pending, text)
	if i < 0 {
		return false
	}
	t.pending = slices.Delete(t.pending, i, i+1)
	return true
}

func (t *echoTracker) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pending = nil
}
