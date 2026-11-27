//go:build !leafen_noapps

package app

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An app answers its endpoint's request after calls of its own into Leafen.
// When one of them was turned away for a moment (the app over its budget,
// apps' writes queued, Leafen busy: a 429 or a 503) and the app then failed
// with a server error, the client is told to come back with 503 and
// Retry-After, as Leafen's own refusals tell it. A request the app was
// stopped under ends as a stopped app's requests do (2026-10-16 OP11 load:
// Web Clips answered 500 and the drive 502, nothing saying when to try again,
// so a clipper dropped its clip and a WebDAV client failed its save).

// appRunRefusals holds the longest wait a run's calls into Leafen were told.
type appRunRefusals struct {
	mu         sync.Mutex
	seen       bool
	retryAfter int
}

type appRunRefusalsKey struct{}

func withAppRunRefusals(ctx context.Context) (context.Context, *appRunRefusals) {
	refusals := &appRunRefusals{}
	return context.WithValue(ctx, appRunRefusalsKey{}, refusals), refusals
}

// noteAppRunRefusal keeps a call's refusal for a moment, if the run is an
// endpoint's.
func noteAppRunRefusal(ctx context.Context, result nativeOperationResult) {
	refusals, _ := ctx.Value(appRunRefusalsKey{}).(*appRunRefusals)
	if refusals == nil || (result.Status != http.StatusTooManyRequests && result.Status != http.StatusServiceUnavailable) {
		return
	}
	wait, err := strconv.Atoi(result.RetryAfter)
	if err != nil || wait < 1 {
		wait = 1
	}
	refusals.mu.Lock()
	defer refusals.mu.Unlock()
	refusals.seen = true
	refusals.retryAfter = max(refusals.retryAfter, min(wait, 60))
}

// failedWait is how long to tell a client whose request failed with status
// to wait: a server error after a refused call.
func (refusals *appRunRefusals) failedWait(status int) (int, bool) {
	refusals.mu.Lock()
	defer refusals.mu.Unlock()
	return refusals.retryAfter, refusals.seen && status >= http.StatusInternalServerError
}

// appStops remembers when each app was last stopped, for the requests it was
// answering then.
var appStops sync.Map

func init() {
	appStopCleanups = append(appStopCleanups, func(installID string) { appStops.Store(installID, time.Now()) })
}

func appStoppedSince(installID string, since time.Time) bool {
	stopped, ok := appStops.Load(installID)
	return ok && !stopped.(time.Time).Before(since)
}

// appEndpointRunFailed answers a run that failed as the client should take
// it when the app was stopped under it or a call of its own was refused;
// answered is false when it was neither.
func appEndpointRunFailed(event *core.RequestEvent, installID string, started time.Time, refusals *appRunRefusals) (bool, error) {
	if appStoppedSince(installID, started) {
		return true, appEndpointPlain(event, http.StatusServiceUnavailable, "The app is stopped.")
	}
	if wait, refused := refusals.failedWait(http.StatusBadGateway); refused {
		event.Response.Header().Set("Retry-After", strconv.Itoa(wait))
		return true, appEndpointPlain(event, http.StatusServiceUnavailable, "The app is busy; try again in a moment.")
	}
	return false, nil
}
