package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An object's API identity (a data source's collection:// ID, a comment's or
// a file upload's ID) is made from its workspace and its own ID
// (notionCompatibleObjectID), so an object moved to another workspace
// (page_workspace_move.go) would answer with a new one there. It keeps the
// one it had instead, which agents and their instructions name: the identity
// it was given first, in the workspace it was in. That identity still
// resolves to it (resolveNotionObjectIdentity checks it against the
// workspace it was made in), and no other object names the same ID.

// movedNotionObjectIdentity is the identity an object was given in another
// workspace than the one it is in now, the first if several; ok is false
// for one that has none elsewhere.
func movedNotionObjectIdentity(app core.App, objectKind, workspaceID, internalID string) (string, bool, error) {
	kept := ""
	err := app.DB().NewQuery(`
		SELECT [[public_id]] FROM {{leafen_notion_object_identities}}
			INDEXED BY [[idx_leafen_notion_object_identities_moved]]
		WHERE [[internal_id]] = {:internalId} AND [[object_kind]] = {:objectKind} AND [[workspace_id]] != {:workspaceId}
		ORDER BY [[created]], [[public_id]] LIMIT 1
	`).Bind(dbx.Params{"internalId": internalID, "objectKind": objectKind, "workspaceId": workspaceID}).Row(&kept)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return kept, err == nil, err
}
