package app

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Search's full-text index is merged as it is written. An FTS5 row can't be
// changed, so a page or chunk that changes is deleted and written again; the
// old copy and the mark that deletes it stay until the segments holding them
// are merged, and FTS5 merges its largest ones rarely. On a workspace changed
// all the time the index grew to four times its merged size and the same
// search got four times slower (2026-10-20: 128 MB against 32.5 MB merged,
// p50 104 ms against 34 ms).
//
// So the indexer merges the whole index again, a few pages at a time, once
// about half as much has been written to it since as the last merge wrote: a
// merge costs the index's size, paid by half that much writing, and the
// index stays within about one and a half times its merged size (2026-10-20,
// counting each byte once: twice). Each step is a short write after people's
// edits, and a turn of steps is capped, as the index's other background
// writes are.

const (
	// The pages of the index (FTS5's pgsz, 4050 bytes) one step writes.
	workspaceSearchMergePages = 32
	workspaceSearchMergePage  = 4050
	// How long a turn of merge steps goes on.
	workspaceSearchMergeTurn = 100 * time.Millisecond
)

// searchIndexMergeOwed is the text written to the index, less what merging
// paid for, in bytes; merging starts when it is above zero.
var searchIndexMergeOwed atomic.Int64

// noteSearchIndexWrite counts text written to the index, twice: a merge
// is owed by half its size in writing.
func noteSearchIndexWrite(bytes int) {
	searchIndexMergeOwed.Add(2 * int64(bytes))
}

// mergeIndex takes merge steps while one is owed or a merge is under way,
// for up to workspaceSearchMergeTurn: the first step of a merge takes in all
// the index's segments (a negative 'merge'), the next ones carry it on, and
// one that finds nothing to do ends it.
func (indexer *workspaceSearchIndexer) mergeIndex(ctx context.Context) {
	began := time.Now()
	for ctx.Err() == nil && time.Since(began) < workspaceSearchMergeTurn &&
		(indexer.merging || searchIndexMergeOwed.Load() > 0) {
		yieldToPeople(ctx)
		pages := workspaceSearchMergePages
		if !indexer.merging {
			pages = -pages
		}
		worked := false
		err := indexer.app.RunInTransaction(func(txApp core.App) error {
			// FTS5 says a 'merge' did nothing by fewer than two changes.
			var before, after int64
			if err := txApp.DB().NewQuery(`SELECT total_changes()`).Row(&before); err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_search_pages_fts}} ([[leafen_search_pages_fts]], [[rank]])
				VALUES ('merge', {:pages})`).Bind(dbx.Params{"pages": pages}).Execute(); err != nil {
				return err
			}
			if err := txApp.DB().NewQuery(`SELECT total_changes()`).Row(&after); err != nil {
				return err
			}
			worked = after-before >= 2
			return nil
		})
		if err != nil {
			indexer.app.Logger().Warn("search index: merging failed", "error", err)
			return
		}
		if worked {
			indexer.merging = true
			searchIndexMergeOwed.Add(-workspaceSearchMergePages * workspaceSearchMergePage)
			continue
		}
		if !indexer.merging {
			// One segment: nothing to merge until it is written again.
			searchIndexMergeOwed.Store(0)
			return
		}
		indexer.merging = false
	}
}
