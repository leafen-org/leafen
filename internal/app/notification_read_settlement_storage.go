package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedNotificationReadSettlement struct {
	ID                string `db:"id"`
	ActorID           string `db:"actor_id"`
	WorkspaceID       string `db:"workspace_id"`
	RequestID         string `db:"request_id"`
	RequestHash       string `db:"request_hash"`
	Mode              string `db:"mode"`
	Status            string `db:"status"`
	ThroughOccurredAt int64  `db:"through_occurred_at"`
	ThroughID         string `db:"through_id"`
	CursorOccurredAt  int64  `db:"cursor_occurred_at"`
	CursorID          string `db:"cursor_id"`
	ReadAt            int64  `db:"read_at"`
	ReadCount         int    `db:"read_count"`
	Revision          int    `db:"revision"`
	CreatedAt         int64  `db:"created_at"`
	UpdatedAt         int64  `db:"updated_at"`
	CompletedAt       int64  `db:"completed_at"`
}

type storedNotificationReadCandidate struct {
	ID         string `db:"id"`
	PageID     string `db:"page_id"`
	Target     string `db:"target"`
	OccurredAt int64  `db:"occurred_at"`
}

func loadNotificationReadSettlementByRequest(
	app core.App,
	actorID string,
	requestID string,
) (storedNotificationReadSettlement, bool, error) {
	row := storedNotificationReadSettlement{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[actor_id]],[[workspace_id]],[[request_id]],[[request_hash]],
			[[mode]],[[status]],[[through_occurred_at]],[[through_id]],
			[[cursor_occurred_at]],[[cursor_id]],[[read_at]],[[read_count]],[[revision]],
			[[created_at]],[[updated_at]],[[completed_at]]
		FROM {{leafen_notification_read_settlements}}
		INDEXED BY [[idx_leafen_notification_read_settlements_request]]
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotificationReadSettlement{}, false, nil
	}
	return row, err == nil, err
}

func loadNotificationReadSettlement(
	app core.App,
	workspaceID string,
	settlementID string,
) (storedNotificationReadSettlement, error) {
	row := storedNotificationReadSettlement{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[actor_id]],[[workspace_id]],[[request_id]],[[request_hash]],
			[[mode]],[[status]],[[through_occurred_at]],[[through_id]],
			[[cursor_occurred_at]],[[cursor_id]],[[read_at]],[[read_count]],[[revision]],
			[[created_at]],[[updated_at]],[[completed_at]]
		FROM {{leafen_notification_read_settlements}}
		WHERE [[workspace_id]]={:workspaceId} AND [[id]]={:settlementId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "settlementId": settlementID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotificationReadSettlement{}, errNotificationReadNotFound
	}
	return row, err
}

func selectedNotificationReadCandidates(
	app core.App,
	actorID string,
	workspaceID string,
	notificationIDs []string,
) ([]storedNotificationReadCandidate, error) {
	// A line of the inbox is read whole (notification_inbox_groups.go).
	notificationIDs, err := expandNotificationGroups(app, actorID, workspaceID, notificationIDs)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"workspaceId": workspaceID, "userId": actorID}
	params["ids"] = notificationIDList(notificationIDs)
	candidates := []storedNotificationReadCandidate{}
	err = app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[target]],[[occurred_at]] FROM {{leafen_notifications}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND [[read_at]]=0 AND [[archived_at]]=0 AND ` + notificationShownSQL("") + `
			AND ` + notificationIDListSQL("ids") + `
		ORDER BY [[id]]
	`).Bind(params).All(&candidates)
	return candidates, err
}

func markVisibleNotificationCandidatesRead(
	app core.App,
	row *storedNotificationReadSettlement,
	candidates []storedNotificationReadCandidate,
) (int, error) {
	notices := make([]notificationSubjectRef, 0, len(candidates))
	for _, candidate := range candidates {
		notices = append(notices, notificationSubjectRef{PageID: candidate.PageID, Target: candidate.Target})
	}
	shown, err := shownNotificationSubjects(app, row.ActorID, row.WorkspaceID, notices)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(candidates))
	for index, candidate := range candidates {
		if shown[index] {
			ids = append(ids, candidate.ID)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	params := dbx.Params{
		"workspaceId": row.WorkspaceID, "userId": row.ActorID, "readAt": row.ReadAt,
	}
	params["ids"] = notificationIDList(ids)
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notifications}}
		SET [[read_at]]={:readAt},[[updated_at]]={:readAt}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND [[read_at]]=0 AND [[archived_at]]=0 AND ` + notificationShownSQL("") + `
			AND ` + notificationIDListSQL("ids") + `
	`).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	return int(changed), err
}

func persistNotificationReadProgress(
	app core.App,
	row *storedNotificationReadSettlement,
) error {
	previousRevision := row.Revision
	now := time.Now().UTC().UnixMilli()
	if now < row.UpdatedAt {
		now = row.UpdatedAt
	}
	row.Revision++
	row.UpdatedAt = now
	if row.Status == notificationReadStatusComplete {
		row.CompletedAt = now
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_read_settlements}}
		SET [[status]]={:status},[[cursor_occurred_at]]={:cursorOccurred},
			[[cursor_id]]={:cursorId},[[read_count]]={:readCount},[[revision]]={:revision},
			[[updated_at]]={:updatedAt},[[completed_at]]={:completedAt}
		WHERE [[id]]={:settlementId} AND [[revision]]={:previousRevision}
	`).Bind(dbx.Params{
		"status": row.Status, "cursorOccurred": row.CursorOccurredAt,
		"cursorId": row.CursorID, "readCount": row.ReadCount, "revision": row.Revision,
		"updatedAt": row.UpdatedAt, "completedAt": row.CompletedAt,
		"settlementId": row.ID, "previousRevision": previousRevision,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errNotificationReadStateConflict
	}
	return nil
}
