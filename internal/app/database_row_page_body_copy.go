package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's body is copied block by block under new IDs, with its
// text, for its duplicate, a template's use or duplicate, and the database
// pages and templates a page or database copy takes.

// rowPageBodySynced is how a copy of a database page's body takes the synced
// block originals in it (synced_block_duplication.go).
type rowPageBodySynced int

const (
	// rowPageBodySyncedNew takes each original as a new original on the
	// copy, with its content, the body's copies of it showing the new one.
	rowPageBodySyncedNew rowPageBodySynced = iota
	// rowPageBodySyncedAsCopies takes each original as a copy of it, its own
	// blocks left out: the app's template use, as Notion's app's.
	rowPageBodySyncedAsCopies
)

// duplicateDatabaseRowPageBody gives a template's duplicate its source's
// body: the same blocks under new IDs, with their text, each synced block
// original a copy of it, as Notion's app duplicates a template (seen
// 2026-11-14).
func duplicateDatabaseRowPageBody(
	app core.App,
	sourceRowID, targetRowID, workspaceID, actorID string,
) error {
	return duplicateDatabaseRowPageBodyText(app, sourceRowID, targetRowID, workspaceID, actorID, nil, rowPageBodySyncedAsCopies)
}

// duplicateDatabaseRowPageBodyFrom gives a copied database page its source's
// body, from a source in another workspace when it is: the body is read
// there, and passed through each rewrite when it is set.
func duplicateDatabaseRowPageBodyFrom(
	app core.App,
	sourceRowID, sourceWorkspaceID, targetRowID, actorID string,
	rewrite ...func([]newRowPageBlock) error,
) error {
	return duplicateDatabaseRowPageBodyRewritten(
		app, sourceRowID, targetRowID, sourceWorkspaceID, actorID, nil, rowPageBodySyncedNew, rewrite...,
	)
}

// duplicateDatabaseRowPageBodyText copies a database page's body, passing
// each block's text through text when it is set and taking its synced block
// originals as synced says; its linked databases come with their views
// (database_template_linked_views.go), and its own databases as copies of
// them, after the rest (database_template_body_databases.go).
func duplicateDatabaseRowPageBodyText(
	app core.App,
	sourceRowID, targetRowID, workspaceID, actorID string,
	text func([]graphTextSpan) []graphTextSpan,
	synced rowPageBodySynced,
) error {
	return duplicateDatabaseRowPageBodyRewritten(app, sourceRowID, targetRowID, workspaceID, actorID, text, synced)
}

// duplicateDatabaseRowPageBodyRewritten is duplicateDatabaseRowPageBodyText
// with the copied blocks passed through each rewrite before they are written.
func duplicateDatabaseRowPageBodyRewritten(
	app core.App,
	sourceRowID, targetRowID, workspaceID, actorID string,
	text func([]graphTextSpan) []graphTextSpan,
	synced rowPageBodySynced,
	rewrites ...func([]newRowPageBlock) error,
) error {
	copied := func(id string) string { return rowPageBodyCopyBlockID(targetRowID, id) }
	body, err := readDatabaseRowPageBodyCopy(app, sourceRowID, workspaceID, targetRowID, copied, text, synced)
	if err != nil || len(body) == 0 {
		return err
	}
	for _, rewrite := range rewrites {
		if rewrite == nil {
			continue
		}
		if err := rewrite(body); err != nil {
			return err
		}
	}
	target, err := app.FindRecordById(pagesCollection, targetRowID)
	if err != nil {
		return err
	}
	source := func(id string) string { return strings.TrimPrefix(id, targetRowID+":") }
	// A database of the body's own comes as a copy of it, after the rest
	// (database_template_body_databases.go).
	var databases []rowPageOwnDatabase
	if rowPageOwnDatabasesCopied(target) {
		body, databases = takeRowPageOwnDatabases(body, source, "")
	}
	views, err := copiedLinkedDatabaseViews(app, sourceRowID, targetRowID, target.GetString("owner"), body, source, copied)
	if err != nil {
		return err
	}
	// A template's sub-pages come as copies, begun before its databases
	// (database_row_page_sub_page_copies.go).
	subPages, err := rowPageBodySubPageCopies(app, sourceRowID, target, body, source)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		if err := writeNewRowPageBody(app, targetRowID, actorID, body, time.Now().UTC().UnixMilli()); err != nil {
			return err
		}
		if err := writeCopiedLinkedDatabaseViews(app, target.GetString("owner"), target.GetString("workspace"), targetRowID, body, views); err != nil {
			return err
		}
	}
	return queueRowPageOwnDatabaseCopies(app, actorID, sourceRowID, targetRowID, databases, subPages)
}

// readDatabaseRowPageBodyCopy reads a database page's body as the page
// targetPageID takes it: each block under the ID copied gives it, in its
// order, with its text passed through text when it is set. A synced block
// original comes as synced says, and a sub-page block as a link to the
// sub-page, which stays, or for a template's which a copy of it takes the
// place of (database_row_page_sub_page_copies.go).
func readDatabaseRowPageBodyCopy(
	app core.App,
	sourceRowID, workspaceID, targetPageID string,
	copied func(string) string,
	text func([]graphTextSpan) []graphTextSpan,
	synced rowPageBodySynced,
) ([]newRowPageBlock, error) {
	source := []struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
		Position string `db:"position"`
		Type     string `db:"type"`
		Content  string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:sourceRowId} AND [[workspace_id]]={:workspaceId}
		ORDER BY [[parent_id]], [[position]], [[id]]
	`).Bind(dbx.Params{"sourceRowId": sourceRowID, "workspaceId": workspaceID}).All(&source); err != nil {
		return nil, err
	}
	if len(source) == 0 {
		return nil, nil
	}
	textDocument, err := loadRowPageTextDocument(app, sourceRowID)
	if err != nil {
		return nil, err
	}
	copiedID := func(id string) string {
		if id == "" {
			return ""
		}
		return copied(id)
	}
	parents := make(map[string]string, len(source))
	originals := map[string]bool{}
	for _, block := range source {
		parents[block.ID] = block.ParentID
		if syncedBlockIsOriginal(block.Type, block.Content) {
			originals[block.ID] = true
		}
	}
	// Taken as copies, an original's content stays the original's: the
	// body holds a copy of it instead.
	asCopies := synced == rowPageBodySyncedAsCopies
	// Up through every ancestor, however deep; each one once.
	insideOriginal := func(id string) bool {
		seen := map[string]bool{}
		for parent := parents[id]; parent != "" && !seen[parent]; parent = parents[parent] {
			seen[parent] = true
			if originals[parent] {
				return true
			}
		}
		return false
	}
	body := make([]newRowPageBlock, 0, len(source))
	for _, block := range source {
		if asCopies && insideOriginal(block.ID) {
			continue
		}
		if asCopies && originals[block.ID] {
			block.Content = syncedBlockCopyOf(block.ID, sourceRowID)
		}
		if isStoredChildPageBlock(block.ID, block.Type, block.Content) {
			// The copy's sub-page blocks link to the sub-pages, a template's
			// until their copies take their places.
			block.Type = graphBlockTypeLinkToPage
		}
		content := map[string]any{}
		if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
			return nil, err
		}
		spans, _ := rowPageBlockText(textDocument, block.ID)
		if spans == nil {
			spans = []graphTextSpan{}
		}
		if text != nil {
			spans = text(spans)
		}
		body = append(body, newRowPageBlock{
			ID: copiedID(block.ID), ParentID: copiedID(block.ParentID), Position: block.Position,
			Type: block.Type, Content: content, Spans: spans,
		})
	}
	if !asCopies {
		// Each original is a new one on the copy, which its copies show.
		copiedOriginals := make(map[string]copiedSyncedBlock, len(originals))
		for id := range originals {
			copiedOriginals[id] = copiedSyncedBlock{SourceID: id, TargetID: copiedID(id), TargetPageID: targetPageID}
		}
		for _, block := range body {
			pointSyncedCopyAtCopiedOriginal(block.Content, copiedOriginals)
		}
	}
	return body, nil
}

// duplicateDatabaseRowPagePresentationAndBody copies a row page's look and
// body onto its duplicate, the body passed through each rewrite.
func duplicateDatabaseRowPagePresentationAndBody(
	app core.App,
	sourceRowID, targetRowID, workspaceID, actorID string,
	rewrite ...func([]newRowPageBlock) error,
) error {
	return duplicateDatabaseRowPagePresentationAndBodyFrom(
		app, sourceRowID, workspaceID, targetRowID, workspaceID, actorID, nil, rewrite...,
	)
}

// pointRowPageBodySyncedCopiesAtRows is a body copy's rewrite for a
// duplicate that copied the rows copiedRows names (source to copy): its
// synced copies of originals on those rows show the originals' copies.
func pointRowPageBodySyncedCopiesAtRows(app core.App, copiedRows map[string]string) func([]newRowPageBlock) error {
	return pointRowPageBodySyncedCopies(func(contents []map[string]any) (map[string]copiedSyncedBlock, error) {
		return syncedOriginalsOnCopiedRows(app, syncedCopyIDs(contents), func([]string) (map[string]string, error) {
			return copiedRows, nil
		})
	})
}

// duplicateDatabaseRowPagePresentationAndBodyFrom copies a row page from a
// source in another workspace; files, which stay in their workspace, are the
// copies files names when it is set.
func duplicateDatabaseRowPagePresentationAndBodyFrom(
	app core.App,
	sourceRowID, sourceWorkspaceID, targetRowID, workspaceID, actorID string,
	files func(string) string,
	rewrite ...func([]newRowPageBlock) error,
) error {
	found, err := copyRowPageRecordPresentationFrom(app, sourceRowID, sourceWorkspaceID, targetRowID, workspaceID, false)
	if err != nil {
		return err
	}
	if !found {
		return errDatabaseRowPageConflict
	}
	var presentation struct {
		ContainerPageID string `db:"container_page_id"`
		Icon            string `db:"icon"`
		IconType        string `db:"icon_type"`
		Cover           string `db:"cover"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[row_page]].[[container_page_id]],[[row_record]].[[icon]],[[row_record]].[[icon_type]],
			[[row_record]].[[cover]]
		FROM {{leafen_database_row_pages}} AS [[row_page]]
		JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]]=[[row_page]].[[row_id]]
		WHERE [[row_page]].[[row_id]]={:rowId} AND [[row_page]].[[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{
		"rowId": targetRowID, "workspaceId": workspaceID,
	}).One(&presentation); err != nil {
		return err
	}
	if files != nil {
		if presentation.IconType == graphDataSourceIconImage {
			presentation.Icon = files(presentation.Icon)
			if presentation.Icon == "" {
				presentation.IconType = "none"
			}
		}
		presentation.Cover = files(presentation.Cover)
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_pages}} SET [[icon]]={:icon},[[icon_type]]={:iconType},[[cover]]={:cover}
			WHERE [[id]]={:rowId}
		`).Bind(dbx.Params{
			"icon": presentation.Icon, "iconType": presentation.IconType,
			"cover": presentation.Cover, "rowId": targetRowID,
		}).Execute(); err != nil {
			return err
		}
	}
	if err := replaceProductFileDatabaseRowPageIconReference(
		app, workspaceID, presentation.ContainerPageID, targetRowID,
		presentation.Icon, presentation.IconType,
	); err != nil {
		return err
	}
	if err := replaceProductFilePageReferences(app, workspaceID, targetRowID, "", "none", presentation.Cover); err != nil {
		return err
	}
	return duplicateDatabaseRowPageBodyFrom(app, sourceRowID, sourceWorkspaceID, targetRowID, actorID, rewrite...)
}
