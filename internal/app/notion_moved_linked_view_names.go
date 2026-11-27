package app

import (
	"encoding/json"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Before 2026-11-21 a Notion move gave a linked database Notion never named
// the name "Untitled", which Notion's API calls it by, as its own
// (notion_import_linked_views.go): fetch wrote it in its page's tag and its
// head, and its page opened alone showed it as its title. Migration 843
// takes that name away from each linked database block a move made, its
// job's records still here, that nobody changed since the move wrote it —
// its block written no later than its job's last turn — so it goes by its
// source's name as one made in Leafen does. One changed since keeps what it
// has (a person may have named it so); one whose move's records are gone
// can be renamed by hand.

type movedUntitledLinkedView struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	ParentID     string `db:"parent_id"`
	Position     string `db:"position"`
	Content      string `db:"content"`
	LastEditedBy string `db:"last_edited_by"`
	Updated      int64  `db:"updated"`
}

// clearMovedUntitledLinkedViewNames is migration 843's content.
func clearMovedUntitledLinkedViewNames(app core.App) error {
	views := []movedUntitledLinkedView{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[block]].[[id]], [[block]].[[owner]], [[block]].[[workspace_id]], [[block]].[[page_id]],
			[[block]].[[parent_id]], [[block]].[[position]], [[block]].[[content]], [[block]].[[last_edited_by]], [[block]].[[updated]]
		FROM {{leafen_blocks}} AS [[block]]
		JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[kind]] = 'block' AND [[mapping]].[[native_id]] = [[block]].[[id]]
		JOIN {{leafen_jobs}} AS [[job]] ON [[job]].[[id]] = [[mapping]].[[job_id]] AND [[job]].[[workspace_id]] = [[block]].[[workspace_id]]
		WHERE [[block]].[[type]] = 'inline_database' AND json_valid([[block]].[[content]])
			AND json_extract([[block]].[[content]], '$.linkedDatabaseSource') = 1
			AND json_extract([[block]].[[content]], '$.linkedTitle') = {:untitled}
			AND json_type([[block]].[[content]], '$.linkedTitleSpans') IS NULL
			AND [[block]].[[updated]] <= [[job]].[[updated]]
		ORDER BY [[block]].[[page_id]], [[block]].[[id]]
	`).Bind(dbx.Params{"untitled": notionUnnamedLinkedDatabaseTitle}).All(&views); err != nil {
		return err
	}
	cleared := 0
	for start := 0; start < len(views); {
		end := start + 1
		for end < len(views) && views[end].PageID == views[start].PageID {
			end++
		}
		if err := clearMovedUntitledLinkedViewNamesOnPage(app, views[start:end]); err != nil {
			return fmt.Errorf("linked databases moved unnamed on page %s: %w", views[start].PageID, err)
		}
		cleared += end - start
		start = end
	}
	if cleared > 0 {
		app.Logger().Info("linked databases a Notion move named Untitled left unnamed", "databases", cleared)
	}
	return nil
}

// clearMovedUntitledLinkedViewNamesOnPage takes the name away from a page's
// linked databases and gives their blocks back their last edits: they stand
// as the move meant them, unedited.
func clearMovedUntitledLinkedViewNamesOnPage(app core.App, views []movedUntitledLinkedView) error {
	first := views[0]
	batch, err := loadStructuralMutationBatch(app, first.PageID, map[string]*structuralMutationBatch{})
	if err != nil {
		return err
	}
	operations := make([]graphOperation, 0, len(views))
	for _, view := range views {
		content := map[string]any{}
		if err := json.Unmarshal([]byte(view.Content), &content); err != nil {
			return err
		}
		delete(content, "linkedTitle")
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		block := &graphBlock{
			ID: view.ID, Type: graphBlockTypeInlineDatabase, Position: view.Position, Content: encoded,
			BaseContent: json.RawMessage(view.Content),
		}
		if view.ParentID != "" {
			parentID := view.ParentID
			block.ParentID = &parentID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, first.Owner, first.WorkspaceID, first.PageID, first.Owner, operations, false, nil,
	); err != nil {
		return err
	}
	if err := saveServerStructuralBatch(app, first.Owner, first.PageID, first.Owner, "moved-linked-views-unnamed", batch, nil); err != nil {
		return err
	}
	for _, view := range views {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_blocks}} SET [[last_edited_by]] = {:by}, [[updated]] = {:updated}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}
		`).Bind(dbx.Params{
			"by": view.LastEditedBy, "updated": view.Updated, "owner": view.Owner, "pageId": view.PageID, "id": view.ID,
		}).Execute(); err != nil {
			return err
		}
	}
	// Its history keeps the blocks as they now stand.
	return recordPageHistoryRewrite(app, first.PageID, first.Owner, true)
}

// validateMovedUntitledLinkedViewNames is its check: the tables it reads and
// writes are there.
func validateMovedUntitledLinkedViewNames(app core.App) error {
	for _, table := range []string{"leafen_blocks", "leafen_notion_import_mappings", "leafen_jobs", "leafen_ygo_structural_documents"} {
		if !app.HasTable(table) {
			return fmt.Errorf("linked databases moved unnamed: the table %s is missing", table)
		}
	}
	return nil
}
