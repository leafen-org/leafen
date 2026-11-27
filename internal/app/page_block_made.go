package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Who made a block, as the activity beside Notion's block menu names it
// under its last editor (seen 2026-11-26 in Chrome at 1440, two tabs: "생성자
// 〈name〉" and how long ago, a line an integration made naming the
// integration alone). Of several blocks, the one made first: Notion's menu
// for several blocks has no foot to show it (seen 2026-11-26).

// pageBlockMaker is the block made first among some blocks: who made it,
// when (ms) and through which connection.
type pageBlockMaker struct {
	ActorID    string
	At         int64
	Connection pageHistoryConnection
}

// pageBlockFirstMade reads the maker of the block made first among ids from
// its row (created_by, created), with the connection its first version in
// the page's history names when that version is the maker's own edit — a
// history starting anew (a later baseline) made nothing. A row that names
// no maker takes that version's.
func pageBlockFirstMade(app core.App, pageID string, ids []string) (pageBlockMaker, error) {
	if len(ids) == 0 {
		return pageBlockMaker{}, nil
	}
	params := dbx.Params{"pageId": pageID}
	bindings := make([]string, len(ids))
	for index, id := range ids {
		key := fmt.Sprintf("block%d", index)
		params[key] = id
		bindings[index] = "{:" + key + "}"
	}
	row := struct {
		ID        string `db:"id"`
		CreatedBy string `db:"created_by"`
		Created   int64  `db:"created"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], COALESCE([[created_by]], '') AS [[created_by]], COALESCE([[created]], 0) AS [[created]]
		FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:pageId} AND [[id]] IN (` + strings.Join(bindings, ", ") + `)
		ORDER BY [[created]] ASC, [[id]] ASC LIMIT 1
	`).Bind(params).One(&row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pageBlockMaker{}, nil
		}
		return pageBlockMaker{}, err
	}
	first := struct {
		ActorID      string `db:"actor_id"`
		CreatedAt    int64  `db:"created_at"`
		Source       string `db:"source"`
		Cursor       int    `db:"version_cursor"`
		Connection   string `db:"connection_name"`
		ConnectionID string `db:"connection_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[version]].[[actor_id]], [[version]].[[created_at]], [[version]].[[source]], [[version]].[[version_cursor]],
			[[version]].[[connection_name]], [[version]].[[connection_id]]
		FROM {{leafen_page_history_block_spans}} [[span]]
		JOIN {{leafen_page_history_versions}} [[version]]
			ON [[version]].[[page_id]] = [[span]].[[page_id]] AND [[version]].[[version_cursor]] = [[span]].[[valid_from]]
		WHERE [[span]].[[page_id]] = {:pageId} AND [[span]].[[block_id]] = {:blockId}
		ORDER BY [[span]].[[valid_from]] ASC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "blockId": row.ID}).One(&first); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pageBlockMaker{}, err
	}
	made := pageBlockMaker{ActorID: row.CreatedBy, At: row.Created}
	if first.ActorID == "" || first.Source == pageHistoryVersionBaseline && first.Cursor != 0 || first.Source == pageHistoryVersionCommentMarks {
		return made, nil
	}
	if made.ActorID == "" {
		made.ActorID = first.ActorID
		if made.At == 0 {
			made.At = first.CreatedAt
		}
	}
	if made.ActorID == first.ActorID {
		made.Connection = pageHistoryConnection{Name: first.Connection, ID: first.ConnectionID}
	}
	return made, nil
}
