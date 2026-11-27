package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notificationArchiveStatusComplete = "complete"

var (
	errNotificationArchiveInvalid         = errors.New(errorCodeCommentNotificationArchiveInvalid)
	errNotificationArchiveRequestConflict = errors.New(errorCodeCommentNotificationArchiveRequestConflict)
)

type normalizedNotificationArchiveInput struct {
	RequestID       string
	NotificationIDs []string
	RequestHash     string
}

type storedNotificationArchiveReceipt struct {
	ID            string `db:"id"`
	ActorID       string `db:"actor_id"`
	WorkspaceID   string `db:"workspace_id"`
	RequestID     string `db:"request_id"`
	RequestHash   string `db:"request_hash"`
	Status        string `db:"status"`
	ArchivedAt    int64  `db:"archived_at"`
	ArchivedCount int    `db:"archived_count"`
	CreatedAt     int64  `db:"created_at"`
}

type storedNotificationArchiveCandidate struct {
	ID     string `db:"id"`
	PageID string `db:"page_id"`
	Target string `db:"target"`
}

func normalizeNotificationArchiveInput(
	workspaceID string,
	input notificationArchiveInput,
) (normalizedNotificationArchiveInput, error) {
	requestID := strings.TrimSpace(input.RequestID)
	if strings.TrimSpace(workspaceID) == "" {
		return normalizedNotificationArchiveInput{}, refuse(errNotificationArchiveInvalid, "the workspace ID in the path is empty")
	}
	if !validCommentIdentifier(requestID) {
		return normalizedNotificationArchiveInput{}, notificationRequestIDRefusal(errNotificationArchiveInvalid)
	}
	if err := notificationIDsRefusal(errNotificationArchiveInvalid, input.NotificationIDs, notificationArchiveSelectedSize); err != nil {
		return normalizedNotificationArchiveInput{}, err
	}
	seen := make(map[string]struct{}, len(input.NotificationIDs))
	for _, rawID := range input.NotificationIDs {
		id := strings.TrimSpace(rawID)
		seen[id] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	digest := sha256.Sum256([]byte(strings.Join(
		append([]string{"notification-archive", workspaceID}, ids...), "\x00",
	)))
	return normalizedNotificationArchiveInput{
		RequestID: requestID, NotificationIDs: ids, RequestHash: hex.EncodeToString(digest[:]),
	}, nil
}

func notificationArchiveReceiptID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte("notification-archive\x00" + actorID + "\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}

func renderNotificationArchiveReceipt(
	row storedNotificationArchiveReceipt,
) notificationArchiveReceipt {
	return notificationArchiveReceipt{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Status: row.Status,
		ArchivedAt: notificationInboxTime(row.ArchivedAt), ArchivedCount: row.ArchivedCount,
		CreatedAt: notificationInboxTime(row.CreatedAt),
	}
}

func settleNotificationArchive(
	app core.App,
	actorID string,
	workspaceID string,
	input notificationArchiveInput,
) (notificationArchiveResponse, error) {
	normalized, err := normalizeNotificationArchiveInput(workspaceID, input)
	if err != nil {
		return notificationArchiveResponse{}, err
	}
	if strings.TrimSpace(actorID) == "" {
		return notificationArchiveResponse{}, errNotificationArchiveInvalid
	}
	response := notificationArchiveResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, exists, err := loadNotificationArchiveReceipt(
			txApp, actorID, normalized.RequestID,
		)
		if err != nil {
			return err
		}
		if exists {
			if _, err := authorizeCommentNotificationScope(
				txApp, actorID, workspaceID,
			); err != nil {
				return err
			}
			if existing.WorkspaceID != workspaceID || existing.RequestHash != normalized.RequestHash {
				return errNotificationArchiveRequestConflict
			}
			response = notificationArchiveResponse{
				Receipt: renderNotificationArchiveReceipt(existing), Duplicate: true,
			}
			return nil
		}
		candidates, err := selectNotificationArchiveCandidates(
			txApp, actorID, workspaceID, normalized.NotificationIDs,
		)
		if err != nil {
			return err
		}
		ids, err := visibleNotificationArchiveIDs(txApp, actorID, workspaceID, candidates)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		changed, err := updateNotificationsArchived(txApp, actorID, workspaceID, ids, now)
		if err != nil {
			return err
		}
		row := storedNotificationArchiveReceipt{
			ID:      notificationArchiveReceiptID(actorID, normalized.RequestID),
			ActorID: actorID, WorkspaceID: workspaceID, RequestID: normalized.RequestID,
			RequestHash: normalized.RequestHash, Status: notificationArchiveStatusComplete,
			ArchivedAt: now, ArchivedCount: changed, CreatedAt: now,
		}
		if _, err := txApp.DB().Insert(notificationArchiveReceiptsTable, dbx.Params{
			"id": row.ID, "actor_id": row.ActorID, "workspace_id": row.WorkspaceID,
			"request_id": row.RequestID, "request_hash": row.RequestHash, "status": row.Status,
			"archived_at": row.ArchivedAt, "archived_count": row.ArchivedCount,
			"created_at": row.CreatedAt,
		}).Execute(); err != nil {
			return err
		}
		response.Receipt = renderNotificationArchiveReceipt(row)
		return nil
	})
	return response, err
}

func loadNotificationArchiveReceipt(
	app core.App,
	actorID string,
	requestID string,
) (storedNotificationArchiveReceipt, bool, error) {
	row := storedNotificationArchiveReceipt{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[actor_id]],[[workspace_id]],[[request_id]],[[request_hash]],
			[[status]],[[archived_at]],[[archived_count]],[[created_at]]
		FROM {{leafen_notification_archive_receipts}}
		INDEXED BY [[idx_leafen_notification_archive_receipts_request]]
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotificationArchiveReceipt{}, false, nil
	}
	return row, err == nil, err
}

func selectNotificationArchiveCandidates(
	app core.App,
	actorID string,
	workspaceID string,
	ids []string,
) ([]storedNotificationArchiveCandidate, error) {
	// A line of the inbox goes to the archive whole (notification_inbox_groups.go).
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
			AND [[archived_at]]=0 AND ` + notificationShownSQL("") + `
			AND ` + notificationIDListSQL("ids") + `
		ORDER BY [[id]]
	`).Bind(params).All(&rows)
	return rows, err
}

func visibleNotificationArchiveIDs(
	app core.App,
	actorID string,
	workspaceID string,
	candidates []storedNotificationArchiveCandidate,
) ([]string, error) {
	notices := make([]notificationSubjectRef, 0, len(candidates))
	for _, candidate := range candidates {
		notices = append(notices, notificationSubjectRef{PageID: candidate.PageID, Target: candidate.Target})
	}
	shown, err := shownNotificationSubjects(app, actorID, workspaceID, notices)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(candidates))
	for index, candidate := range candidates {
		if shown[index] {
			ids = append(ids, candidate.ID)
		}
	}
	return ids, nil
}

func updateNotificationsArchived(
	app core.App,
	actorID string,
	workspaceID string,
	ids []string,
	archivedAt int64,
) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "userId": actorID, "archivedAt": archivedAt,
	}
	params["ids"] = notificationIDList(ids)
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notifications}}
		SET [[archived_at]]={:archivedAt},[[updated_at]]={:archivedAt}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND [[archived_at]]=0 AND ` + notificationShownSQL("") + `
			AND ` + notificationIDListSQL("ids") + `
	`).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	return int(changed), err
}
