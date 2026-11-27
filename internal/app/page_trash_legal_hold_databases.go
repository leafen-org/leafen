package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The databases in a workspace's trash that an active legal hold keeps: one
// whose page is held — the page itself, its owner as custodian, the
// workspace or the whole organization — as the trash's own retention spares
// it (block_trash.go, database_container_trash.go). The trash's lines say so
// before the press, and deleting one for good is refused while it lasts
// (2026-11-15: an inline database's line kept a live delete, and the deletion
// went through under the hold). Without the organization module an active
// hold keeps every one.

// trashDatabaseHoldAll is whether every database in the trash is kept (no
// organization module and a hold active), and none is when no table of holds
// exists yet.
func trashDatabaseHoldAll(app core.App) (all bool, none bool, err error) {
	if !tableKnown(app, organizationLegalHoldsTable) {
		return false, true, nil
	}
	if organizationPolicy.PermanentDeleteHold != nil {
		return false, false, nil
	}
	kept, err := legalHoldsKeptWithoutModule(app)
	return kept, !kept, err
}

// heldTrashPages keeps those of the pages an active hold keeps.
func heldTrashPages(app core.App, workspaceID string, pageIDs []string) (map[string]bool, error) {
	held := map[string]bool{}
	if len(pageIDs) == 0 {
		return held, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	kept := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[candidate]].[[value]] FROM json_each({:pages}) AS [[candidate]]
		WHERE ` + legalHoldRetainsPageSQL("[[candidate]].[[value]]", "{:workspaceId}") + `
	`).Bind(dbx.Params{"pages": string(encoded), "workspaceId": workspaceID}).Column(&kept); err != nil {
		return nil, err
	}
	for _, pageID := range kept {
		held[pageID] = true
	}
	return held, nil
}

// trashedDatabasesKeptByLegalHold names the held databases in the trash by
// the ID the trash lists them with (their block's, or a kept source's own).
func trashedDatabasesKeptByLegalHold(app core.App, workspaceID, readerID string) ([]string, error) {
	ids := []string{}
	all, none, err := trashDatabaseHoldAll(app)
	if err != nil || none {
		return ids, err
	}
	type listed struct{ id, pageID string }
	items := []listed{}
	stored := []struct {
		BlockID string `db:"block_id"`
		PageID  string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block_id]],[[page_id]] FROM {{leafen_deleted_databases}} WHERE [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&stored); err != nil {
		return nil, err
	}
	for _, row := range stored {
		items = append(items, listed{row.BlockID, row.PageID})
	}
	for after := (deletedDatabaseCursor{}); ; {
		containers, err := listTrashedDatabaseContainers(app, workspaceID, "", after, deletedDatabaseListWindow)
		if err != nil {
			return nil, err
		}
		for _, item := range containers {
			items = append(items, listed{item.RootBlockID, item.PageID})
		}
		if len(containers) < deletedDatabaseListWindow {
			break
		}
		last := containers[len(containers)-1]
		after = deletedDatabaseCursor{At: last.DeletedAt, ID: last.RootBlockID}
	}
	pageIDs := []string{}
	for _, item := range items {
		pageIDs = append(pageIDs, item.pageID)
	}
	held := map[string]bool{}
	if !all {
		if held, err = heldTrashPages(app, workspaceID, pageIDs); err != nil {
			return nil, err
		}
	}
	// Of those, the ones on a page the reader reads, judged together
	// (owner_listed_pages.go).
	heldPageIDs := []string{}
	for _, item := range items {
		if all || held[item.pageID] {
			heldPageIDs = append(heldPageIDs, item.pageID)
		}
	}
	reads, err := listedPagesPersonReads(app, readerID, heldPageIDs)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if (all || held[item.pageID]) && reads[item.pageID] {
			ids = append(ids, item.id)
		}
	}
	return ids, nil
}

// legalHoldKeepsTrashedDatabase is whether an active hold keeps the database
// in the trash on this page from deletion for good.
func legalHoldKeepsTrashedDatabase(app core.App, workspaceID, pageID string) (bool, error) {
	all, none, err := trashDatabaseHoldAll(app)
	if err != nil || none || all {
		return all, err
	}
	held, err := heldTrashPages(app, workspaceID, []string{pageID})
	return held[pageID], err
}

// refuseHeldTrashedDatabase refuses deleting for good a database in the
// trash whose page an active hold keeps; one not found here is left to the
// deletion's own reading.
func refuseHeldTrashedDatabase(app core.App, userID, workspaceID, blockID string) error {
	pageID := ""
	stored := storedDeletedDatabase{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:blockId} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"blockId": blockID, "workspaceId": workspaceID}).One(&stored); err == nil {
		pageID = stored.PageID
	} else if item, err := loadTrashedDatabaseContainer(app, userID, workspaceID, blockID); err == nil {
		pageID = item.PageID
	}
	if pageID == "" {
		return nil
	}
	held, err := legalHoldKeepsTrashedDatabase(app, workspaceID, pageID)
	if err != nil {
		return err
	}
	if held {
		return errLegalHoldPreventsPermanentDelete
	}
	return nil
}
