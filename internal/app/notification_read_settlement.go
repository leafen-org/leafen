package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notificationReadModeSelected   = "selected"
	notificationReadModeAll        = "all"
	notificationReadStatusPending  = "pending"
	notificationReadStatusComplete = "complete"
)

var (
	errNotificationReadInvalid         = errors.New(errorCodeCommentNotificationReadInvalid)
	errNotificationReadRequestConflict = errors.New(errorCodeCommentNotificationReadRequestConflict)
	errNotificationReadNotFound        = errors.New(errorCodeCommentNotificationReadNotFound)
	errNotificationReadStateConflict   = errors.New(errorCodeCommentNotificationReadStateConflict)
)

type normalizedNotificationReadInput struct {
	RequestID         string
	Mode              string
	NotificationIDs   []string
	Through           *notificationInboxBoundary
	ThroughOccurredAt int64
	// ThroughSpan is how many milliseconds through's time as written covers
	// (notification_read_through.go).
	ThroughSpan int64
	RequestHash string
}

func normalizeNotificationReadInput(
	workspaceID string,
	input notificationReadInput,
) (normalizedNotificationReadInput, error) {
	requestID := strings.TrimSpace(input.RequestID)
	mode := strings.TrimSpace(input.Mode)
	if strings.TrimSpace(workspaceID) == "" {
		return normalizedNotificationReadInput{}, refuse(errNotificationReadInvalid, "the workspace ID in the path is empty")
	}
	if !validCommentIdentifier(requestID) {
		return normalizedNotificationReadInput{}, notificationRequestIDRefusal(errNotificationReadInvalid)
	}
	normalized := normalizedNotificationReadInput{RequestID: requestID, Mode: mode}
	switch mode {
	case notificationReadModeSelected:
		if input.Through != nil {
			return normalizedNotificationReadInput{}, refuse(errNotificationReadInvalid, "through goes only with mode all")
		}
		if err := notificationIDsRefusal(errNotificationReadInvalid, input.NotificationIDs, notificationReadSelectedSize); err != nil {
			return normalizedNotificationReadInput{}, err
		}
		seen := map[string]struct{}{}
		for _, rawID := range input.NotificationIDs {
			seen[strings.TrimSpace(rawID)] = struct{}{}
		}
		normalized.NotificationIDs = make([]string, 0, len(seen))
		for id := range seen {
			normalized.NotificationIDs = append(normalized.NotificationIDs, id)
		}
		sort.Strings(normalized.NotificationIDs)
	case notificationReadModeAll:
		if len(input.NotificationIDs) != 0 {
			return normalizedNotificationReadInput{}, refuse(errNotificationReadInvalid, "notificationIds goes only with mode selected")
		}
		if input.Through == nil || !validNotificationReadID(strings.TrimSpace(input.Through.ID)) {
			return normalizedNotificationReadInput{}, refuse(errNotificationReadInvalid, "through names the newest notification read, by its id and its time (occurredAt) as the inbox list or leafen_inbox gives them")
		}
		throughTime, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.Through.OccurredAt))
		if err != nil || throughTime.UnixMilli() <= 0 {
			return normalizedNotificationReadInput{}, refuse(errNotificationReadInvalid, "through.occurredAt is a time as RFC 3339 writes it, as the inbox list or leafen_inbox gives it")
		}
		normalized.ThroughOccurredAt = throughTime.UnixMilli()
		normalized.ThroughSpan = notificationReadThroughSpan(input.Through.OccurredAt)
		normalized.Through = &notificationInboxBoundary{
			OccurredAt: notificationInboxTime(normalized.ThroughOccurredAt),
			ID:         strings.TrimSpace(input.Through.ID),
		}
	default:
		return normalizedNotificationReadInput{}, refuse(errNotificationReadInvalid, "mode is %s or %s, not %q", notificationReadModeSelected, notificationReadModeAll, mode)
	}
	normalized.RequestHash = notificationReadRequestHash(workspaceID, normalized)
	return normalized, nil
}

func validNotificationReadID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func notificationReadRequestHash(
	workspaceID string,
	input normalizedNotificationReadInput,
) string {
	parts := []string{"notification-read", workspaceID, input.Mode}
	if input.Mode == notificationReadModeSelected {
		parts = append(parts, input.NotificationIDs...)
	} else {
		parts = append(parts, strconv.FormatInt(input.ThroughOccurredAt, 10), input.Through.ID)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func notificationReadSettlementID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte("notification-read\x00" + actorID + "\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}

func renderNotificationReadSettlement(
	row storedNotificationReadSettlement,
) notificationReadSettlement {
	result := notificationReadSettlement{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Mode: row.Mode, Status: row.Status,
		ReadAt: notificationInboxTime(row.ReadAt), ReadCount: row.ReadCount,
		Revision: row.Revision, CreatedAt: notificationInboxTime(row.CreatedAt),
		UpdatedAt:   notificationInboxTime(row.UpdatedAt),
		CompletedAt: notificationInboxTime(row.CompletedAt),
	}
	if row.Mode == notificationReadModeAll {
		result.Through = &notificationInboxBoundary{
			OccurredAt: notificationInboxTime(row.ThroughOccurredAt), ID: row.ThroughID,
		}
	}
	return result
}

func settleNotificationRead(
	app core.App,
	actorID string,
	workspaceID string,
	input notificationReadInput,
) (notificationReadResponse, error) {
	normalized, err := normalizeNotificationReadInput(workspaceID, input)
	if err != nil {
		return notificationReadResponse{}, err
	}
	if strings.TrimSpace(actorID) == "" {
		return notificationReadResponse{}, errNotificationReadInvalid
	}
	response := notificationReadResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, exists, err := loadNotificationReadSettlementByRequest(
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
				return errNotificationReadRequestConflict
			}
			settlement, err := renderNotificationReadAnswer(txApp, existing)
			response = notificationReadResponse{Settlement: settlement, Duplicate: true}
			return err
		}
		// Through the notice named as it is stored, when the time given is
		// its time written coarser (notification_read_through.go).
		if err := notificationReadThroughAsStored(txApp, actorID, workspaceID, &normalized); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		row := storedNotificationReadSettlement{
			ID:      notificationReadSettlementID(actorID, normalized.RequestID),
			ActorID: actorID, WorkspaceID: workspaceID, RequestID: normalized.RequestID,
			RequestHash: normalized.RequestHash, Mode: normalized.Mode,
			Status: notificationReadStatusPending, ThroughOccurredAt: normalized.ThroughOccurredAt,
			ReadAt: now, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		if normalized.Through != nil {
			row.ThroughID = normalized.Through.ID
		}
		if _, err := txApp.DB().Insert(notificationReadSettlementsTable, dbx.Params{
			"id": row.ID, "actor_id": row.ActorID, "workspace_id": row.WorkspaceID,
			"request_id": row.RequestID, "request_hash": row.RequestHash,
			"mode": row.Mode, "status": row.Status,
			"through_occurred_at": row.ThroughOccurredAt, "through_id": row.ThroughID,
			"cursor_occurred_at": 0, "cursor_id": "", "read_at": row.ReadAt,
			"read_count": 0, "revision": row.Revision,
			"created_at": now, "updated_at": now, "completed_at": 0,
		}).Execute(); err != nil {
			return err
		}
		if row.Mode == notificationReadModeSelected {
			err = processSelectedNotificationRead(txApp, &row, normalized.NotificationIDs)
		} else {
			err = processAllNotificationRead(txApp, &row)
		}
		if err != nil {
			return err
		}
		response.Settlement, err = renderNotificationReadAnswer(txApp, row)
		return err
	})
	return response, err
}

func readNotificationReadSettlement(
	app core.App,
	actorID string,
	workspaceID string,
	settlementID string,
) (notificationReadResponse, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(workspaceID) == "" {
		return notificationReadResponse{}, errNotificationReadInvalid
	}
	// An ID no job can have names none.
	if !validNotificationReadID(strings.TrimSpace(settlementID)) {
		return notificationReadResponse{}, errNotificationReadNotFound
	}
	response := notificationReadResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		if _, err := authorizeCommentNotificationScope(
			txApp, actorID, workspaceID,
		); err != nil {
			return err
		}
		row, err := loadNotificationReadSettlement(txApp, workspaceID, settlementID)
		if err != nil {
			return err
		}
		if row.ActorID != actorID {
			return permissionDenied()
		}
		response.Settlement, err = renderNotificationReadAnswer(txApp, row)
		return err
	})
	return response, err
}

func continueNotificationReadSettlement(
	app core.App,
	actorID string,
	workspaceID string,
	settlementID string,
) (notificationReadResponse, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(workspaceID) == "" {
		return notificationReadResponse{}, errNotificationReadInvalid
	}
	// An ID no job can have names none.
	if !validNotificationReadID(strings.TrimSpace(settlementID)) {
		return notificationReadResponse{}, errNotificationReadNotFound
	}
	response := notificationReadResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		row, err := loadNotificationReadSettlement(txApp, workspaceID, settlementID)
		if err != nil {
			return err
		}
		if row.ActorID != actorID {
			return permissionDenied()
		}
		if row.Status == notificationReadStatusPending {
			if row.Mode != notificationReadModeAll {
				return errNotificationReadStateConflict
			}
			if err := processAllNotificationRead(txApp, &row); err != nil {
				return err
			}
		} else if _, err := authorizeCommentNotificationScope(
			txApp, actorID, workspaceID,
		); err != nil {
			return err
		}
		response.Settlement, err = renderNotificationReadAnswer(txApp, row)
		return err
	})
	return response, err
}

func processSelectedNotificationRead(
	app core.App,
	row *storedNotificationReadSettlement,
	notificationIDs []string,
) error {
	candidates, err := selectedNotificationReadCandidates(
		app, row.ActorID, row.WorkspaceID, notificationIDs,
	)
	if err != nil {
		return err
	}
	changed, err := markVisibleNotificationCandidatesRead(app, row, candidates)
	if err != nil {
		return err
	}
	row.ReadCount += changed
	row.Status = notificationReadStatusComplete
	return persistNotificationReadProgress(app, row)
}

func processAllNotificationRead(
	app core.App,
	row *storedNotificationReadSettlement,
) error {
	candidates := []storedNotificationReadCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[target]],[[occurred_at]]
		FROM {{leafen_notifications}}
		INDEXED BY [[idx_leafen_notifications_user_activity]]
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND [[read_at]]=0 AND [[archived_at]]=0 AND ` + notificationShownSQL("") + `
			AND ([[occurred_at]] < {:throughOccurred}
				OR ([[occurred_at]]={:throughOccurred} AND [[id]]<={:throughId}))
			AND ({:cursorOccurred}=0 OR [[occurred_at]] < {:cursorOccurred}
				OR ([[occurred_at]]={:cursorOccurred} AND [[id]]<{:cursorId}))
		ORDER BY [[occurred_at]] DESC, [[id]] DESC
		LIMIT {:turnLimit}
	`).Bind(dbx.Params{
		"workspaceId": row.WorkspaceID, "userId": row.ActorID,
		"throughOccurred": row.ThroughOccurredAt, "throughId": row.ThroughID,
		"cursorOccurred": row.CursorOccurredAt, "cursorId": row.CursorID,
		"turnLimit": notificationReadTurnSize,
	}).All(&candidates)
	if err != nil {
		return err
	}
	changed, err := markVisibleNotificationCandidatesRead(app, row, candidates)
	if err != nil {
		return err
	}
	row.ReadCount += changed
	if len(candidates) > 0 {
		row.CursorOccurredAt = candidates[len(candidates)-1].OccurredAt
		row.CursorID = candidates[len(candidates)-1].ID
	}
	if len(candidates) < notificationReadTurnSize {
		row.Status = notificationReadStatusComplete
	}
	return persistNotificationReadProgress(app, row)
}
