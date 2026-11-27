package app

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The data database's schema is large — some five hundred tables, fifteen
// hundred indexes and a thousand triggers, 850 KB of SQL (2026-11-14) — and a
// new connection reads all of it before its first statement, then compiles
// its statements afresh: about 240 ms a connection on a NAS-class server.
// The data's read pools open connections as they are asked for and close one
// left unused for three minutes (PocketBase's pool, and the read snapshots'
// as it), so the first visit after a start, and any visit after a few quiet
// minutes — the usual on a home server — opened several at once; SQLite as
// modernc builds it allocates behind one lock, so they took turns, and the
// reads waiting for them answered together 600–970 ms late (2026-11-14, NAS
// condition: an organization's people, notifications, a page's comments,
// backlinks and the page itself, each 1–36 ms on an open connection).
//
// So each of those pools keeps open as many connections as requests are
// worked on at once (request_admission.go), at most half the pool: they are
// opened one after another as the server starts, and taken and given back
// before the pool would close them. Connections beyond them still close
// after three idle minutes. What they hold stays within the memory plan,
// which sizes every connection's caches as if all were open (memory_plan.go).

const readConnectionsKeepEvery = time.Minute

type readConnectionsKeeper struct {
	app core.App

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newReadConnectionsKeeper(app core.App) *readConnectionsKeeper {
	return &readConnectionsKeeper{app: app}
}

func (keeper *readConnectionsKeeper) Start() {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	if keeper.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	keeper.cancel = cancel
	keeper.done = make(chan struct{})
	go keeper.run(ctx, keeper.done)
}

func (keeper *readConnectionsKeeper) Shutdown(ctx context.Context) error {
	keeper.mu.Lock()
	cancel, done := keeper.cancel, keeper.done
	keeper.cancel, keeper.done = nil, nil
	keeper.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("keeping read connections open: %w", ctx.Err())
	}
}

func (keeper *readConnectionsKeeper) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		for _, pool := range keeper.pools() {
			keepReadConnectionsOpen(ctx, pool)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(readConnectionsKeepEvery):
		}
	}
}

// pools are the data database's read pools: PocketBase's and the read
// snapshots' (read_snapshot.go).
func (keeper *readConnectionsKeeper) pools() []*sql.DB {
	pools := []*sql.DB{}
	if db, ok := keeper.app.ConcurrentDB().(*dbx.DB); ok && db != nil {
		pools = append(pools, db.DB())
	}
	if db, ok := keeper.app.Store().Get(readSnapshotPoolStoreKey).(*dbx.DB); ok && db != nil {
		pools = append(pools, db.DB())
	}
	return pools
}

// keepReadConnectionsOpen takes the connections a pool keeps all at once,
// opening those it lacks one after another, and gives them back: each one's
// three minutes start again. A pool busy past them is left as it is — its
// connections are open and in use.
func keepReadConnectionsOpen(ctx context.Context, pool *sql.DB) {
	stats := pool.Stats()
	keep := min(cap(requestAdmissionSlots), stats.MaxOpenConnections/2)
	if keep <= 0 || stats.InUse+keep > stats.MaxOpenConnections {
		return
	}
	held := make([]*sql.Conn, 0, keep)
	defer func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	}()
	for len(held) < keep {
		conn, err := pool.Conn(ctx)
		if err != nil {
			return
		}
		held = append(held, conn)
	}
}
