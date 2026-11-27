package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// storedPublicMentionedDatabase is a mentioned database, the page holding
// it, its name as the visitor reads it (publicMentionedDatabasesAsRead) and
// the icon its mention draws (database_mention_icon.go).
type storedPublicMentionedDatabase struct {
	ID       string `db:"id"`
	PageID   string `db:"page_id"`
	PageKind string `db:"page_kind"`
	Title    string `db:"-"`
	Icon     string `db:"-"`
	IconType string `db:"-"`
}

// loadPublicMentionedDatabases reads the live databases of the workspace
// among the mentioned ones: the page holding each, and each one's icon.
func loadPublicMentionedDatabases(
	app core.App, authorization publicVisibilityAuthorization, containerIDs []string,
) (map[string]storedPublicMentionedDatabase, error) {
	databases := make(map[string]storedPublicMentionedDatabase, len(containerIDs))
	for start := 0; start < len(containerIDs); start += publicLinkedPageWindow {
		window := containerIDs[start:min(start+publicLinkedPageWindow, len(containerIDs))]
		params := dbx.Params{
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.Workspace.GetString("owner"),
		}
		placeholders := make([]string, 0, len(window))
		for index, containerID := range window {
			key := fmt.Sprintf("mentionedDatabase%d", index)
			params[key] = containerID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []storedPublicMentionedDatabase{}
		if err := app.DB().NewQuery(`
			SELECT [[container]].[[id]], [[container]].[[page_id]], [[page]].[[kind]] AS [[page_kind]]
			FROM {{leafen_database_containers}} [[container]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
			WHERE [[container]].[[id]] IN (` + joinSQL(placeholders) + `)
				AND [[container]].[[workspace_id]] = {:workspaceId}
				AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
				AND [[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_database_container_trash}} [[trash]]
					WHERE [[trash]].[[container_id]] = [[container]].[[id]]
				)
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			databases[row.ID] = row
		}
	}
	ids := make([]string, 0, len(databases))
	for id := range databases {
		ids = append(ids, id)
	}
	icons, err := readDatabaseMentionIcons(app, ids)
	if err != nil {
		return nil, err
	}
	for id, icon := range icons {
		database := databases[id]
		database.Icon, database.IconType = icon.Icon, icon.IconType
		databases[id] = database
	}
	return databases, nil
}
