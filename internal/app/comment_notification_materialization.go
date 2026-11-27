package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const errorCodeCommentNotificationMaterializationFailed = "LEAFEN_NOTIFICATION_MATERIALIZATION_FAILED"

var errCommentNotificationMaterializationConflict = errors.New("comment notification materialization cursor conflict")

type storedCommentNotificationEvent struct {
	ID                     string `db:"id"`
	WorkspaceID            string `db:"workspace_id"`
	SourceKind             string `db:"source_kind"`
	SourceID               string `db:"source_id"`
	ActorID                string `db:"actor_id"`
	MentionRecipientJSON   string `db:"mention_recipient_ids_json"`
	FixedRecipientJSON     string `db:"fixed_recipient_ids_json"`
	IncludePageAudience    bool   `db:"include_page_audience"`
	AudienceAt             int64  `db:"audience_at"`
	Status                 string `db:"status"`
	MaterializationPhase   string `db:"materialization_phase"`
	RecipientOffset        int    `db:"recipient_offset"`
	AudienceCursor         string `db:"audience_cursor"`
	AudienceCursorCreated  int64  `db:"audience_cursor_created"`
	MaterializationAttempt int    `db:"materialization_attempt"`
}

type commentNotificationCandidate struct {
	UserID string
	Kind   string
}

type commentNotificationMaterializationPatch struct {
	Phase                 string
	Offset                int
	AudienceCursor        string
	AudienceCursorCreated int64
	Status                string
	MaterializedAt        int64
}

func commentNotificationMaterializationDeliveryID(eventID, userID string) string {
	digest := sha256.Sum256([]byte(strings.Join(
		[]string{"comment-notification-delivery", eventID, userID}, "\x00",
	)))
	return hex.EncodeToString(digest[:16])
}

func materializeCommentNotificationEventSlice(
	ctx context.Context,
	app core.App,
	eventID string,
	now time.Time,
) (int, error) {
	current, err := loadCommentNotificationMaterializationEvent(ctx, app, eventID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != "pending") {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	switch current.MaterializationPhase {
	case "mentions":
		return materializeCommentNotificationExplicitSlice(
			ctx, app, current, current.MentionRecipientJSON, "mention", "fixed", now,
		)
	case "fixed":
		// The fixed recipients, then the comment's followers, whose cursor
		// is the audience cursor until the audience begins.
		values := []string{}
		if err := json.Unmarshal([]byte(current.FixedRecipientJSON), &values); err != nil {
			return 0, err
		}
		if current.RecipientOffset < len(values) {
			return materializeCommentNotificationExplicitSlice(
				ctx, app, current, current.FixedRecipientJSON, "generic", "fixed", now,
			)
		}
		// An event kept from the page's audience (a comment edit's newly
		// mentioned people) is kept from the followers too.
		if !current.IncludePageAudience {
			return materializeCommentNotificationAudienceSlice(ctx, app, current, now)
		}
		return materializeCommentNotificationFollowerSlice(ctx, app, current, now)
	case "audience":
		return materializeCommentNotificationAudienceSlice(ctx, app, current, now)
	case "complete":
		return 0, nil
	default:
		return 0, fmt.Errorf("invalid materialization phase %q", current.MaterializationPhase)
	}
}

func loadCommentNotificationMaterializationEvent(
	ctx context.Context,
	app core.App,
	eventID string,
) (storedCommentNotificationEvent, error) {
	row := storedCommentNotificationEvent{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[source_kind]], [[source_id]], [[actor_id]],
			[[mention_recipient_ids_json]], [[fixed_recipient_ids_json]],
			[[include_page_audience]], [[audience_at]], [[status]],
			[[materialization_phase]], [[recipient_offset]], [[audience_cursor]],
			[[audience_cursor_created]],
			[[materialization_attempt]]
		FROM {{leafen_notification_events}} WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": eventID}).One(&row)
	return row, err
}

func materializeCommentNotificationExplicitSlice(
	ctx context.Context,
	app core.App,
	event storedCommentNotificationEvent,
	payload string,
	kind string,
	nextPhase string,
	now time.Time,
) (int, error) {
	values := []string{}
	if err := json.Unmarshal([]byte(payload), &values); err != nil {
		return 0, err
	}
	if event.RecipientOffset < 0 || event.RecipientOffset > len(values) {
		return 0, errors.New("comment notification recipient offset is outside its payload")
	}
	end := event.RecipientOffset + commentNotificationMaterializationRecipientLimit
	if end > len(values) {
		end = len(values)
	}
	selected := values[event.RecipientOffset:end]
	candidates := make([]commentNotificationCandidate, 0, len(selected))
	for _, userID := range selected {
		candidates = append(candidates, commentNotificationCandidate{UserID: userID, Kind: kind})
	}
	patch := commentNotificationMaterializationPatch{
		Phase: event.MaterializationPhase, Offset: end,
		AudienceCursor:        event.AudienceCursor,
		AudienceCursorCreated: event.AudienceCursorCreated, Status: "pending",
	}
	// The fixed recipients' end is where their followers begin.
	if end == len(values) && nextPhase != event.MaterializationPhase {
		patch.Phase = nextPhase
		patch.Offset = 0
	}
	return commitCommentNotificationMaterializationSlice(
		ctx, app, event, candidates, patch, now,
	)
}

func materializeCommentNotificationFollowerSlice(
	ctx context.Context,
	app core.App,
	event storedCommentNotificationEvent,
	now time.Time,
) (int, error) {
	followers, more, err := commentNotificationFollowersAfter(
		ctx, app, event, event.AudienceCursor, commentNotificationMaterializationRecipientLimit,
	)
	if err != nil {
		return 0, err
	}
	candidates := make([]commentNotificationCandidate, 0, len(followers))
	for _, userID := range followers {
		candidates = append(candidates, commentNotificationCandidate{UserID: userID, Kind: "generic"})
	}
	patch := commentNotificationMaterializationPatch{Phase: "audience", Status: "pending"}
	if more {
		patch = commentNotificationMaterializationPatch{
			Phase: event.MaterializationPhase, Offset: event.RecipientOffset,
			AudienceCursor: followers[len(followers)-1], Status: "pending",
		}
	}
	return commitCommentNotificationMaterializationSlice(ctx, app, event, candidates, patch, now)
}

func materializeCommentNotificationAudienceSlice(
	ctx context.Context,
	app core.App,
	event storedCommentNotificationEvent,
	now time.Time,
) (int, error) {
	if !event.IncludePageAudience {
		return commitCommentNotificationMaterializationSlice(
			ctx, app, event, nil,
			commentNotificationMaterializationPatch{
				Phase: "complete", Status: "delivering", MaterializedAt: now.UnixMilli(),
			},
			now,
		)
	}
	rows := []struct {
		UserID  string `db:"user_id"`
		Created int64  `db:"created"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]], [[created]] FROM {{leafen_workspace_memberships}}
		INDEXED BY [[idx_leafen_workspace_memberships_notification_audience]]
		WHERE [[workspace_id]] = {:workspaceId} AND [[role]] = 'member'
			AND [[created]] <= {:audienceAt}
			AND ([[created]] > {:cursorCreated}
				OR ([[created]] = {:cursorCreated} AND [[user_id]] > {:cursor}))
		ORDER BY [[created]], [[user_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": event.WorkspaceID, "audienceAt": event.AudienceAt,
		"cursorCreated": event.AudienceCursorCreated, "cursor": event.AudienceCursor,
		"limit": commentNotificationMaterializationRecipientLimit + 1,
	}).All(&rows); err != nil {
		return 0, err
	}
	hasMore := len(rows) > commentNotificationMaterializationRecipientLimit
	if hasMore {
		rows = rows[:commentNotificationMaterializationRecipientLimit]
	}
	candidates := make([]commentNotificationCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, commentNotificationCandidate{
			UserID: row.UserID, Kind: "generic",
		})
	}
	patch := commentNotificationMaterializationPatch{
		Phase: "complete", Status: "delivering", MaterializedAt: now.UnixMilli(),
	}
	if hasMore {
		patch.Phase = "audience"
		patch.Status = "pending"
		patch.MaterializedAt = 0
		patch.AudienceCursor = rows[len(rows)-1].UserID
		patch.AudienceCursorCreated = rows[len(rows)-1].Created
	}
	return commitCommentNotificationMaterializationSlice(
		ctx, app, event, candidates, patch, now,
	)
}

func commitCommentNotificationMaterializationSlice(
	ctx context.Context,
	app core.App,
	event storedCommentNotificationEvent,
	candidates []commentNotificationCandidate,
	patch commentNotificationMaterializationPatch,
	now time.Time,
) (int, error) {
	unique := map[string]string{}
	for _, candidate := range candidates {
		userID := strings.TrimSpace(candidate.UserID)
		if userID == "" || userID == event.ActorID {
			continue
		}
		if candidate.Kind == "mention" || unique[userID] == "" {
			unique[userID] = candidate.Kind
		}
	}
	nowMillis := now.UTC().UnixMilli()
	for userID, kind := range unique {
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_notification_deliveries}} (
				[[id]], [[event_id]], [[workspace_id]], [[user_id]], [[kind]], [[status]],
				[[attempts]], [[next_attempt_at]], [[last_error_code]], [[delivered_at]],
				[[skipped_at]], [[created_at]], [[updated_at]]
			) VALUES (
				{:id}, {:eventId}, {:workspaceId}, {:userId}, {:kind}, 'pending',
				0, 0, '', 0, 0, {:now}, {:now}
			)
			ON CONFLICT ([[event_id]], [[user_id]]) DO UPDATE SET
				[[kind]] = CASE
					WHEN excluded.[[kind]] = 'mention' THEN 'mention'
					ELSE [[kind]]
				END,
				[[updated_at]] = excluded.[[updated_at]]
		`).WithContext(ctx).Bind(dbx.Params{
			"id":      commentNotificationMaterializationDeliveryID(event.ID, userID),
			"eventId": event.ID, "workspaceId": event.WorkspaceID,
			"userId": userID, "kind": kind, "now": nowMillis,
		}).Execute()
		if err != nil {
			return 0, err
		}
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_events}}
		SET [[materialization_phase]] = {:nextPhase},
			[[recipient_offset]] = {:nextOffset},
			[[audience_cursor]] = {:nextAudienceCursor},
			[[audience_cursor_created]] = {:nextAudienceCursorCreated},
			[[status]] = {:nextStatus}, [[materialized_at]] = {:materializedAt},
			[[materialization_next_attempt_at]] = 0,
			[[materialization_error_code]] = '', [[updated_at]] = {:updatedAt}
		WHERE [[id]] = {:id} AND [[status]] = 'pending'
			AND [[materialization_phase]] = {:currentPhase}
			AND [[recipient_offset]] = {:currentOffset}
			AND [[audience_cursor]] = {:currentAudienceCursor}
			AND [[audience_cursor_created]] = {:currentAudienceCursorCreated}
	`).WithContext(ctx).Bind(dbx.Params{
		"id": event.ID, "nextPhase": patch.Phase, "nextOffset": patch.Offset,
		"nextAudienceCursor": patch.AudienceCursor, "nextStatus": patch.Status,
		"nextAudienceCursorCreated":    patch.AudienceCursorCreated,
		"materializedAt":               patch.MaterializedAt,
		"updatedAt":                    now.UTC().Format(time.RFC3339Nano),
		"currentPhase":                 event.MaterializationPhase,
		"currentOffset":                event.RecipientOffset,
		"currentAudienceCursor":        event.AudienceCursor,
		"currentAudienceCursorCreated": event.AudienceCursorCreated,
	}).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if changed != 1 {
		return 0, errCommentNotificationMaterializationConflict
	}
	return len(unique), nil
}
