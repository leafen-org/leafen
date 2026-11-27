package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// cleanNotificationsForPermanentlyDeletedPage owns the materialized Inbox
// cleanup decision. The page lifecycle supplies its durable fence and bounded
// continuation, while this owner retains the only direct notification delete.
func cleanNotificationsForPermanentlyDeletedPage(
	app core.App,
	pageID string,
	limit int,
) (bool, error) {
	if pageID == "" || limit <= 0 {
		return false, errPagePermanentDeleteUnavailable
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notifications}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_notifications}}
			INDEXED BY [[idx_leafen_notifications_page_cleanup]]
			WHERE [[page_id]] = {:pageId}
			ORDER BY [[id]] LIMIT {:limit}
		)
	`).Bind(dbx.Params{"pageId": pageID, "limit": limit}).Execute()
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected >= int64(limit), nil
}
