package app

import (
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pageArchiveEffect names the archived page that archives a page: the page
// itself or its nearest archived ancestor.
type pageArchiveEffect struct {
	PageID     string `db:"page_id"`
	RootID     string `db:"root_id"`
	ArchivedAt string `db:"archived_at"`
	ArchivedBy string `db:"archived_by"`
}

// applyPageArchiveState is the single writer for page archive state. It keeps
// the first archive while a page stays archived and clears it on unarchive.
func applyPageArchiveState(page *core.Record, archived bool, actorID string, transitionAt string) {
	if !archived {
		page.Set("archived_at", "")
		page.Set("archived_by", "")
		return
	}
	if strings.TrimSpace(page.GetString("archived_at")) != "" {
		return
	}
	page.Set("archived_at", transitionAt)
	page.Set("archived_by", actorID)
}

func pageArchiveTimestamp(page *core.Record) *string {
	return optionalPageTrashTimestamp(page.GetString("archived_at"))
}

func pageArchived(page *core.Record) bool {
	return strings.TrimSpace(page.GetString("archived_at")) != ""
}

// loadPageArchiveEffects reports, for each listed page archived itself or
// through an ancestor, the archived page nearest to it. Pages that are not
// archived are absent from the result.
func loadPageArchiveEffects(app core.App, pageIDs []string) (map[string]pageArchiveEffect, error) {
	effects := map[string]pageArchiveEffect{}
	if len(pageIDs) == 0 {
		return effects, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		name := "page" + strconv.Itoa(index)
		params[name] = pageID
		placeholders = append(placeholders, "{:"+name+"}")
	}
	rows := []pageArchiveEffect{}
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[chain]] ([[page_id]], [[id]], [[parent_id]], [[archived_at]], [[archived_by]]) AS (
			SELECT [[page]].[[id]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `,
				[[page]].[[archived_at]], [[page]].[[archived_by]]
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (` + strings.Join(placeholders, ",") + `)
			UNION
			SELECT [[chain]].[[page_id]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `,
				[[page]].[[archived_at]], [[page]].[[archived_by]]
			FROM [[chain]] JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[chain]].[[parent_id]]
			WHERE [[chain]].[[parent_id]] != '' AND trim([[chain]].[[archived_at]]) = ''
		)
		SELECT [[page_id]], [[id]] AS [[root_id]], [[archived_at]], [[archived_by]]
		FROM [[chain]] WHERE trim([[archived_at]]) != ''
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		effects[row.PageID] = row
	}
	return effects, nil
}

func loadPageArchiveEffect(app core.App, pageID string) (pageArchiveEffect, bool, error) {
	effects, err := loadPageArchiveEffects(app, []string{pageID})
	if err != nil {
		return pageArchiveEffect{}, false, err
	}
	effect, archived := effects[pageID]
	return effect, archived, nil
}
