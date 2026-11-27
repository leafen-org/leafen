package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's mention draws the database's own icon before its name with
// the mention's arrow, as Notion's does (seen 2026-11-22 in Chrome at 1440:
// an inline database's 🍋 and a full-page database's ⭐ where a page
// mention's emoji sits, the workspace's People by its person in a ring, and a
// table for one with none): a full-page database's page's icon, which its
// page's head shows, else its first source's — People's own while People has
// none (people_database_icon.go); one of several sources of its own its own
// (database_own_icon.go). The app's mentions read it with the
// database's name (readDatabaseMentionPreview), a published page's with its
// target (public_content_mentioned_databases.go).

// databaseMentionIconWindow is how many databases one read of their icons names.
const databaseMentionIconWindow = 256

type databaseMentionIcon struct {
	Icon     string
	IconType string
}

// readDatabaseMentionIcons gives the icon each named database's mention
// draws; one with none of its own is left out.
func readDatabaseMentionIcons(app core.App, containerIDs []string) (map[string]databaseMentionIcon, error) {
	icons := make(map[string]databaseMentionIcon, len(containerIDs))
	for start := 0; start < len(containerIDs); start += databaseMentionIconWindow {
		window := containerIDs[start:min(start+databaseMentionIconWindow, len(containerIDs))]
		params := dbx.Params{}
		placeholders := make([]string, 0, len(window))
		for index, containerID := range window {
			key := fmt.Sprintf("mentionIconDatabase%d", index)
			params[key] = containerID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []struct {
			ID             string `db:"id"`
			FullPage       bool   `db:"full_page"`
			PageIcon       string `db:"page_icon"`
			PageIconType   string `db:"page_icon_type"`
			SourceIcon     string `db:"source_icon"`
			SourceIconType string `db:"source_icon_type"`
			Own            int    `db:"own"`
			BlockContent   string `db:"block_content"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[container]].[[id]],
				([[page]].[[kind]] = '` + pageKindDatabase + `' AND [[container]].[[parent_block_id]] = '') AS [[full_page]],
				[[page]].[[icon]] AS [[page_icon]], [[page]].[[icon_type]] AS [[page_icon_type]],
				COALESCE(` + dataSourceIconSQL("[[source]]") + `, '') AS [[source_icon]],
				COALESCE(` + dataSourceIconTypeSQL("[[source]]") + `, '') AS [[source_icon_type]],
				(SELECT COUNT(*) FROM {{leafen_database_container_sources}} [[own]]
					WHERE [[own]].[[container_id]] = [[container]].[[id]] AND [[own]].[[source_container_id]] = '') AS [[own]],
				COALESCE((SELECT [[block]].[[content]] FROM {{leafen_blocks}} [[block]]
					WHERE [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]), '') AS [[block_content]]
			FROM {{leafen_database_containers}} [[container]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
			LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = (
				SELECT [[member]].[[data_source_id]] FROM {{leafen_database_container_sources}} [[member]]
				WHERE [[member]].[[container_id]] = [[container]].[[id]]
				ORDER BY [[member]].[[position]], [[member]].[[data_source_id]] LIMIT 1
			)
			WHERE [[container]].[[id]] IN (` + joinSQL(placeholders) + `)
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			// One of several sources of its own draws its own icon (database_own_icon.go).
			if row.Own > 1 {
				icon, iconType := databaseOfSeveralIcon(row.FullPage, row.PageIcon, row.PageIconType, []byte(row.BlockContent), row.SourceIcon, row.SourceIconType)
				if databaseMentionIconSet(icon, iconType) {
					icons[row.ID] = databaseMentionIcon{Icon: icon, IconType: iconType}
				}
				continue
			}
			if row.FullPage && databaseMentionIconSet(row.PageIcon, row.PageIconType) {
				icons[row.ID] = databaseMentionIcon{Icon: row.PageIcon, IconType: row.PageIconType}
			} else if databaseMentionIconSet(row.SourceIcon, row.SourceIconType) {
				icons[row.ID] = databaseMentionIcon{Icon: row.SourceIcon, IconType: row.SourceIconType}
			}
		}
	}
	return icons, nil
}

func databaseMentionIconSet(icon, iconType string) bool {
	return icon != "" && iconType != "" && iconType != graphDataSourceIconNone
}
