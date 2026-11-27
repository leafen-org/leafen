package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database holds at most 100 views, as Notion's: a change that would make
// its 101st is refused with databaseViewLimitMessage, whoever makes it — the
// views API (seen 2026-11-12: POST /v1/views at 100 answers 400 once
// everything else the request names has passed), the screen, an agent or an
// import. Its tabs count, feeds and
// dashboards too; a dashboard's widgets don't (seen 2026-11-12: a widget
// added to a dashboard of a database at 100 is made, and the database still
// lists 100 views). A view written again, moved or restored with its page
// isn't a new one, and a workspace restore brings back what it had.

const (
	databaseViewLimit        = 100
	databaseViewLimitMessage = "A database holds at most 100 views"
)

// newGraphViewIDs are the views a change writes that don't exist yet.
func newGraphViewIDs(app core.App, operations []graphOperation) ([]string, error) {
	written := []string{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertView && operation.View != nil {
			written = append(written, operation.View.ID)
		}
	}
	if len(written) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(written)
	if err != nil {
		return nil, err
	}
	existing := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_views}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&existing); err != nil {
		return nil, err
	}
	known := map[string]struct{}{}
	for _, id := range existing {
		known[id] = struct{}{}
	}
	made := []string{}
	for _, id := range written {
		if _, found := known[id]; !found {
			made = append(made, id)
		}
	}
	return made, nil
}

// refuseDatabaseViewsPastLimit refuses a change that left a database on the
// page with more views than the limit when it made one of them there.
func refuseDatabaseViewsPastLimit(app core.App, pageID string, made []string) error {
	if len(made) == 0 {
		return nil
	}
	encoded, err := json.Marshal(made)
	if err != nil {
		return err
	}
	over := 0
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (
			SELECT [[tab]].[[container_id]]
			FROM {{leafen_database_container_views}} AS [[made]]
			JOIN {{leafen_database_views}} AS [[madeView]] ON [[madeView]].[[id]] = [[made]].[[view_id]]
			JOIN {{leafen_database_container_views}} AS [[tab]] ON [[tab]].[[container_id]] = [[made]].[[container_id]]
			JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[tab]].[[view_id]]
			WHERE [[made]].[[page_id]] = {:pageId} AND [[made]].[[view_id]] IN (SELECT [[value]] FROM json_each({:made}))
				AND ` + databaseViewNotWidget("madeView") + ` AND ` + databaseViewNotWidget("view") + `
			GROUP BY [[tab]].[[container_id]]
			HAVING COUNT(DISTINCT [[tab]].[[view_id]]) > {:limit}
		)
	`).Bind(dbx.Params{"pageId": pageID, "made": string(encoded), "limit": databaseViewLimit}).Row(&over); err != nil {
		return err
	}
	if over > 0 {
		return graphInputError(errorCodeGraphInvalid, databaseViewLimitMessage)
	}
	return nil
}

// databaseViewNotWidget is the SQL condition that the aliased view is no
// dashboard's widget (dashboardWidgetOwner).
func databaseViewNotWidget(alias string) string {
	return `COALESCE(CASE WHEN json_valid([[` + alias + `]].[[config]]) THEN json_extract([[` + alias + `]].[[config]], '$.dashboardViewId') END, '') = ''`
}
