package app

import (
	"context"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Search's index is made in the background: a change marks its page dirty
// (workspace_search_dirty.go) and wakes the indexer, which waits a
// moment for the changes around it and settles the dirty pages a window at
// a time, each workspace under its stripe so two never settle the same one.
// A search reads the index as it stands and never waits on it; it settles a
// few waiting pages itself only when no one else is (advanceWorkspaceSearch).

const (
	workspaceSearchIndexInterval = time.Second
	// A change waits this long for the changes around it, and turns come at
	// most this often: a page edited all the time is read again a few times a
	// second, not without pause.
	workspaceSearchIndexSettle     = 150 * time.Millisecond
	workspaceSearchIndexMinTurn    = 500 * time.Millisecond
	workspaceSearchIndexWorkspaces = 32
)

const workspaceSearchIndexerStoreKey = "leafen.workspaceSearchIndexer"

type workspaceSearchIndexer struct {
	app    core.App
	wakeup chan struct{}
	mu     sync.Mutex
	stop   context.CancelFunc
	done   chan struct{}
	// A merge of the index is under way (workspace_search_merge.go).
	merging bool
}

func newWorkspaceSearchIndexer(app core.App) *workspaceSearchIndexer {
	return &workspaceSearchIndexer{app: app, wakeup: make(chan struct{}, 1)}
}

// wakeWorkspaceSearchIndexer tells the indexer pages are waiting; it looks
// on its own every second too.
func wakeWorkspaceSearchIndexer(app core.App) {
	if indexer, ok := app.Store().Get(workspaceSearchIndexerStoreKey).(*workspaceSearchIndexer); ok && indexer != nil {
		select {
		case indexer.wakeup <- struct{}{}:
		default:
		}
	}
}

func (indexer *workspaceSearchIndexer) Start() {
	indexer.mu.Lock()
	defer indexer.mu.Unlock()
	if indexer.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	indexer.stop, indexer.done = cancel, make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		ticker := time.NewTicker(workspaceSearchIndexInterval)
		defer ticker.Stop()
		for {
			began := time.Now()
			indexer.runTurn(ctx)
			indexer.mergeIndex(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-indexer.wakeup:
				wait := max(workspaceSearchIndexSettle, workspaceSearchIndexMinTurn-time.Since(began))
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
		}
	}(indexer.done)
}

func (indexer *workspaceSearchIndexer) Shutdown(ctx context.Context) error {
	indexer.mu.Lock()
	stop, done := indexer.stop, indexer.done
	indexer.stop, indexer.done = nil, nil
	indexer.mu.Unlock()
	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runTurn settles a window of what waits in each workspace, so none waits
// behind a large one, and goes round again only while a backlog is larger
// than a window and settling; pages changed again meanwhile, and a
// workspace whose pages fail, are left for the next turn.
func (indexer *workspaceSearchIndexer) runTurn(ctx context.Context) {
	failed := map[string]bool{}
	for ctx.Err() == nil {
		workspaces := []string{}
		if err := indexer.app.DB().NewQuery(`
			SELECT [[workspace_id]] FROM {{leafen_search_dirty}}
			GROUP BY [[workspace_id]] ORDER BY MIN([[enqueued_at]]) LIMIT {:limit}
		`).Bind(dbx.Params{"limit": workspaceSearchIndexWorkspaces + len(failed)}).Column(&workspaces); err != nil {
			indexer.app.Logger().Warn("search index: reading what waits failed", "error", err)
			return
		}
		backlog := false
		for _, workspaceID := range workspaces {
			if failed[workspaceID] || ctx.Err() != nil {
				continue
			}
			stripe := workspaceSearchStripe(workspaceID)
			stripe.Lock()
			settled, more, err := drainWorkspaceSearchProjection(indexer.app, workspaceID, true, 0)
			stripe.Unlock()
			if err != nil {
				failed[workspaceID] = true
				indexer.app.Logger().Warn("search index: settling pages failed", "workspaceId", workspaceID, "error", err)
			}
			backlog = backlog || (more && settled > 0)
		}
		if !backlog {
			return
		}
	}
}

// workspaceSearchBacklog is whether more of a workspace's pages wait for the
// index than a turn settles: a big change is still being indexed, and a
// search may miss some of it, which the screen and agents are told
// (2026-10-15: indexing waits for people's edits, and after a big import it
// takes a while).
func workspaceSearchBacklog(app core.App, workspaceID string) bool {
	var waiting int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_search_dirty}} WHERE [[workspace_id]] = {:workspaceId} LIMIT {:limit})
	`).Bind(dbx.Params{"workspaceId": workspaceID, "limit": workspaceSearchProjectionWindow + 1}).Row(&waiting); err != nil {
		return false
	}
	return waiting > workspaceSearchProjectionWindow
}
