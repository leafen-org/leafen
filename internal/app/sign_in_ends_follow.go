package app

import (
	"context"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Sign-ins another process ends — the command line's `leafen account
// reset-password` beside a running server — reach the running server's open
// screens within seconds (2026-10-28: a chat tab learned only when its
// 25-second wait ran out, 12.6 seconds after; endSignInWaits in the
// command's own process wakes no wait here). The server looks every few
// seconds for sign-ins ended since and ends their people's waits; one it
// ended itself is woken once more, which only makes the screens ask again.

// signInEndsFollowEvery is how often the server looks for ended sign-ins.
const signInEndsFollowEvery = 2 * time.Second

// signInEndsLookBack is how far back each look reaches, so an end written
// just before a look and committed after it is still found.
const signInEndsLookBack = 10 * time.Second

// signInEndsFollower runs followSignInEnds while the server runs and ends it
// as the server stops, as the other background work does: the desktop app
// starts its own server again in the same process to finish a restore, where
// a look left running read the stopped server's closed database and ended
// the app (2026-11-07).
type signInEndsFollower struct {
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

func (follower *signInEndsFollower) Start(app core.App) {
	follower.mu.Lock()
	defer follower.mu.Unlock()
	if follower.stop != nil {
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	follower.stop, follower.done = stop, done
	go func() {
		defer close(done)
		followSignInEnds(app, stop)
	}()
}

func (follower *signInEndsFollower) Shutdown(ctx context.Context) error {
	follower.mu.Lock()
	stop, done := follower.stop, follower.done
	follower.stop, follower.done = nil, nil
	follower.mu.Unlock()
	if stop == nil {
		return nil
	}
	close(stop)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// followSignInEnds ends the waits of people whose sign-ins ended since the
// last look, until stop closes.
func followSignInEnds(app core.App, stop <-chan struct{}) {
	woken := map[string]int64{}
	ticker := time.NewTicker(signInEndsFollowEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		from := time.Now().Add(-signInEndsLookBack).UnixMilli()
		ends := []struct {
			Owner string `db:"owner_id"`
			At    int64  `db:"ended"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[owner_id]], MAX([[revoked_at]]) AS [[ended]] FROM {{leafen_auth_sessions}}
			WHERE [[revoked_at]] > {:from} GROUP BY [[owner_id]]`).Bind(dbx.Params{"from": from}).All(&ends); err != nil {
			continue
		}
		for _, end := range ends {
			if end.At > woken[end.Owner] {
				woken[end.Owner] = end.At
				endSignInWaits(app, end.Owner)
			}
		}
		for owner, at := range woken {
			if at <= from {
				delete(woken, owner)
			}
		}
	}
}
