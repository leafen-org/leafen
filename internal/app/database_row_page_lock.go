package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page locks as a page does: someone who can edit it locks it
// for everyone or unlocks it, and unlocks it only for themselves or locks it
// for them again. A locked database page keeps its title, body, properties,
// icon and cover in the app, its properties in every view of its database
// too, while moving and ordering its row stay; the public API and MCP change
// it by access alone.

func registerDatabaseRowPageLockRoutes(event *core.ServeEvent, rooms workspaceRoomInvalidator) {
	event.Router.PUT(apiRoot+databaseRowPageLockPath, func(request *core.RequestEvent) error {
		return setDatabaseRowPageLock(request, rooms)
	})
	event.Router.PUT(apiRoot+databaseRowPageLockForMePath, func(request *core.RequestEvent) error {
		return setDatabaseRowPageLockForMe(request, rooms)
	})
}

func databaseRowPageLockError(event *core.RequestEvent, err error, step string) error {
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageInTrash):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageInTrash, "Restore the database row before locking it.")
	case errors.Is(err, errDatabaseRowPageConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row changed. Try again.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
		"Couldn't change the lock.", "database_row_page_lock", step, errorCodeDatabaseRowPageFailed, err)
}

func setDatabaseRowPageLock(event *core.RequestEvent, rooms workspaceRoomInvalidator) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	input := databaseRowPageLockInput{}
	if rowID == "" || event.BindBody(&input) != nil || input.ExpectedAuthorityRevision < 1 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the lock details.")
	}
	storeRoomTypingBeforeLock(event, rowID, true)
	response, err := updateDatabaseRowPageLock(event.App, user.Id, rowID, input)
	if err != nil {
		return databaseRowPageLockError(event, err, "lock")
	}
	invalidateLockedPageRoom(rooms, rowID)
	return event.JSON(http.StatusOK, response)
}

// updateDatabaseRowPageLock locks a database page for everyone or unlocks
// it, ending everyone's own unlocks.
func updateDatabaseRowPageLock(
	app core.App, principalID, rowID string, input databaseRowPageLockInput,
) (databaseRowPageActionResponse, error) {
	response := databaseRowPageActionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseRowOrTemplatePage(txApp, principalID, rowID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		// A template's lock is its page record's, set last-wins
		// (database_template_row_page.go).
		if !authority.Template && authority.AuthorityRevision != input.ExpectedAuthorityRevision {
			return errDatabaseRowPageConflict
		}
		if err := setDatabaseRowPageLocked(txApp, rowID, input.Locked, principalID); err != nil {
			return err
		}
		if !authority.Template {
			recordDatabaseRowPageLockWebhookEvent(txApp, authority, principalID, input.Locked)
		}
		response = databaseRowPageActionResponse{
			RowID: rowID, ContainerPageID: authority.ContainerPageID, AuthorityRevision: authority.AuthorityRevision + 1,
		}
		if authority.Template {
			response.AuthorityRevision = authority.AuthorityRevision
			return nil
		}
		return announceDatabaseRowPageChange(txApp, rowID)
	})
	return response, err
}

// setDatabaseRowPageLocked locks a database page for everyone or unlocks it,
// ending everyone's own unlocks; the lock names who set it (page_lock_schema.go),
// its updates list the change (page_lock_records.go) and its history keeps
// it as a version of its own, as a page's (page_history_page_lock.go).
func setDatabaseRowPageLocked(app core.App, rowID string, locked bool, actorID string) error {
	var was bool
	if err := app.DB().NewQuery(`SELECT [[is_locked]] FROM {{leafen_pages}} WHERE [[id]]={:rowId}`).
		Bind(dbx.Params{"rowId": rowID}).Row(&was); err != nil {
		return err
	}
	if was != locked {
		if err := ensurePageHistoryBaseline(app, rowID); err != nil {
			return err
		}
		if err := recordPageLockChange(app, rowID, "", actorID, locked); err != nil {
			return err
		}
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET
			[[locked_by]] = CASE WHEN NOT {:locked} THEN '' WHEN [[is_locked]] THEN [[locked_by]] ELSE {:actor} END,
			[[is_locked]] = {:locked}
		WHERE [[id]]={:rowId}
	`).Bind(dbx.Params{"locked": locked, "actor": actorID, "rowId": rowID}).Execute(); err != nil {
		return err
	}
	if err := clearPageLockUnlocks(app, rowID); err != nil {
		return err
	}
	if was != locked {
		if err := recordPageHistoryPageLock(app, rowID, actorID); err != nil {
			return err
		}
	}
	return touchDatabaseRowPageAuthority(app, rowID, time.Now().UTC().UnixMilli())
}

// applyNotionDatabaseRowPageLock applies a Public API is_locked to the row's
// page; nil, or the lock it has, leaves it as it is.
func applyNotionDatabaseRowPageLock(app core.App, authority databaseRowPageAuthority, actorID string, locked *bool) error {
	if locked == nil || *locked == authority.Locked {
		return nil
	}
	if err := setDatabaseRowPageLocked(app, authority.RowID, *locked, actorID); err != nil {
		return err
	}
	recordDatabaseRowPageLockWebhookEvent(app, authority, actorID, *locked)
	return announceDatabaseRowPageChange(app, authority.RowID)
}

// recordDatabaseRowPageLockWebhookEvent tells of a database page locked or
// unlocked, as a page's lock is told of.
func recordDatabaseRowPageLockWebhookEvent(app core.App, authority databaseRowPageAuthority, actorID string, locked bool) {
	kind := eventWebhookPageUnlocked
	if locked {
		kind = eventWebhookPageLocked
	}
	recordDatabaseRowPageWebhookEvent(app, kind, authority, actorID)
}

// setDatabaseRowPageLockForMe unlocks a locked database page for the person
// asking, or locks it for them again; their open editors reconnect.
func setDatabaseRowPageLockForMe(event *core.RequestEvent, rooms workspaceRoomInvalidator) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	unlocked, ok := bindPageLockForMe(event)
	if rowID == "" || !ok {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, pageLockForMeRefusal)
	}
	state := databaseRowPageLockState{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseRowOrTemplatePage(txApp, user.Id, rowID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		if unlocked && authority.Locked {
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_page_lock_unlocks}} ([[page_id]], [[principal_id]], [[workspace_id]], [[created_at]])
				VALUES ({:rowId}, {:principalId}, {:workspaceId}, {:now})
				ON CONFLICT ([[page_id]], [[principal_id]]) DO NOTHING
			`).Bind(dbx.Params{
				"rowId": rowID, "principalId": user.Id, "workspaceId": authority.WorkspaceID, "now": time.Now().UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		} else if !unlocked {
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_page_lock_unlocks}} WHERE [[page_id]]={:rowId} AND [[principal_id]]={:principalId}
			`).Bind(dbx.Params{"rowId": rowID, "principalId": user.Id}).Execute(); err != nil {
				return err
			}
		}
		state, err = databaseRowPageLockStateFor(txApp, authority, user.Id)
		return err
	})
	if err != nil {
		return databaseRowPageLockError(event, err, "lock-for-me")
	}
	invalidateLockedPageRoom(rooms, rowID)
	return event.JSON(http.StatusOK, state)
}

func databaseRowPageLockStateFor(app core.App, authority databaseRowPageAuthority, principalID string) (databaseRowPageLockState, error) {
	if !authority.Locked {
		return databaseRowPageLockState{}, nil
	}
	unlocked, err := pageUnlockedFor(app, authority.RowID, principalID)
	return databaseRowPageLockState{Locked: true, UnlockedForMe: unlocked}, err
}

// databaseRowPageLockedFor reports whether a database page's lock stops the
// person's changes in the app.
func databaseRowPageLockedFor(app core.App, authority databaseRowPageAuthority, principalID string) (bool, error) {
	state, err := databaseRowPageLockStateFor(app, authority, principalID)
	return state.Locked && !state.UnlockedForMe, err
}

// refuseLockedRowValueChanges refuses changes to the values of rows whose
// database pages are locked for the person; moving and ordering them stay.
func refuseLockedRowValueChanges(app core.App, principalID string, operations []graphOperation) error {
	rows := map[string]*graphDatabaseRow{}
	ids := []any{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil && len(operation.Row.Values) > 0 {
			if _, seen := rows[operation.Row.ID]; !seen {
				ids = append(ids, operation.Row.ID)
			}
			rows[operation.Row.ID] = operation.Row
		}
	}
	if len(ids) == 0 {
		return nil
	}
	locked := []string{}
	if err := app.DB().Select("id").From(pagesCollection).
		Where(dbx.And(dbx.In("id", ids...), dbx.NewExp("[[is_locked]]"), dbx.NewExp("[[data_source_id]]!=''"))).
		Column(&locked); err != nil {
		return err
	}
	for _, rowID := range locked {
		unlocked, err := pageUnlockedFor(app, rowID, principalID)
		if err != nil {
			return err
		}
		if unlocked {
			continue
		}
		changed, err := graphRowValuesChange(app, rows[rowID])
		if err != nil {
			return err
		}
		if changed {
			return pageLockedError()
		}
	}
	return nil
}

// graphRowValuesChange reports whether an upsert changes any of a row's
// stored values or their formatting.
func graphRowValuesChange(app core.App, row *graphDatabaseRow) (bool, error) {
	stored := []struct {
		PropertyID string `db:"property_id"`
		Type       string `db:"type"`
		Value      string `db:"value"`
		Spans      string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property_id]], [[type]], [[value]], [[spans]] FROM {{leafen_database_row_values}} WHERE [[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": row.ID}).All(&stored); err != nil {
		return false, err
	}
	cells := make(map[string]int, len(stored))
	for index, cell := range stored {
		cells[cell.PropertyID] = index
	}
	for propertyID, rowValue := range row.Values {
		index, found := cells[propertyID]
		if graphRowValueIsCleared(rowValue.Value) {
			if found {
				return true, nil
			}
			continue
		}
		if !found || stored[index].Type != rowValue.Type {
			return true, nil
		}
		value, err := decodeBoundedGraphJSON(rowValue.Value)
		if err != nil {
			return false, err
		}
		var storedValue, storedSpans any
		if json.Unmarshal([]byte(stored[index].Value), &storedValue) != nil {
			return true, nil
		}
		if stored[index].Spans != "" && json.Unmarshal([]byte(stored[index].Spans), &storedSpans) != nil {
			return true, nil
		}
		if !sameGraphJSON(storedValue, value) || !sameGraphJSON(formattedSpans(storedSpans), formattedSpans(rowValue.Spans)) {
			return true, nil
		}
	}
	return false, nil
}

// formattedSpans is a value's spans as JSON when any of them carries more
// than text, and nil for plain text.
func formattedSpans(spans any) any {
	encoded, err := json.Marshal(spans)
	if err != nil {
		return spans
	}
	decoded := []map[string]any{}
	if json.Unmarshal(encoded, &decoded) != nil {
		return nil
	}
	for _, span := range decoded {
		for key := range span {
			if key != "text" {
				return decoded
			}
		}
	}
	return nil
}

// sameGraphJSON compares two values as the JSON they encode to.
func sameGraphJSON(left, right any) bool {
	encode := func(value any) string {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "\x00"
		}
		var decoded any
		if json.Unmarshal(encoded, &decoded) != nil {
			return "\x00"
		}
		normal, _ := json.Marshal(decoded)
		return string(normal)
	}
	return encode(left) == encode(right)
}
