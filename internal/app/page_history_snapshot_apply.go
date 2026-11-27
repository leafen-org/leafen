package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Applying a write's history deltas to the page's history projection: the
// blocks' spans, their text and the typing spans a checkpoint settles
// (page_history_snapshot_projection.go captures the deltas).

func applyPageHistoryGraphDeltas(
	app core.App, workspaceID, pageID string, version int, deltas []pageHistoryGraphDelta,
) error {
	// The blocks deleted one after another end their spans together: two
	// statements for each, a 20,100-block deletion ran 40,000 of them while
	// it held the one write connection (2026-11-08).
	deleted := []string{}
	endDeleted := func() error {
		if len(deleted) == 0 {
			return nil
		}
		encoded, err := json.Marshal(deleted)
		if err != nil {
			return err
		}
		deleted = deleted[:0]
		for _, table := range []string{pageHistoryBlockSpansTable, pageHistoryTextSpansTable} {
			if _, err := app.DB().NewQuery(`
				UPDATE {{` + table + `}} SET [[valid_through]]={:through}
				WHERE [[page_id]]={:pageId} AND [[valid_through]] IS NULL
					AND [[block_id]] IN (SELECT [[value]] FROM json_each({:blockIds}))
			`).Bind(dbx.Params{"through": version - 1, "pageId": pageID, "blockIds": string(encoded)}).Execute(); err != nil {
				return err
			}
		}
		return nil
	}
	for _, delta := range deltas {
		if delta.Deleted {
			deleted = append(deleted, delta.BlockID)
			continue
		}
		if err := endDeleted(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_history_block_spans}} SET [[valid_through]]={:through}
			WHERE [[page_id]]={:pageId} AND [[block_id]]={:blockId} AND [[valid_through]] IS NULL
		`).Bind(dbx.Params{"through": version - 1, "pageId": pageID, "blockId": delta.BlockID}).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().Insert(pageHistoryBlockSpansTable, dbx.Params{
			"workspace_id": workspaceID, "page_id": pageID, "block_id": delta.BlockID,
			"valid_from": version, "valid_through": nil, "after_id": delta.AfterID,
			"parent_id": delta.ParentID, "type": delta.Type, "content": string(delta.Content),
			"created_at": delta.Created, "updated_at": delta.Updated,
		}).Execute(); err != nil {
			return err
		}
	}
	return endDeleted()
}

// setPageHistoryBlockText makes a block's text in the history from a version
// on the given runs.
func setPageHistoryBlockText(
	app core.App, workspaceID, pageID, blockID string, version int, runs []pageHistoryTextRun,
) error {
	text := pageHistoryTextRunsText(runs)
	rich, err := encodePageHistoryTextRuns(runs)
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_page_history_text_spans}}
		SET [[plain_text]]={:text},[[rich_text]]={:rich} WHERE [[page_id]]={:pageId} AND [[block_id]]={:blockId}
		AND [[valid_through]] IS NULL AND [[valid_from]]={:version}`).Bind(dbx.Params{
		"text": text, "rich": rich, "pageId": pageID, "blockId": blockID, "version": version,
	}).Execute()
	if err != nil {
		return err
	}
	if updated, err := result.RowsAffected(); err != nil || updated > 0 {
		return err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_history_text_spans}}
		SET [[valid_through]]={:through} WHERE [[page_id]]={:pageId}
		AND [[block_id]]={:blockId} AND [[valid_through]] IS NULL`).Bind(dbx.Params{
		"through": version - 1, "pageId": pageID, "blockId": blockID,
	}).Execute(); err != nil {
		return err
	}
	_, err = app.DB().Insert(pageHistoryTextSpansTable, dbx.Params{
		"workspace_id": workspaceID, "page_id": pageID, "block_id": blockID,
		"valid_from": version, "valid_through": nil, "plain_text": text, "rich_text": rich,
	}).Execute()
	return err
}

// settlePageHistoryTextSpans gives each block, at a version with a
// checkpoint, the text the checkpoint keeps where the history shows other
// text: typing writes none (ygo_persistence.go), and history that fell
// behind a page's text catches up. A block typed in since the checkpoint
// before takes it from its last typing's version, which is its last edit
// (page_block_last_edit.go) — the text it kept from there to the checkpoint.
// The checkpoint is the one documents has just taken, else the stored one.
func settlePageHistoryTextSpans(
	app core.App, workspaceID, pageID string, version int, documents *pageHistoryDocuments,
) error {
	blocksJSON, document, found, err := documents.checkpointAt(app, pageID, version, true)
	if err != nil || !found {
		return err
	}
	blocks := []pageHistoryBlockState{}
	if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
		return err
	}
	shown := []struct {
		BlockID   string `db:"block_id"`
		From      int    `db:"valid_from"`
		PlainText string `db:"plain_text"`
		RichText  string `db:"rich_text"`
	}{}
	// The open spans from their own index: the planner took the history's
	// and read every span the page ever had (2026-11-18).
	if err := app.DB().NewQuery(`SELECT [[block_id]],[[valid_from]],[[plain_text]],[[rich_text]] FROM {{leafen_page_history_text_spans}}
		INDEXED BY [[idx_leafen_page_history_text_spans_open]]
		WHERE [[page_id]]={:pageId} AND [[valid_through]] IS NULL`).Bind(
		dbx.Params{"pageId": pageID},
	).All(&shown); err != nil {
		return err
	}
	texts := make(map[string][2]string, len(shown))
	from := make(map[string]int, len(shown))
	for _, span := range shown {
		texts[span.BlockID] = [2]string{span.PlainText, span.RichText}
		from[span.BlockID] = span.From
	}
	// The typing since the checkpoint before is read only once a block's
	// text differs: read at every checkpoint and restore, its events grew
	// with the page's history where no block needed them (2026-11-18).
	var typed map[string]int
	for _, block := range blocks {
		runs := pageHistoryBlockTextRuns(document, block.ID)
		rich, err := encodePageHistoryTextRuns(runs)
		if err != nil {
			return err
		}
		if texts[block.ID] == [2]string{pageHistoryTextRunsText(runs), rich} {
			continue
		}
		if typed == nil {
			if typed, err = pageHistoryTypingSinceCheckpoint(app, pageID, version); err != nil {
				return err
			}
		}
		since := version
		if last, found := typed[block.ID]; found && last > from[block.ID] {
			since = last
		}
		if err := setPageHistoryBlockText(app, workspaceID, pageID, block.ID, since, runs); err != nil {
			return err
		}
	}
	return nil
}

// pageHistoryTypingSinceCheckpoint is pageHistoryLastTyping after the
// checkpoint before a version through the version.
func pageHistoryTypingSinceCheckpoint(app core.App, pageID string, version int) (map[string]int, error) {
	var previous int
	if err := app.DB().NewQuery(`SELECT COALESCE(MAX([[version_cursor]]),-1) FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]<{:version}`).Bind(
		dbx.Params{"pageId": pageID, "version": version},
	).Row(&previous); err != nil {
		return nil, err
	}
	return pageHistoryLastTyping(app, pageID, previous, version)
}

// pageHistoryLastTyping is, for each block typed in after one version
// through another, the version of its last typing.
func pageHistoryLastTyping(app core.App, pageID string, after, through int) (map[string]int, error) {
	events := []struct {
		Cursor  int    `db:"version_cursor"`
		Payload []byte `db:"payload"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[version_cursor]],[[payload]] FROM {{leafen_page_history_events}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:after} AND [[version_cursor]]<={:through}
			AND [[kind]]={:kind}
	`).Bind(dbx.Params{
		"pageId": pageID, "after": after, "through": through, "kind": pageHistoryEventCollaboration,
	}).All(&events); err != nil {
		return nil, err
	}
	typed := map[string]int{}
	for _, event := range events {
		envelope, err := pageHistoryEventPayload(event.Payload)
		if err != nil {
			return nil, err
		}
		for _, delta := range envelope.TextDeltas {
			typed[delta.BlockID] = max(typed[delta.BlockID], event.Cursor)
		}
	}
	return typed, nil
}
