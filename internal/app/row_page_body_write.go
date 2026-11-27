package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A new page's first body, from the Public API, MCP, a template, a copy or an
// import, is written as a page's body is kept: its blocks among the page's
// blocks in the page's body order, their text in a new page document and
// their page mentions and files with them. The page's structure document is
// made from these blocks when the body is first edited. A body already
// written, or a page whose documents already exist, is not written over; an
// import writes a database page's body in parts, until the body is first
// edited. A page's history holds the body from its first version on.

type newRowPageBlock struct {
	ID       string
	ParentID string
	Position string
	Type     string
	// Content may carry the block's text in any stored form; the text goes
	// to the page's document.
	Content map[string]any
	// Spans, when set, is the block's text.
	Spans []graphTextSpan
	// Created and Updated, when set, keep an imported block's times.
	Created int64
	Updated int64
	// LinkedViews, when a rewrite of a copy sets it on a linked database,
	// gives each of its copied views what the copy names in place of what
	// the source's view names (copiedLinkedDatabaseViews).
	LinkedViews func(graphDatabaseView) (graphDatabaseView, error)
}

func writeNewRowPageBody(app core.App, rowID, actorID string, blocks []newRowPageBlock, now int64) error {
	return writeRowPageBody(app, rowID, actorID, blocks, now, false)
}

// writeImportedRowPageBody adds a part of an imported database page body.
func writeImportedRowPageBody(app core.App, rowID, actorID string, blocks []newRowPageBlock, now int64) error {
	return writeRowPageBody(app, rowID, actorID, blocks, now, true)
}

var errNewPageBodyWritten = errors.New("The page's body is already written.")

// writeRowPageBody writes a database page's first body.
func writeRowPageBody(app core.App, rowID, actorID string, blocks []newRowPageBlock, now int64, inParts bool) error {
	if len(blocks) == 0 {
		return nil
	}
	rowPage, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil || !isDatabaseRowPageRecord(rowPage) {
		return refusedUnlessStorage(errDatabaseRowPageNotFound, err)
	}
	if err := writeFirstPageBody(app, rowPage, actorID, blocks, now, inParts); err != nil {
		if errors.Is(err, errNewPageBodyWritten) {
			return errDatabaseRowPageConflict
		}
		return err
	}
	// Its history, from values changed before, starts anew with the body.
	if err := refreshDatabaseRowPageHistoryBaseline(app, rowID); err != nil {
		return err
	}
	return touchDatabaseRowPageBody(app, rowID, actorID, now)
}

// writeNewPageBody writes the first body of a page made just now.
func writeNewPageBody(app core.App, page *core.Record, actorID string, blocks []newRowPageBlock, now int64) error {
	if len(blocks) == 0 {
		return nil
	}
	if err := writeFirstPageBody(app, page, actorID, blocks, now, false); err != nil {
		return err
	}
	if err := refreshCreatedPageHistoryBaseline(app, page.Id); err != nil {
		return err
	}
	if isDatabaseRowPageRecord(page) {
		return touchDatabaseRowPageBody(app, page.Id, actorID, now)
	}
	return markSearchDirty(app, page)
}

// refreshCreatedPageHistoryBaseline makes the first body of a page made in
// this transaction part of its first version, as Notion's first version of a
// page made with content holds that content: the version stays, and its
// checkpoint and projection are taken again with the body. A page whose
// history has gone on since it was made starts it anew from now instead.
func refreshCreatedPageHistoryBaseline(app core.App, pageID string) error {
	var versions, later int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*), COALESCE(SUM([[version_cursor]] > 0), 0)
		FROM {{leafen_page_history_versions}} WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Row(&versions, &later); err != nil || versions == 0 {
		return err
	}
	if later > 0 {
		return recordDatabaseRowPageHistoryBaseline(app, pageID)
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]=0;
		DELETE FROM {{leafen_page_history_block_spans}} WHERE [[page_id]]={:pageId};
		DELETE FROM {{leafen_page_history_text_spans}} WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Execute(); err != nil {
		return err
	}
	if err := keepSeededPageStructure(app, pageID); err != nil {
		return err
	}
	if err := capturePageHistoryCheckpoint(app, pageID, 0, nil); err != nil {
		return err
	}
	return ensurePageHistoryProjectionBaseline(app, pageID)
}

// keepSeededPageStructure keeps the structure of a page made with its body,
// seeded from its blocks, as the page's own once the body is written: its
// first version's checkpoint and the page's later changes are then one
// document. A checkpoint that seeded one of its own, and a page that seeded
// another at its first change, held the body twice when the history played
// the later changes (2026-11-06: an agent's changes to a page it made with
// content couldn't be reverted).
func keepSeededPageStructure(app core.App, pageID string) error {
	_, found, err := loadMaterializedYgoStructuralDocument(app, pageID)
	if err != nil || found {
		return err
	}
	document := crdt.New()
	if err := seedStructuralGraphDocument(app, pageID, document); err != nil {
		return err
	}
	if len(document.GetMap(ygoBlockMetadataRoot).Keys()) == 0 {
		return nil
	}
	return replaceYgoStructuralSnapshot(app, pageID, crdt.EncodeStateAsUpdateV1(document, nil))
}

func writeFirstPageBody(app core.App, page *core.Record, actorID string, blocks []newRowPageBlock, now int64, inParts bool) (err error) {
	pageID := page.Id
	// The body's blocks raise their workspace's revisions once
	// (block_revision_hold_schema.go).
	release, err := holdBlockRevisions(app, pageID)
	if err != nil {
		return err
	}
	defer func() { err = release(err) }()
	var written, structural int
	if err := app.DB().NewQuery(`
		SELECT (SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId}) +
			(SELECT COUNT(*) FROM {{leafen_ygo_documents}} WHERE [[room]]={:pageId}),
			(SELECT COUNT(*) FROM {{leafen_ygo_structural_documents}} WHERE [[room]]={:pageId})
	`).Bind(dbx.Params{"pageId": pageID}).Row(&written, &structural); err != nil {
		return err
	}
	if structural != 0 || written != 0 && !inParts {
		return errNewPageBodyWritten
	}
	ownerID, workspaceID := page.GetString("owner"), page.GetString("workspace")
	doc, err := loadRowPageTextDocument(app, pageID)
	if err != nil {
		return err
	}
	for _, block := range blocks {
		content := make(map[string]any, len(block.Content))
		for key, value := range block.Content {
			content[key] = value
		}
		spans := block.Spans
		if spans == nil {
			spans = databaseRowBlockTextSpans(content)
		}
		for _, key := range rowBodyStoredTextKeys {
			delete(content, key)
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		if len(spans) > 0 {
			replaceNotionBlockResponseText(doc, block.ID, spans)
			for _, target := range blockTextPageTargets(doc.GetText(ygoBlockTextRootPrefix + block.ID)) {
				if _, err := app.DB().NewQuery(`
					INSERT OR IGNORE INTO {{leafen_block_text_page_references}} ([[block_id]], [[target_page_id]])
					VALUES ({:blockId}, {:targetId})
				`).Bind(dbx.Params{"blockId": block.ID, "targetId": target}).Execute(); err != nil {
					return err
				}
			}
		}
		created, updated := now, now
		if block.Created > 0 {
			created, updated = block.Created, max(block.Updated, block.Created)
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_blocks}}
				([[id]], [[owner]], [[workspace_id]], [[page_id]], [[parent_id]], [[position]], [[type]], [[content]],
				 [[created]], [[updated]], [[created_by]], [[last_edited_by]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:parentId}, {:position}, {:type}, {:content},
				{:created}, {:updated}, {:actorId}, {:actorId})
		`).Bind(dbx.Params{
			"id": block.ID, "owner": ownerID, "workspaceId": workspaceID, "pageId": pageID,
			"parentId": block.ParentID, "position": block.Position, "type": block.Type,
			"content": string(encoded), "created": created, "updated": updated, "actorId": actorID,
		}).Execute(); err != nil {
			return err
		}
		parentID := block.ParentID
		if err := replaceProductFileBlockReference(app, workspaceID, pageID, &graphBlock{
			ID: block.ID, ParentID: &parentID, Position: block.Position, Type: block.Type, Content: encoded,
		}); err != nil {
			return err
		}
	}
	ids := make([]string, len(blocks))
	for index, block := range blocks {
		ids[index] = block.ID
	}
	if docHasText(doc, ids) {
		if err := replaceYgoSnapshot(app, pageID, crdt.EncodeStateAsUpdateV1(doc, nil)); err != nil {
			return err
		}
	}
	return nil
}
