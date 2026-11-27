package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	commentTargetPage        = "page"
	commentTargetDatabaseRow = "database_row"
)

func ensureNativeCommentTargetNotionIdentities(app core.App, row storedComment) error {
	if row.TargetKind != commentTargetDatabaseRow {
		return nil
	}
	if _, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityRowPage, row.WorkspaceID, row.TargetID,
	); err != nil {
		return err
	}
	if row.BlockID != "" {
		// A block of a database page's body is a page block with the public
		// ID the block API gives it; a row block of its own name would
		// change that ID once someone commented on it.
		var bodyBlock int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": row.BlockID}).Row(&bodyBlock); err != nil {
			return err
		}
		if bodyBlock > 0 {
			return nil
		}
		if _, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityRowBlock, row.WorkspaceID, row.BlockID,
		); err != nil {
			return err
		}
	}
	return nil
}
