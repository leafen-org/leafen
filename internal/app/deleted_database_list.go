package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The trash's databases list a window at a time, newest first: the deleted
// databases and the source databases in the trash of deleted blocks, merged
// by when they were deleted, keeping those the person may edit. Each window
// reads a finite number of each, and the list goes on to the last.

const (
	deletedDatabaseListWindow  = 200
	deletedDatabaseCursorBytes = 256
)

var errDeletedDatabaseCursorInvalid = errors.New("deleted database cursor invalid")

type deletedDatabaseCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func decodeDeletedDatabaseCursor(raw string) (deletedDatabaseCursor, error) {
	if raw == "" {
		return deletedDatabaseCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if len(raw) > deletedDatabaseCursorBytes || err != nil {
		return deletedDatabaseCursor{}, errDeletedDatabaseCursorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	cursor := deletedDatabaseCursor{}
	if decoder.Decode(&cursor) != nil || cursor.ID == "" {
		return deletedDatabaseCursor{}, errDeletedDatabaseCursorInvalid
	}
	return cursor, nil
}

func listDeletedDatabases(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		return deletedDatabaseRouteError(event, err, "list-workspace")
	}
	if err := purgeExpiredDeletedDatabases(event.App, time.Now().UTC().UnixMilli()); err != nil {
		return deletedDatabaseRouteError(event, err, "purge")
	}
	items, next, err := readDeletedDatabaseWindow(event.App, user.Id, workspaceID, event.Request.URL.Query().Get("cursor"))
	if errors.Is(err, errDeletedDatabaseCursorInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Start the list again.")
	}
	if err != nil {
		return deletedDatabaseRouteError(event, err, "list")
	}
	return event.JSON(http.StatusOK, deletedDatabaseList{Items: items, NextCursor: next})
}

// readDeletedDatabaseWindow reads the next window of a workspace's databases
// in the trash from the cursor on, keeping those the person may edit; next
// goes on from there, "" once all are read.
func readDeletedDatabaseWindow(app core.App, userID, workspaceID, raw string) ([]deletedDatabase, string, error) {
	after, err := decodeDeletedDatabaseCursor(raw)
	if err != nil {
		return nil, "", err
	}
	keyset := ""
	if after.ID != "" {
		keyset = `AND [[deleted_at]] <= {:at} AND ([[deleted_at]] < {:at} OR [[block_id]] > {:id})`
	}
	stored := []storedDeletedDatabase{}
	if err := app.DB().NewQuery(`
		SELECT [[block_id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_ids]],[[block_content]],[[title]],
			[[deleted_at]],[[expires_at]]
		FROM {{leafen_deleted_databases}} INDEXED BY [[idx_leafen_deleted_databases_workspace]]
		WHERE [[workspace_id]]={:workspaceId} ` + keyset + `
		ORDER BY [[deleted_at]] DESC,[[block_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "at": after.At, "id": after.ID, "limit": deletedDatabaseListWindow}).All(&stored); err != nil {
		return nil, "", err
	}
	containers, err := listTrashedDatabaseContainers(app, workspaceID, "", after, deletedDatabaseListWindow)
	if err != nil {
		return nil, "", err
	}
	merged := make([]deletedDatabaseCandidate, 0, len(stored)+len(containers))
	for left, right := 0, 0; left < len(stored) || right < len(containers); {
		takeLeft := right == len(containers) || (left < len(stored) &&
			(stored[left].DeletedAt > containers[right].DeletedAt ||
				(stored[left].DeletedAt == containers[right].DeletedAt && stored[left].BlockID <= containers[right].RootBlockID)))
		if takeLeft {
			item := stored[left]
			merged = append(merged, deletedDatabaseCandidate{containerID: item.BlockID, item: deletedDatabase{BlockID: item.BlockID, Title: item.Title,
				PageID: item.PageID, DeletedAt: item.DeletedAt, ExpiresAt: item.ExpiresAt}})
			left++
			continue
		}
		item := containers[right]
		listed := deletedDatabase{BlockID: item.RootBlockID, Title: item.Title,
			PageID: item.PageID, DeletedAt: item.DeletedAt, ExpiresAt: item.ExpiresAt}
		owned := 0
		if item.InPlace {
			if owned, err = databaseOwnSourceCount(app, item.ContainerID); err != nil {
				return nil, "", err
			}
			listed.InPlace = true
		}
		// A database in a page in the trash (database_in_trashed_page.go), or
		// one whose block is, is placed at its own last edit, as Notion's
		// trash places "L inline" below the rows added to it after (seen
		// 2026-11-12) and a database whose in_trash was set below the pages
		// sent there between its last edit and then (seen 2026-11-13). One
		// whose block was deleted from its page lists at that time, its last
		// edit in Notion too (seen 2026-11-13, through the block menu).
		listed.InTrashedPage = item.InTrashedPage
		if !item.InPlace {
			listed.EditedAt = item.EditedAt
		}
		merged = append(merged, deletedDatabaseCandidate{container: true, containerID: item.ContainerID, ownSources: owned, item: listed})
		right++
	}
	done := len(stored) < deletedDatabaseListWindow && len(containers) < deletedDatabaseListWindow &&
		len(merged) <= deletedDatabaseListWindow
	if len(merged) > deletedDatabaseListWindow {
		merged = merged[:deletedDatabaseListWindow]
	}
	kept := []deletedDatabaseCandidate{}
	for _, entry := range merged {
		// A source kept in its database is a line of its own only as one
		// named source of several: its database's only source, or one with
		// no name, lists as its pages alone, as Notion's trash does (seen
		// 2026-11-12: "S one", a full-page database's only source, and "I
		// one", an inline one's, list none; an untitled one of two lists
		// none, its pages "… / 새 데이터 소스"; "H one" and "V second", each
		// one of two, and both of "J one" and "J second" list).
		if entry.item.InPlace && (entry.ownSources < 2 || strings.TrimSpace(entry.item.Title) == "") {
			continue
		}
		// A database in both lists shows once, as a deleted database.
		if entry.container {
			var both bool
			if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_deleted_databases}} WHERE [[block_id]] = {:id})`).
				Bind(dbx.Params{"id": entry.item.BlockID}).Row(&both); err != nil {
				return nil, "", err
			}
			if both {
				continue
			}
		}
		if _, err := authorizePageBody(app, userID, entry.item.PageID, permissionWorkspaceWrite); err != nil {
			if isPermissionDenied(err) {
				continue
			}
			return nil, "", err
		}
		if err := nameDeletedDatabaseRowHolder(app, &entry.item); err != nil {
			return nil, "", err
		}
		kept = append(kept, entry)
	}
	items, err := nameDeletedDatabaseLines(app, kept)
	if err != nil {
		return nil, "", err
	}
	if done || len(merged) == 0 {
		return items, "", nil
	}
	last := merged[len(merged)-1].item
	encoded, _ := json.Marshal(deletedDatabaseCursor{At: last.DeletedAt, ID: last.BlockID})
	return items, base64.RawURLEncoding.EncodeToString(encoded), nil
}

// nameDeletedDatabaseRowHolder names the database's page holding a database
// in the trash — a row's body, a template's — which the app's page list
// doesn't hold: the trash's line places the database below it, as Notion's
// trash places a database from a row's body ("… / 행 하나", seen
// 2026-11-15), the same as one from a page's body.
func nameDeletedDatabaseRowHolder(app core.App, item *deletedDatabase) error {
	holder := struct {
		Title string `db:"title"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[data_source_id]] != ''
	`).Bind(dbx.Params{"id": item.PageID}).One(&holder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	item.HolderRow, item.HolderTitle = true, holder.Title
	return nil
}
