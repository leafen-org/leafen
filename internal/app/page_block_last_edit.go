package app

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A block's last edit, as the foot of Notion's block menu names it (seen
// 2026-10-01: "〈이름〉 최종 편집" and when): the latest change to the blocks'
// structure or text that the page's history holds — its history starting
// anew (a later baseline) is no edit — or, when later, the blocks' own last
// change, such as a move, with who made it. Several blocks take the latest.
// Beside it, who made the block (pageBlockFirstMade), for the activity the
// foot shows under the pointer; each with the kind of connection it came
// through, an integration's named alone as Notion's (seen 2026-11-26).

func readPageBlockLastEdit(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	ids := pageBlockLastEditIDs(event.Request.URL.Query().Get("ids"))
	if pageID == "" || len(ids) == 0 || len(ids) > pageHistoryBlockLastEditIDs {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the blocks.")
	}
	if _, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead); err != nil {
		return writePageHistoryResult(event, nil, err, "block-last-edit")
	}
	response, err := pageBlockLastEdit(event.App, pageID, ids)
	return writePageHistoryResult(event, response, err, "block-last-edit")
}

// pageBlockLastEditIDs are the distinct block IDs a comma-separated list names.
func pageBlockLastEditIDs(raw string) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		if id := strings.TrimSpace(part); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func pageBlockLastEdit(app core.App, pageID string, ids []string) (pageBlockLastEditResponse, error) {
	edits, err := pageBlockLastEdits(app, pageID, ids)
	if err != nil {
		return pageBlockLastEditResponse{}, err
	}
	edit := pageBlockEdit{}
	for _, id := range ids {
		if found, ok := edits[id]; ok && found.At > edit.At {
			edit = found
		}
	}
	made, err := pageBlockFirstMade(app, pageID, ids)
	if err != nil {
		return pageBlockLastEditResponse{}, err
	}
	response := pageBlockLastEditResponse{EditedAt: edit.At, EditorID: edit.ActorID, EditorVia: edit.Connection.Name,
		EditorViaKind: pageHistoryConnectionKind(edit.Connection.ID), CreatedAt: made.At, CreatorID: made.ActorID,
		CreatorVia: made.Connection.Name, CreatorViaKind: pageHistoryConnectionKind(made.Connection.ID)}
	actors := []string{}
	for _, actor := range []string{edit.ActorID, made.ActorID} {
		if actor != "" && !slices.Contains(actors, actor) {
			actors = append(actors, actor)
		}
	}
	if len(actors) == 0 {
		return response, nil
	}
	profiles, err := pageHistoryActorProfiles(app, actors)
	if err != nil {
		return pageBlockLastEditResponse{}, err
	}
	for _, profile := range profiles {
		// An app's own account is the app: named alone, as a connection
		// is, with nothing beside it.
		if profile.UserID == edit.ActorID {
			response.EditorName = profile.DisplayName
			if profile.App {
				response.EditorApp, response.EditorVia, response.EditorViaKind = true, "", ""
			}
		}
		if profile.UserID == made.ActorID {
			response.CreatorName = profile.DisplayName
			if profile.App {
				response.CreatorApp, response.CreatorVia, response.CreatorViaKind = true, "", ""
			}
		}
	}
	return response, nil
}

// pageBlockEdit is one block's last edit: who made it, when (ms) and through
// which connection, which its menu names beside them as Notion's does (seen
// 2026-11-05) and its mentions' notices name (noticeConnection).
type pageBlockEdit struct {
	ActorID    string
	At         int64
	Connection pageHistoryConnection
}

// pageBlockLastEditWindow is how many blocks one statement asks about.
const pageBlockLastEditWindow = 200

// pageBlockLastEdits reads each block's last edit, the one its block menu
// names: the latest of its structure or text change in the page's history, its
// typing since the history's last checkpoint, and its own row's change. The
// projection of the people a page's text mentions asks it too (2026-10-26: a
// mention typed into someone else's block was taken for that person's).
func pageBlockLastEdits(app core.App, pageID string, ids []string) (map[string]pageBlockEdit, error) {
	edits := map[string]pageBlockEdit{}
	if len(ids) == 0 {
		return edits, nil
	}
	take := func(id, actorID string, at int64, connection pageHistoryConnection) {
		if current, found := edits[id]; !found || at > current.At {
			edits[id] = pageBlockEdit{ActorID: actorID, At: at, Connection: connection}
		}
	}
	for start := 0; start < len(ids); start += pageBlockLastEditWindow {
		window := ids[start:min(start+pageBlockLastEditWindow, len(ids))]
		params := dbx.Params{"pageId": pageID, "baseline": pageHistoryVersionBaseline}
		bindings := make([]string, len(window))
		for index, id := range window {
			key := fmt.Sprintf("block%d", index)
			params[key] = id
			bindings[index] = "{:" + key + "}"
		}
		in := strings.Join(bindings, ", ")
		spans := []struct {
			BlockID      string `db:"block_id"`
			ActorID      string `db:"actor_id"`
			CreatedAt    int64  `db:"created_at"`
			Connection   string `db:"connection_name"`
			ConnectionID string `db:"connection_id"`
		}{}
		// A history starting anew (a later baseline) is no edit, nor is a span
		// that only follows another block than the one before it did — the
		// block's place shifted as blocks were put in or taken out around it,
		// its own row as it was (2026-11-07: a conflict copy kept beside one PDF
		// named the copier as the next file's last editor). A move writes the
		// moved block's row.
		if err := app.DB().NewQuery(`
			SELECT [[last]].[[block_id]], [[version]].[[actor_id]], [[version]].[[created_at]], [[version]].[[connection_name]],
				[[version]].[[connection_id]]
			FROM (
				SELECT [[span]].[[block_id]], MAX([[span]].[[valid_from]]) AS [[cursor]] FROM (
					SELECT [[block_id]], [[valid_from]] FROM (
						SELECT [[block_id]], [[valid_from]], [[parent_id]], [[type]], [[content]], [[updated_at]],
							LAG([[valid_through]]) OVER (PARTITION BY [[block_id]] ORDER BY [[valid_from]]) AS [[before_through]],
							LAG([[parent_id]]) OVER (PARTITION BY [[block_id]] ORDER BY [[valid_from]]) AS [[before_parent]],
							LAG([[type]]) OVER (PARTITION BY [[block_id]] ORDER BY [[valid_from]]) AS [[before_type]],
							LAG([[content]]) OVER (PARTITION BY [[block_id]] ORDER BY [[valid_from]]) AS [[before_content]],
							LAG([[updated_at]]) OVER (PARTITION BY [[block_id]] ORDER BY [[valid_from]]) AS [[before_updated]]
						FROM {{leafen_page_history_block_spans}}
						WHERE [[page_id]] = {:pageId} AND [[block_id]] IN (` + in + `)
					)
					WHERE [[before_through]] IS NULL OR [[before_through]] != [[valid_from]] - 1
						OR [[before_parent]] != [[parent_id]] OR [[before_type]] != [[type]]
						OR [[before_content]] != [[content]] OR [[before_updated]] != [[updated_at]]
					UNION ALL
					SELECT [[block_id]], [[valid_from]] FROM {{leafen_page_history_text_spans}}
					WHERE [[page_id]] = {:pageId} AND [[block_id]] IN (` + in + `)
				) [[span]]
				JOIN {{leafen_page_history_versions}} [[started]]
					ON [[started]].[[page_id]] = {:pageId} AND [[started]].[[version_cursor]] = [[span]].[[valid_from]]
				WHERE ([[started]].[[source]] != {:baseline} OR [[started]].[[version_cursor]] = 0)
					AND ` + pageHistoryEditVersion("started") + `
				GROUP BY [[span]].[[block_id]]
			) [[last]]
			JOIN {{leafen_page_history_versions}} [[version]]
				ON [[version]].[[page_id]] = {:pageId} AND [[version]].[[version_cursor]] = [[last]].[[cursor]]
		`).Bind(params).All(&spans); err != nil {
			return nil, err
		}
		for _, span := range spans {
			take(span.BlockID, span.ActorID, span.CreatedAt, pageHistoryConnection{Name: span.Connection, ID: span.ConnectionID})
		}
		rows := []struct {
			ID           string `db:"id"`
			Updated      int64  `db:"updated"`
			LastEditedBy string `db:"last_edited_by"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[id]], COALESCE([[updated]], 0) AS [[updated]], COALESCE([[last_edited_by]], '') AS [[last_edited_by]]
			FROM {{leafen_blocks}}
			WHERE [[page_id]] = {:pageId} AND [[id]] IN (` + in + `)
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		// A row change without its editor names no one. The row keeps the time
		// of the same person's edit its history names the connection of.
		for _, row := range rows {
			if row.LastEditedBy != "" {
				connection := pageHistoryConnection{}
				if current := edits[row.ID]; current.ActorID == row.LastEditedBy {
					connection = current.Connection
				}
				take(row.ID, row.LastEditedBy, row.Updated, connection)
			}
		}
	}
	// Typing since the page's last checkpoint is in no span yet
	// (settlePageHistoryTextSpans): its versions name the blocks they typed in.
	var checkpoint int
	if err := app.DB().NewQuery(`SELECT COALESCE(MAX([[version_cursor]]),0) FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId}`).Bind(dbx.Params{"pageId": pageID}).Row(&checkpoint); err != nil {
		return nil, err
	}
	typed, err := pageHistoryLastTyping(app, pageID, checkpoint, math.MaxInt)
	if err != nil {
		return nil, err
	}
	versions := map[int]storedPageHistoryVersion{}
	for _, id := range ids {
		cursor := typed[id]
		if cursor == 0 {
			continue
		}
		version, found := versions[cursor]
		if !found {
			if err := app.DB().NewQuery(`SELECT [[actor_id]],[[created_at]],[[connection_name]],[[connection_id]] FROM {{leafen_page_history_versions}}
				WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:cursor} AND ` + pageHistoryEditVersion("")).Bind(
				dbx.Params{"pageId": pageID, "cursor": cursor},
			).One(&version); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			versions[cursor] = version
		}
		if version.CreatedAt > 0 {
			take(id, version.ActorID, version.CreatedAt, pageHistoryConnection{Name: version.ConnectionName, ID: version.ConnectionID})
		}
	}
	return edits, nil
}
