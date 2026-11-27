package app

import (
	"database/sql"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const pagePositionIndex = "idx_leafen_pages_workspace_parent_position"

// firstPageSiblingPosition is a place before every page under the parent.
func firstPageSiblingPosition(app core.App, workspaceID string, parentID string) (float64, error) {
	var position float64
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[position]]), 1) - 1
		FROM {{leafen_pages}}
		WHERE [[workspace]] = {:workspaceId} AND [[parent_id]] = {:parentId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "parentId": parentID}).Row(&position)
	return position, err
}

func nextPageSiblingPosition(app core.App, workspaceID string, parentID string) (float64, error) {
	var position float64
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[position]]), 0) + 1
		FROM {{leafen_pages}}
		WHERE [[workspace]] = {:workspaceId} AND [[parent_id]] = {:parentId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "parentId": parentID}).Row(&position)
	return position, err
}

// pageSiblingPositionAfter is a place right after a page among its siblings:
// halfway to the next one, or one past it when it is the last.
func pageSiblingPositionAfter(app core.App, workspaceID string, parentID string, position float64) (float64, error) {
	var next sql.NullFloat64
	err := app.DB().NewQuery(`
		SELECT MIN([[position]]) FROM {{leafen_pages}}
		WHERE [[workspace]] = {:workspaceId} AND [[parent_id]] = {:parentId} AND [[position]] > {:position}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "parentId": parentID, "position": position}).Row(&next)
	if err != nil || !next.Valid {
		return position + 1, err
	}
	return position + (next.Float64-position)/2, nil
}
