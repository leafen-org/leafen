package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's updates list each lock and unlock for everyone on its own, as
// Notion's do (seen 2026-11-10: "본인 〈page〉 블록을 잠갔습니다." and "…블록을
// 잠금 해제했습니다.", who and when, with no changes or version of their own):
// a page's, a full-page database's and a database page's lock under the page,
// an inline database's under its page and its block, which only the inline
// database's own updates list. Unlocking for oneself is no update. A record
// goes with its page.

// recordPageLockChange notes that someone locked or unlocked a page — or,
// named by its block, the page's inline database — for everyone, through the
// connection the change came by, if any.
func recordPageLockChange(app core.App, pageID, blockID, actorID string, locked bool) error {
	connection := pageHistoryConnectionOf(app)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_lock_records}}
			([[id]], [[page_id]], [[block_id]], [[actor_id]], [[locked]], [[connection_name]], [[connection_id]], [[created_at]])
		VALUES ({:id}, {:pageId}, {:blockId}, {:actorId}, {:locked}, {:connectionName}, {:connectionId}, {:createdAt})
	`).Bind(dbx.Params{
		"id": core.GenerateDefaultRandomId(), "pageId": pageID, "blockId": blockID, "actorId": actorID,
		"locked": locked, "connectionName": connection.Name, "connectionId": connection.ID,
		"createdAt": time.Now().UnixMilli(),
	}).Execute()
	return err
}

// recordDatabaseBlockLockChanges notes each inline database a page's change
// locks or unlocks: its block's "locked" (database_container_lock.go) set
// otherwise than it was. It reports whether that was all the change did —
// every operation an inline database's own lock, its place and the rest of
// its content as they were — so the page's history keeps the change without
// listing it as an edit (Notion lists no edit for it, seen 2026-11-10).
func recordDatabaseBlockLockChanges(app core.App, ownerID, pageID, actorID string, operations []graphOperation) (bool, error) {
	lockOnly := len(operations) > 0
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeInlineDatabase {
			lockOnly = false
			continue
		}
		stored, found, err := loadStoredDatabaseBlock(app, ownerID, pageID, block.ID)
		if err != nil {
			return false, err
		}
		if !found || stored.Type != graphBlockTypeInlineDatabase {
			lockOnly = false
			continue
		}
		was, locked := databaseBlockContentLocked([]byte(stored.Content)), databaseBlockContentLocked(block.Content)
		if was == locked {
			lockOnly = false
			continue
		}
		lockOnly = lockOnly && databaseBlockLockAlone(stored, block)
		if err := recordPageLockChange(app, pageID, block.ID, actorID, locked); err != nil {
			return false, err
		}
	}
	return lockOnly, nil
}

// databaseBlockLockAlone reports whether a write of an inline database's
// block changes nothing but its lock: it stays where it is and its content
// is the same apart from "locked".
func databaseBlockLockAlone(stored storedDatabaseBlock, block *graphBlock) bool {
	if block.AfterID != nil || block.ParentID != nil && *block.ParentID != stored.ParentID ||
		block.Position != "" && block.Position != stored.Position {
		return false
	}
	without := func(content []byte) (string, bool) {
		value := map[string]any{}
		if len(bytes.TrimSpace(content)) > 0 && json.Unmarshal(content, &value) != nil {
			return "", false
		}
		delete(value, "locked")
		raw, err := json.Marshal(value)
		return string(raw), err == nil
	}
	before, readBefore := without([]byte(stored.Content))
	after, readAfter := without(block.Content)
	return readBefore && readAfter && before == after
}

func databaseBlockContentLocked(content []byte) bool {
	value := struct {
		Locked bool `json:"locked"`
	}{}
	return len(bytes.TrimSpace(content)) > 0 && json.Unmarshal(content, &value) == nil && value.Locked
}

// listPageLockRecords is a page's latest locks and unlocks, the newest first:
// its own, or its inline database's when the block is named.
func listPageLockRecords(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	query := event.Request.URL.Query()
	blockID := strings.TrimSpace(query.Get("blockId"))
	limit, err := parsePageHistoryLimit(query.Get("limit"), pageLockRecordPageSize)
	if err != nil || pageID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page lock records query.")
	}
	response := pageLockRecordListResponse{PageID: pageID, BlockID: blockID, Records: []pageLockRecordWire{}}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		rows := []struct {
			ID             string `db:"id"`
			ActorID        string `db:"actor_id"`
			Locked         bool   `db:"locked"`
			ConnectionName string `db:"connection_name"`
			ConnectionID   string `db:"connection_id"`
			CreatedAt      int64  `db:"created_at"`
		}{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]], [[actor_id]], [[locked]], [[connection_name]], [[connection_id]], [[created_at]]
			FROM {{leafen_page_lock_records}}
			WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId}
			ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{"pageId": pageID, "blockId": blockID, "limit": limit}).All(&rows); err != nil {
			return err
		}
		actorIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			response.Records = append(response.Records, pageLockRecordWire{
				ID: row.ID, ActorID: row.ActorID, Locked: row.Locked, CreatedAt: row.CreatedAt,
				ConnectionName: row.ConnectionName, ConnectionKind: pageHistoryConnectionKind(row.ConnectionID),
			})
			actorIDs = append(actorIDs, row.ActorID)
		}
		response.ActorProfiles, err = pageHistoryActorProfiles(txApp, actorIDs)
		return err
	})
	return writePageHistoryResult(event, response, err, "lock_records")
}
