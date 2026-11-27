package app

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// A page's editors in its analytics, as Notion's "편집자": who made it and
// when, and who edited it lately, each once with their last edit. They come
// from the page's history versions, the newest 500 of them; a page's maker
// from its record, or else its first version (a database item's page has
// its versions under the item).

const pageAnalyticsEditorVersions = 500

type storedPageAnalyticsEditor struct {
	UserID      string         `db:"user_id"`
	DisplayName sql.NullString `db:"display_name"`
	At          int64          `db:"at"`
}

func (stored storedPageAnalyticsEditor) wire() pageAnalyticsEditor {
	editor := pageAnalyticsEditor{UserID: stored.UserID, At: time.UnixMilli(stored.At).UTC().Format(time.RFC3339Nano)}
	if stored.DisplayName.Valid && strings.TrimSpace(stored.DisplayName.String) != "" {
		name := stored.DisplayName.String
		editor.DisplayName = &name
	}
	return editor
}

func readPageAnalyticsEditors(app core.App, workspaceID, targetID string, page bool) (*pageAnalyticsEditor, []pageAnalyticsEditor, error) {
	params := dbx.Params{"workspaceId": workspaceID, "targetId": targetID, "versions": pageAnalyticsEditorVersions,
		"limit": maxPageAnalyticsRecentEditors}
	recent := []storedPageAnalyticsEditor{}
	if err := app.DB().NewQuery(`
		SELECT [[version]].[[actor_id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]], MAX([[version]].[[created_at]]) AS [[at]]
		FROM (SELECT [[actor_id]], [[created_at]] FROM {{leafen_page_history_versions}}
			INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
			WHERE [[page_id]] = {:targetId} AND [[workspace_id]] = {:workspaceId} AND ` + pageHistoryEditVersion("") + `
			ORDER BY [[version_cursor]] DESC LIMIT {:versions}) AS [[version]]
		JOIN {{users}} AS [[user]] ON [[user]].[[id]] = [[version]].[[actor_id]]
		GROUP BY [[version]].[[actor_id]] ORDER BY [[at]] DESC, [[user_id]] LIMIT {:limit}
	`).Bind(params).All(&recent); err != nil {
		return nil, nil, err
	}
	editors := make([]pageAnalyticsEditor, 0, len(recent))
	for _, stored := range recent {
		editors = append(editors, stored.wire())
	}
	if page {
		maker := struct {
			UserID      string         `db:"user_id"`
			DisplayName sql.NullString `db:"display_name"`
			Created     string         `db:"created"`
		}{}
		err := app.DB().NewQuery(`
			SELECT [[page]].[[created_by]] AS [[user_id]], [[user]].[[name]] AS [[display_name]], [[page]].[[created]]
			FROM {{leafen_pages}} AS [[page]] JOIN {{users}} AS [[user]] ON [[user]].[[id]] = [[page]].[[created_by]]
			WHERE [[page]].[[id]] = {:targetId}
		`).Bind(params).One(&maker)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		if created, parsed := types.ParseDateTime(maker.Created); err == nil && parsed == nil && !created.IsZero() {
			creator := storedPageAnalyticsEditor{UserID: maker.UserID, DisplayName: maker.DisplayName, At: created.Time().UnixMilli()}.wire()
			return &creator, editors, nil
		}
	}
	first := storedPageAnalyticsEditor{}
	err := app.DB().NewQuery(`
		SELECT [[version]].[[actor_id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]], [[version]].[[created_at]] AS [[at]]
		FROM {{leafen_page_history_versions}} AS [[version]] INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
		JOIN {{users}} AS [[user]] ON [[user]].[[id]] = [[version]].[[actor_id]]
		WHERE [[version]].[[page_id]] = {:targetId} AND [[version]].[[workspace_id]] = {:workspaceId}
		ORDER BY [[version]].[[version_cursor]] ASC LIMIT 1
	`).Bind(params).One(&first)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, editors, nil
	}
	if err != nil {
		return nil, nil, err
	}
	creator := first.wire()
	return &creator, editors, nil
}
