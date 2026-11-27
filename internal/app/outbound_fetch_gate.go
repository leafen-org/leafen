package app

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// What the server reads from outside addresses for people — a link's
// metadata, a linked PDF — is bounded by person and for the whole server
// (2026-10-18: nothing bounded it, so one address could hold a hundred
// reads at once and the server would carry its traffic to another site).
// A person opening a page full of links has a few in flight as the page
// draws; a link preview holds at most 512 KiB and a PDF is streamed, so 32
// at once stay within a few tens of MiB.
const (
	outboundFetchesPerPerson = 4
	outboundFetchesAtOnce    = 32
	outboundFetchWait        = 2 * time.Second
	outboundFetchRetryAfter  = 5
)

type outboundFetchGate struct {
	slots     chan struct{}
	mu        sync.Mutex
	perPerson map[string]int
}

var outboundFetches = &outboundFetchGate{
	slots: make(chan struct{}, outboundFetchesAtOnce), perPerson: map[string]int{},
}

// acquire holds one of the person's reads and one of the server's, waiting
// a little for the server's; ok is false when either stays full.
func (gate *outboundFetchGate) acquire(ctx context.Context, personID string) (func(), bool) {
	gate.mu.Lock()
	if gate.perPerson[personID] >= outboundFetchesPerPerson {
		gate.mu.Unlock()
		return nil, false
	}
	gate.perPerson[personID]++
	gate.mu.Unlock()
	releasePerson := func() {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		if gate.perPerson[personID]--; gate.perPerson[personID] <= 0 {
			delete(gate.perPerson, personID)
		}
	}
	timer := time.NewTimer(outboundFetchWait)
	defer timer.Stop()
	select {
	case gate.slots <- struct{}{}:
		return func() { <-gate.slots; releasePerson() }, true
	case <-timer.C:
	case <-ctx.Done():
	}
	releasePerson()
	return nil, false
}

// admitOutboundFetch holds a read for the signed-in person, or answers that
// too many are in flight and when to try again.
func admitOutboundFetch(event *core.RequestEvent, personID string) (func(), error, bool) {
	release, ok := outboundFetches.acquire(event.Request.Context(), personID)
	if ok {
		return release, nil, true
	}
	event.Response.Header().Set("Retry-After", strconv.Itoa(outboundFetchRetryAfter))
	return nil, apiError(event, http.StatusTooManyRequests, errorCodeRateLimited,
		"Too many links are being read at once. Try again in a few seconds."), false
}
