package app

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// When a sign-in session was last used: what the account's device list says
// and which session the limit signs out first. Each request on a session
// sets its last use here; the session's row is written at most once in
// authSessionUseInterval (the first use at once), so most requests write
// nothing, and the readers take the later of the row and what is held here.
// A session not used for an interval is let go, its last use written first,
// so what is held is the sessions in use. A stopping server writes what it
// holds; one that ends without stopping, or whose write a write under way
// keeps waiting past its time (shutdown.go), forgets at most an interval of
// use. What is held keeps its server: one stopped and started again in the
// same process (the apps after a restore, server_background.go) leaves what
// it held, which is let go unwritten, its data closed, and a request on the
// new server holds the session's use anew.

const authSessionUseInterval = 5 * time.Minute

// authSessionLastUsedSQL is a session's last use where a query reads it: the
// written one, or its renewal or creation where none is later.
const authSessionLastUsedSQL = `MAX([[last_used_at]], COALESCE([[rotated_at]], [[created_at]]))`

type authSessionUse struct {
	app     core.App     // the server the session is on
	seen    atomic.Int64 // the latest request, in milliseconds
	written atomic.Int64 // the use the row has, in milliseconds
}

var (
	authSessionUses      sync.Map // session id -> *authSessionUse
	authSessionUsesSwept atomic.Int64
)

func noteAuthSessionUse(app core.App, sessionID string, now time.Time) error {
	at := now.UnixMilli()
	server := serverAppOf(app)
	held, _ := authSessionUses.LoadOrStore(sessionID, &authSessionUse{app: server})
	for held.(*authSessionUse).app != server {
		authSessionUses.CompareAndDelete(sessionID, held)
		held, _ = authSessionUses.LoadOrStore(sessionID, &authSessionUse{app: server})
	}
	use := held.(*authSessionUse)
	for seen := use.seen.Load(); seen < at && !use.seen.CompareAndSwap(seen, at); seen = use.seen.Load() {
	}
	sweepAuthSessionUses(at)
	written := use.written.Load()
	if at-written < authSessionUseInterval.Milliseconds() || !use.written.CompareAndSwap(written, at) {
		return nil
	}
	if err := writeAuthSessionUse(app, sessionID, at); err != nil {
		use.written.CompareAndSwap(at, written)
		return err
	}
	return nil
}

func writeAuthSessionUse(app core.App, sessionID string, at int64) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_auth_sessions}} SET [[last_used_at]] = {:at}
		WHERE [[id]] = {:id} AND [[last_used_at]] < {:at}`).
		Bind(dbx.Params{"id": sessionID, "at": at}).Execute()
	return err
}

// sweepAuthSessionUses lets go, at most once an interval and off the
// request, of the sessions not used for an interval, writing a last use
// their rows don't have yet. Each server's are written under its own
// background, waited for as it stops (server_background.go); a stopped
// server's are let go unwritten, its data closed.
func sweepAuthSessionUses(at int64) {
	last := authSessionUsesSwept.Load()
	if at-last < authSessionUseInterval.Milliseconds() || !authSessionUsesSwept.CompareAndSwap(last, at) {
		return
	}
	type idle struct {
		id  string
		use *authSessionUse
	}
	go func() {
		due := map[core.App][]idle{}
		authSessionUses.Range(func(id, held any) bool {
			use := held.(*authSessionUse)
			if !dataWorkOf(use.app).isOpen() {
				authSessionUses.CompareAndDelete(id, held)
			} else if at-use.seen.Load() >= authSessionUseInterval.Milliseconds() {
				due[use.app] = append(due[use.app], idle{id: id.(string), use: use})
			}
			return true
		})
		for server, uses := range due {
			serverBackgroundOf(server).run("login sessions' last uses", func(context.Context) {
				for _, held := range uses {
					seen := held.use.seen.Load()
					if at-seen < authSessionUseInterval.Milliseconds() {
						continue
					}
					if seen > held.use.written.Load() {
						if err := writeAuthSessionUse(server, held.id, seen); err != nil {
							server.Logger().Warn("leafen session use not noted", "error", err)
							continue
						}
					}
					authSessionUses.CompareAndDelete(held.id, held.use)
				}
			})
		}
	}()
}

// writeAuthSessionUses writes, as the server stops, the last uses the
// sessions' rows don't have yet; they may be lost (lastWriteMayBeLost).
func writeAuthSessionUses(ctx context.Context) error {
	var failed error
	authSessionUses.Range(func(id, held any) bool {
		use := held.(*authSessionUse)
		if !dataWorkOf(use.app).isOpen() {
			authSessionUses.CompareAndDelete(id, held)
			return true
		}
		if seen := use.seen.Load(); seen > use.written.Load() {
			if failed = writeAuthSessionUse(use.app, id.(string), seen); failed != nil {
				return false
			}
			use.written.Store(seen)
		}
		return ctx.Err() == nil
	})
	return failed
}

// authSessionLastUse is a session's last use: the later of the one its row
// gave (milliseconds) and the latest request held here.
func authSessionLastUse(sessionID string, stored int64) int64 {
	if held, ok := authSessionUses.Load(sessionID); ok {
		return max(stored, held.(*authSessionUse).seen.Load())
	}
	return stored
}

// sortAuthSessionsByUse orders sessions read with their stored last use
// most recently used first.
func sortAuthSessionsByUse(rows []storedAuthSession) {
	for index := range rows {
		rows[index].LastUsedAt = authSessionLastUse(rows[index].ID, rows[index].LastUsedAt)
	}
	slices.SortStableFunc(rows, func(a, b storedAuthSession) int {
		return cmp.Or(cmp.Compare(b.LastUsedAt, a.LastUsedAt), cmp.Compare(b.ID, a.ID))
	})
}

// leastRecentlyUsedAuthSession is the open session of an owner used longest
// ago, or "" when there is none.
func leastRecentlyUsedAuthSession(app core.App, ownerID string, now int64) (string, error) {
	rows := []storedAuthSession{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], ` + authSessionLastUsedSQL + ` AS [[last_used_at]] FROM {{leafen_auth_sessions}}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{"ownerId": ownerID, "now": now}).All(&rows); err != nil || len(rows) == 0 {
		return "", err
	}
	sortAuthSessionsByUse(rows)
	return rows[len(rows)-1].ID, nil
}
