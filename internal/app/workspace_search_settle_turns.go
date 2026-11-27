package app

import (
	"context"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Before a page's index row is written, the chunks it gained go in and the
// ones it lost go out, a short write at a time (2026-10-19: a page's chunks
// were all written in the one write with its row, and a settle's first page
// was always written whole — a long page held the one write connection while
// people's typing waited). The last write then holds the page's row, its
// chunks' order and the check that it didn't change since it was read; were
// it changed, the next turn makes the chunks what it is then, each chunk
// known by what it holds.

// prepareSearchPageChunks writes what changed of a page's chunks in writes
// of about workspaceSearchSettleBudget, waiting for people's edits before
// each in the background.
func prepareSearchPageChunks(app core.App, projection searchPageProjection, background bool) error {
	kept := []storedSearchChunk{}
	if err := app.DB().NewQuery(`SELECT [[chunk_key]], [[chunk_no]], [[fts_row]] FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": projection.PageID}).All(&kept); err != nil {
		return err
	}
	byKey := make(map[string]storedSearchChunk, len(kept))
	for _, chunk := range kept {
		byKey[chunk.Key] = chunk
	}
	type step struct {
		add  *searchChunk
		key  string
		no   int
		gone storedSearchChunk
	}
	steps := []step{}
	seen := map[string]int{}
	if !projection.Deleted {
		for no := range projection.Chunks {
			chunk := &projection.Chunks[no]
			key := fmt.Sprintf("%s:%d", chunk.Hash, seen[chunk.Hash])
			seen[chunk.Hash]++
			if _, found := byKey[key]; found {
				delete(byKey, key)
				continue
			}
			if chunk.Kept {
				return errSearchMemoStale
			}
			steps = append(steps, step{add: chunk, key: key, no: no})
		}
	}
	for _, gone := range byKey {
		steps = append(steps, step{gone: gone})
	}
	pageToken := workspaceSearchChunkPageToken(projection.PageID)
	scopeToken := workspaceSearchChunkScopeToken(projection.WorkspaceID)
	for len(steps) > 0 {
		if background {
			yieldToPeople(context.Background())
		}
		if err := app.RunInTransaction(func(txApp core.App) error {
			began := time.Now()
			for first := true; len(steps) > 0 && (first || time.Since(began) < workspaceSearchSettleBudget); first = false {
				next := steps[0]
				steps = steps[1:]
				if next.add == nil {
					if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] = {:row}`).
						Bind(dbx.Params{"row": next.gone.Row}).Execute(); err != nil {
						return err
					}
					if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId} AND [[chunk_key]] = {:key}`).
						Bind(dbx.Params{"pageId": projection.PageID, "key": next.gone.Key}).Execute(); err != nil {
						return err
					}
					continue
				}
				// Another turn may have written it meanwhile.
				var written int
				if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId} AND [[chunk_key]] = {:key}`).
					Bind(dbx.Params{"pageId": projection.PageID, "key": next.key}).Row(&written); err != nil {
					return err
				}
				if written > 0 {
					continue
				}
				if _, err := txApp.DB().NewQuery(`
					INSERT INTO {{leafen_search_pages_fts}} ([[page_id]], [[workspace_id]], [[page_token]], [[workspace_token]], [[title]], [[body]], [[grams]])
					VALUES ('', '', {:pageToken}, {:scopeToken}, '', {:indexed}, {:grams})
				`).Bind(dbx.Params{"pageToken": pageToken, "scopeToken": scopeToken, "indexed": next.add.Indexed, "grams": next.add.Grams}).Execute(); err != nil {
					return err
				}
				noteSearchIndexWrite(len(next.add.Indexed) + len(next.add.Grams))
				if _, err := txApp.DB().NewQuery(`
					INSERT INTO {{leafen_search_chunks}} ([[page_id]], [[chunk_key]], [[chunk_no]], [[workspace_id]], [[body]], [[body_folded]], [[fts_row]], [[mid_line]])
					VALUES ({:pageId}, {:key}, {:no}, {:workspaceId}, {:body}, {:folded}, last_insert_rowid(), {:midLine})
				`).Bind(dbx.Params{"pageId": projection.PageID, "key": next.key, "no": next.no, "workspaceId": projection.WorkspaceID,
					"body": next.add.Body, "folded": next.add.BodyFolded, "midLine": next.add.MidLine}).Execute(); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
