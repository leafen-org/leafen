package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedSearchDirty struct {
	PageID      string `db:"page_id"`
	WorkspaceID string `db:"workspace_id"`
	Version     int    `db:"version"`
	// The blocks whose text alone changed, while BlocksVersion is Version
	// (workspace_search_dirty_blocks.go).
	Blocks        string `db:"blocks"`
	BlocksVersion int    `db:"blocks_version"`
}

type storedSearchBlock struct {
	ID string `db:"id"`
}

type searchDirtyTarget struct {
	PageID      string `json:"pageId"`
	WorkspaceID string `json:"workspaceId"`
}

type searchPageProjection struct {
	PageID        string
	WorkspaceID   string
	OwnerID       string
	Title         string
	TitleFolded   string
	Body          string
	BodyFolded    string
	SourceVersion int
	Deleted       bool
	// Partial is set when a document's words are not all in the body.
	Partial bool
	// Grams are the title's pairs for the index, Words the body's words and
	// pairs, Chunks its lines (workspace_search_chunks.go): made before the
	// write.
	Grams  string
	Words  string
	Chunks []searchChunk
	// WordsApart is set when the body's words are too many for the page's
	// row: its row holds its title alone (workspace_search_apart.go).
	WordsApart bool
	// Prepared is set when Chunks, Words and BodyRunes are made already, from
	// a kept page (workspace_search_memo.go), which memo is kept again once
	// the entry is written.
	Prepared  bool
	BodyRunes int
	memo      *searchPageMemo
	// Row is set for a database item, whose text cells mention the people
	// CellMentions names (search_cell_person_mentions.go).
	Row          bool
	CellMentions []string
}

// searchTextBlockTypesJSON names every block type whose text a page's search
// body reads: headings, lists, to-dos, toggles, quotes, callouts and code as
// well as paragraphs.
var searchTextBlockTypesJSON = func() string {
	types := make([]string, 0, len(graphTextBlockTypes))
	for blockType := range graphTextBlockTypes {
		types = append(types, blockType)
	}
	sort.Strings(types)
	encoded, _ := json.Marshal(types)
	return string(encoded)
}()

// drainWorkspaceSearchProjection settles up to a window of a workspace's
// waiting pages: how many it settled, and whether more than a window
// waited. A page changed again while it was read is left for the next turn.
//
// The pages are read first, then settled together, a short write at a time
// (workspaceSearchSettleBudget): one write for each page waited for the one
// write connection as often as pages, and while people kept writing the
// index hardly moved (2026-10-15: 43,000 pages of a CSV import waited all
// the time 300 people worked). In the background, each write waits for
// people's edits first (write_lane.go). With maxDocumentBytes, only pages
// whose stored text document — snapshot and updates after it — is no longer
// are made; the rest wait for the indexer.
func drainWorkspaceSearchProjection(app core.App, workspaceID string, background bool, maxDocumentBytes int) (int, bool, error) {
	dirty := []storedSearchDirty{}
	err := app.DB().NewQuery(`
		SELECT [[dirty]].[[page_id]], [[dirty]].[[workspace_id]], [[dirty]].[[version]], [[dirty]].[[blocks]], [[dirty]].[[blocks_version]]
		FROM {{leafen_search_dirty}} [[dirty]]
		WHERE [[dirty]].[[workspace_id]] = {:workspaceId}
			AND ({:maxBytes} = 0 OR COALESCE((
				SELECT LENGTH([[stored]].[[state]]) + [[head]].[[pending_bytes]]
				FROM {{leafen_ygo_documents}} [[head]] JOIN {{leafen_ygo_document_states}} [[stored]] USING ([[room]])
				WHERE [[head]].[[room]] = [[dirty]].[[page_id]]
			), 0) <= {:maxBytes})
		ORDER BY [[dirty]].[[page_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "maxBytes": maxDocumentBytes, "limit": workspaceSearchProjectionWindow + 1,
	}).All(&dirty)
	if err != nil {
		return 0, false, err
	}
	more := len(dirty) > workspaceSearchProjectionWindow
	if more {
		dirty = dirty[:workspaceSearchProjectionWindow]
	}
	// A page that fails stays waiting; the others settle all the same. Read
	// pages wait to be settled together only up to a size, so a window of
	// long pages is never held at once: a long one goes alone.
	var failed error
	settled := 0
	built, builtBytes := []searchSettleItem{}, 0
	settle := func() {
		for len(built) > 0 {
			if background {
				yieldToPeople(context.Background())
			}
			done, count, err := settleSearchPageProjections(app, built)
			if err == nil {
				settled += count
				keepSearchPageMemos(built[:done])
				built = built[done:]
				continue
			}
			// One at a time then, so a page that fails leaves the others settled.
			for index := range built {
				_, count, err := settleSearchPageProjections(app, built[index:index+1])
				if err != nil && failed == nil {
					failed = err
				}
				if err == nil {
					keepSearchPageMemos(built[index : index+1])
				}
				settled += count
			}
			built = nil
		}
		built, builtBytes = []searchSettleItem{}, 0
	}
	for _, dirtyItem := range dirty {
		// A long page changed again and again waits its turn (projection_pace.go).
		if ready, _ := searchProjectionPace.ready(dirtyItem.PageID, time.Now()); !ready {
			continue
		}
		began := time.Now()
		projection, err := buildSearchPageForIndex(app, dirtyItem)
		if err == nil && !projection.Deleted && !projection.Prepared {
			// Made here, not while the index is written: a long page's held
			// the one write connection (2026-10-15).
			prepareSearchPageProjection(&projection)
		}
		if err == nil {
			// Its chunks a short write at a time (workspace_search_settle_turns.go).
			err = prepareSearchPageChunks(app, projection, background)
		}
		// The page waits by what reading and writing it took.
		searchProjectionPace.made(dirtyItem.PageID, began, time.Now())
		if err != nil {
			if failed == nil {
				failed = err
			}
			continue
		}
		size := len(projection.Title) + len(projection.TitleFolded) + len(projection.Body) + len(projection.BodyFolded)
		if len(built) > 0 && builtBytes+size > workspaceSearchSettleBytes {
			settle()
		}
		built, builtBytes = append(built, searchSettleItem{dirty: dirtyItem, projection: projection}), builtBytes+size
	}
	settle()
	return settled, more, failed
}

// workspaceSearchSettleBudget is about how long one write of the index
// holds the write connection: pages are settled together until it is spent;
// workspaceSearchSettleBytes is how much read text waits to be settled.
const (
	workspaceSearchSettleBudget = 25 * time.Millisecond
	workspaceSearchSettleBytes  = 1 << 20
)

type searchSettleItem struct {
	dirty      storedSearchDirty
	projection searchPageProjection
	// settled is set once its entry is written.
	settled bool
}

// keepSearchPageMemos keeps again what long pages were made from, once their
// entries are written; one passed over, changed again since, is made whole.
func keepSearchPageMemos(items []searchSettleItem) {
	for _, item := range items {
		if item.settled && item.projection.memo != nil {
			item.projection.memo.kept()
			searchPageMemos.put(item.projection.PageID, item.projection.memo)
		}
	}
}

// settleSearchPageProjections writes read pages into the index, as many as
// fit one write: how many it went through and how many of them it settled.
// A page changed again since it was read is passed over, left waiting.
func settleSearchPageProjections(app core.App, items []searchSettleItem) (int, int, error) {
	done, settled := 0, 0
	written := []int{}
	holders := false
	err := app.RunInTransaction(func(txApp core.App) error {
		done, settled, written, holders = 0, 0, written[:0], false
		began := time.Now()
		workspaces := map[string]bool{}
		for done < len(items) && (done == 0 || time.Since(began) < workspaceSearchSettleBudget) {
			item := items[done]
			done++
			current, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_search_dirty}}
				WHERE [[page_id]] = {:pageId} AND [[version]] = {:version}
			`).Bind(dbx.Params{"pageId": item.dirty.PageID, "version": item.dirty.Version}).Execute()
			if err != nil {
				return err
			}
			if affected, err := current.RowsAffected(); err != nil || affected != 1 {
				if err != nil {
					return err
				}
				continue
			}
			// The page's own row; its chunks change with what changed.
			moreFTSRows, err := deleteWorkspaceSearchFTSPageRow(txApp, item.projection.PageID)
			if err != nil {
				return err
			}
			if moreFTSRows {
				return errors.New(errorCodeWorkspaceSearchProjectionFailed)
			}
			if item.projection.Deleted {
				if err := deleteSearchPageChunks(txApp, item.projection.PageID); err != nil {
					return err
				}
				if _, err := txApp.DB().NewQuery(`
					DELETE FROM {{leafen_search_pages}} WHERE [[page_id]] = {:pageId}
				`).Bind(dbx.Params{"pageId": item.projection.PageID}).Execute(); err != nil {
					return err
				}
			} else {
				// A sub-page's title is a line of the page holding its block;
				// a database item's is none, but a wiki's page is a line of
				// its home by the link standing for it there (searchPageChildLines).
				retitled, err := searchEntryRetitled(txApp, item.projection)
				if err != nil {
					return err
				}
				if err := upsertSearchPageProjection(txApp, item.projection); err != nil {
					return err
				}
				if retitled {
					// The page holding it reads its title as a line.
					if err := markSearchSubPageLineDirty(txApp, item.projection.WorkspaceID, item.projection.PageID); err != nil {
						return err
					}
					holders = true
				}
				if item.projection.Row {
					if err := keepSearchCellPersonMentions(txApp, item.projection.PageID, item.projection.CellMentions); err != nil {
						return err
					}
				}
			}
			settled++
			written = append(written, done-1)
			workspaces[item.dirty.WorkspaceID] = true
		}
		for workspaceID := range workspaces {
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_search_workspace_state}} ([[workspace_id]], [[revision]], [[updated]])
				VALUES ({:workspaceId}, 1, {:now})
				ON CONFLICT ([[workspace_id]]) DO UPDATE SET
					[[revision]] = [[leafen_search_workspace_state]].[[revision]] + 1,
					[[updated]] = excluded.[[updated]]
			`).Bind(dbx.Params{"workspaceId": workspaceID, "now": time.Now().UnixMilli()}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	for _, index := range written {
		items[index].settled = true
	}
	if holders {
		wakeWorkspaceSearchIndexer(app)
	}
	return done, settled, nil
}

// prepareSearchPageProjection makes what a page's index row and chunks hold.
func prepareSearchPageProjection(projection *searchPageProjection) {
	projection.Chunks = searchChunksOf(projection.Body)
	projection.Words = searchPageWords(projection.BodyFolded, projection.Chunks)
	projection.Grams = searchTextGrams(projection.TitleFolded)
	if len(projection.Words) > searchPageWordsApartBytes {
		projection.Words, projection.WordsApart = "", true
	}
}

func upsertSearchPageProjection(app core.App, projection searchPageProjection) error {
	if projection.Chunks == nil && projection.Words == "" && projection.Grams == "" {
		prepareSearchPageProjection(&projection)
	}
	// The body is kept in the chunks, not again on the page's record; its
	// length is, as the chunks joined count it, for a result's preview.
	bodyRunes := -1
	if projection.Prepared {
		bodyRunes = projection.BodyRunes
	} else if len(projection.Chunks) > 0 {
		bodyRunes = len(projection.Chunks) - 1
		for _, chunk := range projection.Chunks {
			bodyRunes += utf8.RuneCountInString(chunk.Body)
		}
	}
	params := dbx.Params{
		"pageId": projection.PageID, "workspaceId": projection.WorkspaceID,
		"ownerId": projection.OwnerID, "title": projection.Title,
		"titleFolded": projection.TitleFolded, "sourceVersion": projection.SourceVersion, "partial": projection.Partial,
		"pageToken":      workspaceSearchPageToken(projection.PageID),
		"workspaceToken": workspaceSearchScopeToken(projection.WorkspaceID),
		"words":          projection.Words,
		"grams":          projection.Grams,
		"apart":          projection.WordsApart,
		"bodyRunes":      bodyRunes,
		"updated":        time.Now().UnixMilli(),
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_pages}}
			([[page_id]], [[workspace_id]], [[owner_id]], [[title]], [[title_folded]], [[body]], [[body_folded]], [[source_version]], [[partial]], [[chunked]], [[words_apart]], [[body_runes]], [[updated]])
		VALUES
			({:pageId}, {:workspaceId}, {:ownerId}, {:title}, {:titleFolded}, '', '', {:sourceVersion}, {:partial}, 1, {:apart}, {:bodyRunes}, {:updated})
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[owner_id]] = excluded.[[owner_id]],
			[[title]] = excluded.[[title]],
			[[title_folded]] = excluded.[[title_folded]],
			[[body]] = '',
			[[body_folded]] = '',
			[[source_version]] = excluded.[[source_version]],
			[[partial]] = excluded.[[partial]],
			[[chunked]] = 1,
			[[words_apart]] = excluded.[[words_apart]],
			[[body_runes]] = excluded.[[body_runes]],
			[[updated]] = excluded.[[updated]]
	`).Bind(params).Execute(); err != nil {
		return err
	}
	// A page indexed again goes back to its place in the index, so an edit
	// doesn't move it among its tier's matches, which stand in their rows'
	// order (workspace_search_tiers.go); a page new to the index takes the
	// next place (2026-11-11: a page given an icon or a sub-page went to the
	// end of its tier).
	place, err := keptSearchPageIndexPlace(app, projection.PageID)
	if err != nil {
		return err
	}
	params["place"] = place
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_pages_fts}}
			([[rowid]], [[page_id]], [[workspace_id]], [[page_token]], [[workspace_token]], [[title]], [[body]], [[grams]])
		VALUES
			(NULLIF({:place}, 0), {:pageId}, {:workspaceId}, {:pageToken}, {:workspaceToken}, {:titleFolded}, {:words}, {:grams})
	`).Bind(params).Execute(); err != nil {
		return err
	}
	noteSearchIndexWrite(len(projection.TitleFolded) + len(projection.Words) + len(projection.Grams))
	// Where the page stands in the index, which a search reads its page and
	// title by (schema_baseline.json).
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_search_pages}} SET [[fts_row]] = last_insert_rowid() WHERE [[page_id]] = {:pageId}
	`).Bind(params).Execute(); err != nil {
		return err
	}
	return writeSearchPageChunks(app, projection)
}

// keptSearchPageIndexPlace is the row a page's own row stood at in the
// index, once that row is let go of: 0 for a page new to the index, or one
// whose place another row holds (an index made anew), which takes the next.
func keptSearchPageIndexPlace(app core.App, pageID string) (int64, error) {
	var place int64
	if err := app.DB().NewQuery(`SELECT [[fts_row]] FROM {{leafen_search_pages}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).Row(&place); err != nil || place <= 0 {
		return 0, err
	}
	var held int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_search_pages_fts}} WHERE [[rowid]] = {:place}`).
		Bind(dbx.Params{"place": place}).Row(&held); err != nil || held > 0 {
		return 0, err
	}
	return place, nil
}

func workspaceSearchRevision(app core.App, workspaceID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[revision]]), 0)
		FROM {{leafen_search_workspace_state}}
		WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision)
	return revision, err
}

func workspaceHasDirtySearchPages(app core.App, workspaceID string) (bool, error) {
	var exists int
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_search_dirty}}
			WHERE [[workspace_id]] = {:workspaceId}
			LIMIT 1
		)
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&exists)
	return exists == 1, err
}
