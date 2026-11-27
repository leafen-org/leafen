package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A block row's change raises its workspace's content revision and its
// organization's discovery revision, so readers know to read again; each of
// those raises the bootstrap revision, and the discovery one the instance
// export's mark on its organization. A write transaction that changes many
// block rows of a workspace holds those revisions (holdBlockRevisions):
// while it holds them, its block rows mark the hold instead, and releasing
// the hold raises them once when a block row changed (2026-11-08: a write
// deleting a 20,100-block synced original raised them 20,100 times, half of
// its delete statement).
//
// A block row's deletion drops the row block Notion API ID it may have kept
// from before database page bodies became page bodies, unless page block
// trash keeps the block. Only a block with such an ID looks through the
// trash: every deletion looked through every trash snapshot of its
// workspace, and a deletion into the trash, whose snapshot holds the blocks
// it deletes, grew with the square of its blocks (5,000 blocks 11 s). Such a
// block still read every snapshot of its workspace; migration 822 has it
// look for its ID among the blocks each tombstone holds instead
// (schema_baseline.json).

const blockRevisionHoldsTable = "leafen_block_revision_holds"

// holdBlockRevisions holds the block revisions of a page's workspace for
// the rest of the write transaction app is in, and returns what releases
// the hold: it takes the transaction's error and gives it back, or the
// release's own. A hold an outer one already keeps is left to it, and
// outside a transaction nothing is held.
func holdBlockRevisions(app core.App, pageID string) (func(error) error, error) {
	keep := func(err error) error { return err }
	if !app.IsTransactional() {
		return keep, nil
	}
	workspaceID := ""
	err := app.DB().NewQuery(`
		SELECT [[workspace]].[[id]] FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[page]].[[workspace]]
		WHERE [[page]].[[id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return keep, nil
	}
	if err != nil {
		return nil, err
	}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_block_revision_holds}} ([[workspace_id]]) VALUES ({:workspaceId})
		ON CONFLICT ([[workspace_id]]) DO NOTHING
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
	if err != nil {
		return nil, err
	}
	if held, err := result.RowsAffected(); err != nil || held == 0 {
		return keep, err
	}
	return func(err error) error {
		// The release goes whether the work failed or not: a caller that
		// keeps the transaction after a failure must not keep the hold.
		_, releaseErr := app.DB().NewQuery(`
			DELETE FROM {{leafen_block_revision_holds}} WHERE [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
		if err == nil {
			return releaseErr
		}
		return err
	}, nil
}
