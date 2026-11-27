package app

import (
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The inbox row's undo-like changes, as Notion's "이 알림을 읽지 않음으로
// 표시" and taking a notification back out of the archive. Each sets a fixed
// state, so repeating one changes nothing more and needs no request receipt.
const (
	notificationStateChangeUnread    = "unread"
	notificationStateChangeUnarchive = "unarchive"
)

var errNotificationStateInvalid = errors.New(errorCodeCommentNotificationStateInvalid)

func changeNotificationState(
	app core.App,
	actorID string,
	workspaceID string,
	input notificationStateInput,
) (notificationStateResponse, error) {
	change := strings.TrimSpace(input.Change)
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(workspaceID) == "" {
		return notificationStateResponse{}, refuse(errNotificationStateInvalid, "the workspace ID in the path is empty")
	}
	if change != notificationStateChangeUnread && change != notificationStateChangeUnarchive {
		return notificationStateResponse{}, refuse(errNotificationStateInvalid, "change is %s or %s, not %q", notificationStateChangeUnread, notificationStateChangeUnarchive, change)
	}
	if err := notificationIDsRefusal(errNotificationStateInvalid, input.NotificationIDs, notificationArchiveSelectedSize); err != nil {
		return notificationStateResponse{}, err
	}
	seen := make(map[string]struct{}, len(input.NotificationIDs))
	ids := make([]string, 0, len(input.NotificationIDs))
	for _, rawID := range input.NotificationIDs {
		id := strings.TrimSpace(rawID)
		if !validNotificationReadID(id) {
			return notificationStateResponse{}, errNotificationStateInvalid
		}
		if _, repeated := seen[id]; !repeated {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	response := notificationStateResponse{WorkspaceID: workspaceID, Change: change}
	err := app.RunInTransaction(func(txApp core.App) error {
		candidates, err := selectNotificationStateCandidates(txApp, actorID, workspaceID, ids, change)
		if err != nil {
			return err
		}
		visible, err := visibleNotificationArchiveIDs(txApp, actorID, workspaceID, candidates)
		if err != nil {
			return err
		}
		response.ChangedCount, err = updateNotificationState(
			txApp, actorID, workspaceID, visible, change, time.Now().UTC().UnixMilli(),
		)
		return err
	})
	return response, err
}

// The recipient's own notifications from others that the change would move:
// read ones to mark unread, archived ones to bring back.
func selectNotificationStateCandidates(
	app core.App,
	actorID string,
	workspaceID string,
	ids []string,
	change string,
) ([]storedNotificationArchiveCandidate, error) {
	// A line of the inbox changes whole (notification_inbox_groups.go).
	ids, err := expandNotificationGroups(app, actorID, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"workspaceId": workspaceID, "userId": actorID}
	params["ids"] = notificationIDList(ids)
	rows := []storedNotificationArchiveCandidate{}
	err = app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[target]] FROM {{leafen_notifications}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND ` + notificationStateMovable(change) + ` AND ` + notificationShownSQL("") + `
			AND ` + notificationIDListSQL("ids") + `
		ORDER BY [[id]]
	`).Bind(params).All(&rows)
	return rows, err
}

func updateNotificationState(
	app core.App,
	actorID string,
	workspaceID string,
	ids []string,
	change string,
	changedAt int64,
) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	params := dbx.Params{"workspaceId": workspaceID, "userId": actorID, "changedAt": changedAt}
	params["ids"] = notificationIDList(ids)
	column := "[[read_at]]"
	if change == notificationStateChangeUnarchive {
		column = "[[archived_at]]"
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notifications}}
		SET ` + column + `=0,[[updated_at]]={:changedAt}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND ` + notificationStateMovable(change) + ` AND ` + notificationShownSQL("") + `
			AND ` + notificationIDListSQL("ids") + `
	`).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	return int(changed), err
}

func notificationStateMovable(change string) string {
	if change == notificationStateChangeUnarchive {
		return "[[archived_at]]!=0"
	}
	return "[[read_at]]!=0"
}
