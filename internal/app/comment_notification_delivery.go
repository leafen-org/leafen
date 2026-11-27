package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const errorCodeCommentNotificationDeliveryFailed = "LEAFEN_NOTIFICATION_DELIVERY_FAILED"

var errCommentNotificationDeliveryConflict = errors.New("comment notification delivery state conflict")

type storedCommentNotificationDelivery struct {
	ID             string `db:"id"`
	EventID        string `db:"event_id"`
	WorkspaceID    string `db:"workspace_id"`
	UserID         string `db:"user_id"`
	DeliveryKind   string `db:"delivery_kind"`
	DeliveryState  string `db:"delivery_status"`
	Attempts       int    `db:"attempts"`
	NextAttemptAt  int64  `db:"next_attempt_at"`
	EventStatus    string `db:"event_status"`
	SourceKind     string `db:"source_kind"`
	SourceID       string `db:"source_id"`
	SourceRevision int    `db:"source_revision"`
	ActorID        string `db:"actor_id"`
	PageID         string `db:"page_id"`
	BlockID        string `db:"block_id"`
	ParentID       string `db:"parent_comment_id"`
	Title          string `db:"title"`
	Preview        string `db:"preview"`
	Target         string `db:"target"`
	OccurredAt     int64  `db:"audience_at"`
	// ActorConnection names the connection whose write it is, when one's
	// (noticeConnection).
	ActorConnection string `db:"actor_connection"`
}

type commentNotificationRunnableDelivery struct {
	ID       string `db:"id"`
	Attempts int    `db:"attempts"`
}

func commentNotificationActivityKey(delivery storedCommentNotificationDelivery) string {
	if delivery.DeliveryKind == "mention" {
		return fmt.Sprintf(
			"mention:%s:%s:%s:%d",
			delivery.SourceKind, delivery.SourceID, delivery.UserID, delivery.SourceRevision,
		)
	}
	return fmt.Sprintf("comment:%s:%d", delivery.SourceID, delivery.SourceRevision)
}

func commentNotificationID(workspaceID, userID, activityKey string) string {
	digest := sha256.Sum256([]byte(strings.Join(
		[]string{"comment-notification", workspaceID, userID, activityKey}, "\x00",
	)))
	return hex.EncodeToString(digest[:16])
}

// deliverCommentNotificationEventSlice delivers a slice of an event's
// recipients in one commit, as many as fit in commentNotificationDeliveryHold,
// after people's edits (write_lane.go); the rest wait for the next turn. When
// that commit fails, each recipient is delivered on its own, so one that
// fails holds back no other and alone is retried.
func deliverCommentNotificationEventSlice(
	ctx context.Context,
	app core.App,
	eventID string,
	now time.Time,
) (int, error) {
	rows, err := listCommentNotificationRunnableDeliveries(ctx, app, eventID, now)
	if err != nil {
		return 0, err
	}
	yieldToPeople(ctx)
	processed := 0
	err = app.RunInTransaction(func(txApp core.App) error {
		processed = 0
		// The recipients' access is judged in one read of the commit: what
		// they share and the page's place, is decided once.
		access := withAccessMemo(txApp)
		started := time.Now()
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if processed > 0 && time.Since(started) >= commentNotificationDeliveryHold {
				break
			}
			if err := settleCommentNotificationDeliveryIn(ctx, txApp, access, row.ID, now); err != nil {
				return err
			}
			processed++
		}
		return settleCommentNotificationEventIn(ctx, txApp, eventID, now)
	})
	if err == nil || ctx.Err() != nil {
		return processed, err
	}
	return deliverCommentNotificationsOneByOne(ctx, app, eventID, rows, now)
}

// deliverCommentNotificationsOneByOne delivers each recipient in its own
// commit, scheduling a retry for one that fails.
func deliverCommentNotificationsOneByOne(
	ctx context.Context,
	app core.App,
	eventID string,
	rows []commentNotificationRunnableDelivery,
	now time.Time,
) (int, error) {
	errs := []error{}
	processed := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return processed, errors.Join(append(errs, err)...)
		}
		processed++
		err := settleCommentNotificationDelivery(ctx, app, row.ID, now)
		if err == nil {
			continue
		}
		errs = append(errs, err)
		if retryErr := scheduleCommentNotificationDeliveryRetry(
			ctx, app, row.ID, row.Attempts, now,
		); retryErr != nil {
			errs = append(errs, retryErr)
		}
	}
	if err := settleCommentNotificationEvent(ctx, app, eventID, now); err != nil {
		errs = append(errs, err)
	}
	return processed, errors.Join(errs...)
}

func listCommentNotificationRunnableDeliveries(
	ctx context.Context,
	app core.App,
	eventID string,
	now time.Time,
) ([]commentNotificationRunnableDelivery, error) {
	rows := []commentNotificationRunnableDelivery{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[attempts]] FROM {{leafen_notification_deliveries}}
			INDEXED BY [[idx_leafen_notification_deliveries_runnable]]
		WHERE [[event_id]]={:eventId} AND [[status]]='pending'
		ORDER BY [[next_attempt_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"eventId": eventID, "limit": commentNotificationDeliveryRecipientLimit,
	}).All(&rows); err != nil || len(rows) > 0 {
		return rows, err
	}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[attempts]] FROM {{leafen_notification_deliveries}}
			INDEXED BY [[idx_leafen_notification_deliveries_runnable]]
		WHERE [[event_id]]={:eventId} AND [[status]]='retrying'
			AND [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"eventId": eventID, "now": now.UnixMilli(),
		"limit": commentNotificationDeliveryRecipientLimit,
	}).All(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func settleCommentNotificationDelivery(
	ctx context.Context,
	app core.App,
	deliveryID string,
	now time.Time,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		return settleCommentNotificationDeliveryIn(ctx, txApp, txApp, deliveryID, now)
	})
}

// settleCommentNotificationDeliveryIn delivers one recipient's notification
// in a commit, judging their access with access, the commit's app or one
// keeping its decisions.
func settleCommentNotificationDeliveryIn(
	ctx context.Context,
	txApp core.App,
	access core.App,
	deliveryID string,
	now time.Time,
) error {
	delivery, err := loadCommentNotificationDelivery(ctx, txApp, deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if delivery.DeliveryState != "pending" && delivery.DeliveryState != "retrying" {
		return nil
	}
	if delivery.DeliveryState == "retrying" && delivery.NextAttemptAt > now.UnixMilli() {
		return nil
	}
	if delivery.EventStatus != "delivering" && delivery.EventStatus != "retrying" {
		return nil
	}
	delivered := false
	// A comment notifies its audience; a page's text notifies the people
	// it still @mentions.
	if delivery.UserID != delivery.ActorID && (delivery.SourceKind == "comment" || delivery.SourceKind == "block") {
		authErr := authorizeNotificationSubject(access, delivery.UserID, delivery.PageID, delivery.Target)
		current, personal := true, true
		if authErr == nil && delivery.SourceKind == "block" {
			current, err = pagePersonMentionCurrent(ctx, txApp, delivery.PageID, delivery.SourceID, delivery.UserID)
		} else if authErr == nil {
			personal, err = commentNotificationPersonal(ctx, txApp, delivery)
		}
		if err != nil {
			return err
		}
		if authErr == nil && current {
			// A person's notifications are kept, read or not, as in Notion's
			// inbox and archive (2026-10-13: past 500 the oldest read ones
			// were deleted).
			if _, err := upsertCommentNotification(ctx, txApp, delivery, personal, now); err != nil {
				return err
			}
			delivered = true
		} else if authErr != nil && !isPermissionDenied(authErr) {
			return authErr
		}
	}
	return settleCommentNotificationDeliveryRow(ctx, txApp, delivery, delivered, now)
}

func loadCommentNotificationDelivery(
	ctx context.Context,
	app core.App,
	deliveryID string,
) (storedCommentNotificationDelivery, error) {
	row := storedCommentNotificationDelivery{}
	err := app.DB().NewQuery(`
		SELECT [[delivery]].[[id]], [[delivery]].[[event_id]],
			[[delivery]].[[workspace_id]], [[delivery]].[[user_id]],
			[[delivery]].[[kind]] AS [[delivery_kind]],
			[[delivery]].[[status]] AS [[delivery_status]],
			[[delivery]].[[attempts]], [[delivery]].[[next_attempt_at]],
			[[event]].[[status]] AS [[event_status]], [[event]].[[source_kind]],
			[[event]].[[source_id]], [[event]].[[source_revision]], [[event]].[[actor_id]],
			[[event]].[[page_id]], [[event]].[[block_id]], [[event]].[[parent_comment_id]], [[event]].[[title]],
			[[event]].[[preview]], [[event]].[[target]], [[event]].[[audience_at]], [[event]].[[actor_connection]]
		FROM {{leafen_notification_deliveries}} [[delivery]]
		JOIN {{leafen_notification_events}} [[event]] ON [[event]].[[id]]=[[delivery]].[[event_id]]
		WHERE [[delivery]].[[id]]={:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": deliveryID}).One(&row)
	return row, err
}

func upsertCommentNotification(
	ctx context.Context,
	app core.App,
	delivery storedCommentNotificationDelivery,
	personal bool,
	now time.Time,
) (bool, error) {
	activityKey := commentNotificationActivityKey(delivery)
	kind := "comment"
	if delivery.DeliveryKind == "mention" {
		kind = "mention"
	}
	commentID := ""
	if delivery.SourceKind == "comment" {
		commentID = delivery.SourceID
	}
	return upsertProductNotification(ctx, app, productNotificationProjection{
		ID:           commentNotificationID(delivery.WorkspaceID, delivery.UserID, activityKey),
		EventID:      delivery.EventID,
		WorkspaceID:  delivery.WorkspaceID,
		UserID:       delivery.UserID,
		ActivityKey:  activityKey,
		Kind:         kind,
		PageID:       delivery.PageID,
		BlockID:      delivery.BlockID,
		CommentID:    commentID,
		ActorID:      delivery.ActorID,
		Title:        delivery.Title,
		Preview:      delivery.Preview,
		PreviewSpans: sourceLineSpans(app, delivery),
		Target:       delivery.Target,
		OccurredAt:   delivery.OccurredAt,
		Personal:     personal,
		// The connection a write of a page's text was made through.
		ActorConnection: delivery.ActorConnection,
	}, now.UnixMilli())
}

func settleCommentNotificationDeliveryRow(
	ctx context.Context,
	app core.App,
	delivery storedCommentNotificationDelivery,
	delivered bool,
	now time.Time,
) error {
	status := "skipped"
	deliveredAt := int64(0)
	skippedAt := now.UnixMilli()
	if delivered {
		status = "delivered"
		deliveredAt = now.UnixMilli()
		skippedAt = 0
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_deliveries}}
		SET [[status]]={:status}, [[attempts]]={:attempts}, [[next_attempt_at]]=0,
			[[last_error_code]]='', [[delivered_at]]={:deliveredAt},
			[[skipped_at]]={:skippedAt}, [[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[status]]={:currentStatus} AND [[attempts]]={:currentAttempts}
	`).WithContext(ctx).Bind(dbx.Params{
		"id": delivery.ID, "status": status, "attempts": delivery.Attempts + 1,
		"deliveredAt": deliveredAt, "skippedAt": skippedAt, "now": now.UnixMilli(),
		"currentStatus": delivery.DeliveryState, "currentAttempts": delivery.Attempts,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errCommentNotificationDeliveryConflict
	}
	return nil
}

func scheduleCommentNotificationDeliveryRetry(
	ctx context.Context,
	app core.App,
	deliveryID string,
	currentAttempts int,
	now time.Time,
) error {
	nextAttempts := currentAttempts + 1
	nextAttemptAt := now.Add(commentNotificationDeliveryRetryDelay(nextAttempts)).UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_deliveries}}
		SET [[status]]='retrying', [[attempts]]={:nextAttempts},
			[[next_attempt_at]]={:nextAttemptAt}, [[last_error_code]]={:errorCode},
			[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[status]] IN ('pending','retrying')
			AND [[attempts]]={:currentAttempts}
	`).WithContext(ctx).Bind(dbx.Params{
		"id": deliveryID, "currentAttempts": currentAttempts,
		"nextAttempts": nextAttempts, "nextAttemptAt": nextAttemptAt,
		"errorCode": errorCodeCommentNotificationDeliveryFailed, "now": now.UnixMilli(),
	}).Execute()
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

func commentNotificationDeliveryRetryDelay(attempt int) time.Duration {
	delay := commentNotificationMaterializationFailureDelay
	for step := 1; step < attempt && delay < commentNotificationMaterializationMaxRetryDelay; step++ {
		delay *= 2
		if delay > commentNotificationMaterializationMaxRetryDelay {
			return commentNotificationMaterializationMaxRetryDelay
		}
	}
	return delay
}

func settleCommentNotificationEvent(
	ctx context.Context,
	app core.App,
	eventID string,
	now time.Time,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		return settleCommentNotificationEventIn(ctx, txApp, eventID, now)
	})
}

// settleCommentNotificationEventIn marks an event delivered, waiting for its
// retries, or still delivering, in a commit.
func settleCommentNotificationEventIn(
	ctx context.Context,
	txApp core.App,
	eventID string,
	now time.Time,
) error {
	var pending bool
	if err := txApp.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_notification_deliveries}}
			WHERE [[event_id]]={:eventId} AND [[status]]='pending' LIMIT 1
		)
	`).WithContext(ctx).Bind(dbx.Params{"eventId": eventID}).Row(&pending); err != nil {
		return err
	}
	status := "delivering"
	nextAttemptAt := int64(0)
	completedAt := int64(0)
	if !pending {
		var retry sql.NullInt64
		if err := txApp.DB().NewQuery(`
			SELECT MIN([[next_attempt_at]]) FROM {{leafen_notification_deliveries}}
			WHERE [[event_id]]={:eventId} AND [[status]]='retrying'
		`).WithContext(ctx).Bind(dbx.Params{"eventId": eventID}).Row(&retry); err != nil {
			return err
		}
		if retry.Valid {
			status = "retrying"
			nextAttemptAt = retry.Int64
		} else {
			status = "complete"
			completedAt = now.UnixMilli()
		}
	}
	_, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_notification_events}}
		SET [[status]]={:status}, [[delivery_next_attempt_at]]={:nextAttemptAt},
			[[completed_at]]={:completedAt}, [[updated_at]]={:updatedAt}
		WHERE [[id]]={:eventId} AND [[status]] IN ('delivering','retrying')
	`).WithContext(ctx).Bind(dbx.Params{
		"eventId": eventID, "status": status, "nextAttemptAt": nextAttemptAt,
		"completedAt": completedAt, "updatedAt": now.UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err
}

func logCommentNotificationDeliveryFailure(app core.App, eventID string, err error) {
	logDurableJobDiagnostic(
		app, errorCodeCommentNotificationDeliveryFailed, newDiagnosticReference(), "",
		"notification.delivery.continue", eventID, err,
	)
}
