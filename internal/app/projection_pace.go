package app

import (
	"sync"
	"time"
)

// A page's search entry and its reminders, mentions and links are made again
// from the whole page after each change, in the background. For a long page
// changed again and again — an import adding blocks to it a turn at a time —
// that is work in the page's size after every turn, and it took most of the
// server (2026-10-15: at 15,000 blocks, half of the import's time and its
// memory churn). So a page waits, before it is made again, ten times as long
// as it took to make last: a small page at once, a long one every few
// seconds while it keeps changing, and once more when it stops.
//
// A page's search entry also waits a few seconds after it was made, however
// quickly that went: a page being typed changes every second, and a page of
// 750 KB made again at each change was most of a busy server's work
// (2026-10-20). Its last words are found a few seconds later, as in Notion's
// search; reminders and mentions don't wait, as people expect them as they
// type.

const (
	projectionPaceFactor = 10
	// A page made faster than this isn't held back.
	projectionPaceFloor = 20 * time.Millisecond
	// Nor is any page held longer than this.
	projectionPaceCeiling = 30 * time.Second
	// How long a page's search entry waits after it was made.
	searchProjectionGap = 3 * time.Second
)

type projectionPace struct {
	mu    sync.Mutex
	gap   time.Duration
	next  map[string]time.Time
	swept time.Time
}

func newProjectionPace(gap time.Duration) *projectionPace {
	return &projectionPace{gap: gap, next: map[string]time.Time{}}
}

var (
	searchProjectionPace   = newProjectionPace(searchProjectionGap)
	reminderProjectionPace = newProjectionPace(0)
)

// ready reports whether a page may be made now, and else when it may.
func (pace *projectionPace) ready(pageID string, now time.Time) (bool, time.Time) {
	pace.mu.Lock()
	defer pace.mu.Unlock()
	next, held := pace.next[pageID]
	if !held || !now.Before(next) {
		return true, time.Time{}
	}
	return false, next
}

// made notes how long a page took to make, starting at began.
func (pace *projectionPace) made(pageID string, began, now time.Time) {
	took := now.Sub(began)
	wait := pace.gap
	if took >= projectionPaceFloor {
		wait = max(wait, min(took*projectionPaceFactor, projectionPaceCeiling))
	}
	pace.mu.Lock()
	defer pace.mu.Unlock()
	if wait <= 0 {
		delete(pace.next, pageID)
		return
	}
	pace.next[pageID] = now.Add(wait)
	// Pages long done with leave the table, looked for once a second: with
	// a gap every page made is in it a while.
	if now.Sub(pace.swept) < time.Second {
		return
	}
	pace.swept = now
	for id, next := range pace.next {
		if now.Sub(next) > projectionPaceCeiling {
			delete(pace.next, id)
		}
	}
}
