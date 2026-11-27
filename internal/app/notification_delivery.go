package app

import (
	"context"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type productNotificationProjection struct {
	ID          string
	EventID     string
	WorkspaceID string
	UserID      string
	ActivityKey string
	Kind        string
	PageID      string
	BlockID     string
	CommentID   string
	ActorID     string
	Title       string
	Preview     string
	// PreviewSpans is the preview with its mentions, as rich text JSON, when it has any.
	PreviewSpans string
	Target       string
	OccurredAt   int64
	// Personal marks a notification meant for this person — a mention, a
	// reply in their thread or a comment on a page they follow — rather than
	// workspace activity. Only personal notifications may become email.
	Personal bool
	// ActorConnection names the connection whose write it is, when an
	// integration's (noticeConnection); the actor stays its person.
	ActorConnection string
}

type storedProductNotificationIdentity struct {
	WorkspaceID string `db:"workspace_id"`
	UserID      string `db:"user_id"`
	ActivityKey string `db:"activity_key"`
}

func productNotificationIdentityKey(workspaceID, userID, activityKey string) string {
	return workspaceID + "\x00" + userID + "\x00" + activityKey
}

func upsertProductNotifications(
	ctx context.Context,
	app core.App,
	projections []productNotificationProjection,
	now int64,
) (int, error) {
	if len(projections) == 0 {
		return 0, nil
	}
	// An app's account gets no notifications, so no emails or pushes.
	recipients := make([]string, 0, len(projections))
	for _, projection := range projections {
		recipients = append(recipients, projection.UserID)
	}
	integrations, err := integrationAccountUsers(app, recipients)
	if err != nil {
		return 0, err
	}
	if len(integrations) > 0 {
		kept := make([]productNotificationProjection, 0, len(projections))
		for _, projection := range projections {
			if !integrations[projection.UserID] {
				kept = append(kept, projection)
			}
		}
		if projections = kept; len(projections) == 0 {
			return 0, nil
		}
	}
	normalized := make([]productNotificationProjection, 0, len(projections))
	positions := make(map[string]int, len(projections))
	for _, projection := range projections {
		key := productNotificationIdentityKey(
			projection.WorkspaceID, projection.UserID, projection.ActivityKey,
		)
		if position, ok := positions[key]; ok {
			if projection.OccurredAt > normalized[position].OccurredAt {
				normalized[position] = projection
			}
			continue
		}
		positions[key] = len(normalized)
		normalized = append(normalized, projection)
	}
	params := dbx.Params{"now": now}
	existingValues := make([]string, 0, len(normalized))
	notificationValues := make([]string, 0, len(normalized))
	for index, projection := range normalized {
		prefix := fmt.Sprintf("notification%d", index)
		params[prefix+"Id"] = projection.ID
		params[prefix+"EventId"] = projection.EventID
		params[prefix+"WorkspaceId"] = projection.WorkspaceID
		params[prefix+"UserId"] = projection.UserID
		params[prefix+"ActivityKey"] = projection.ActivityKey
		params[prefix+"Kind"] = projection.Kind
		params[prefix+"PageId"] = projection.PageID
		params[prefix+"BlockId"] = projection.BlockID
		params[prefix+"CommentId"] = projection.CommentID
		params[prefix+"ActorId"] = projection.ActorID
		params[prefix+"Title"] = projection.Title
		params[prefix+"Preview"] = projection.Preview
		params[prefix+"PreviewSpans"] = projection.PreviewSpans
		params[prefix+"Target"] = projection.Target
		params[prefix+"OccurredAt"] = projection.OccurredAt
		// Notices such as wiki verification are for the person too, though
		// they bring their own email.
		params[prefix+"Personal"] = projection.Personal || projection.Kind == "system"
		params[prefix+"ActorConnection"] = projection.ActorConnection
		existingValues = append(existingValues, fmt.Sprintf(
			"({:%sWorkspaceId},{:%sUserId},{:%sActivityKey})", prefix, prefix, prefix,
		))
		notificationValues = append(notificationValues, fmt.Sprintf(
			"({:%sId},{:%sEventId},{:%sWorkspaceId},{:%sUserId},{:%sActivityKey},"+
				"{:%sKind},{:%sPageId},{:%sBlockId},{:%sCommentId},{:%sActorId},"+
				"{:%sTitle},{:%sPreview},{:%sTarget},{:%sOccurredAt},0,0,{:now},{:now},{:%sPersonal},{:%sPreviewSpans},"+
				"{:%sActorConnection})",
			prefix, prefix, prefix, prefix, prefix, prefix, prefix,
			prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix,
		))
	}
	existing := []storedProductNotificationIdentity{}
	if err := app.DB().NewQuery(`
		WITH [[wanted]] ([[workspace_id]],[[user_id]],[[activity_key]]) AS (
			VALUES ` + joinSQL(existingValues) + `
		)
		SELECT [[notification]].[[workspace_id]],[[notification]].[[user_id]],
			[[notification]].[[activity_key]]
		FROM {{leafen_notifications}} [[notification]]
		JOIN [[wanted]] ON [[wanted]].[[workspace_id]]=[[notification]].[[workspace_id]]
			AND [[wanted]].[[user_id]]=[[notification]].[[user_id]]
			AND [[wanted]].[[activity_key]]=[[notification]].[[activity_key]]
	`).WithContext(ctx).Bind(params).All(&existing); err != nil {
		return 0, err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notifications}} (
			[[id]], [[event_id]], [[workspace_id]], [[user_id]], [[activity_key]],
			[[kind]], [[page_id]], [[block_id]], [[comment_id]], [[actor_id]],
			[[title]], [[preview]], [[target]], [[occurred_at]], [[read_at]],
			[[archived_at]], [[created_at]], [[updated_at]], [[personal]], [[preview_spans]], [[actor_connection]]
		) VALUES ` + joinSQL(notificationValues) + `
		ON CONFLICT ([[workspace_id]], [[user_id]], [[activity_key]]) DO UPDATE
		SET [[event_id]]=excluded.[[event_id]], [[kind]]=excluded.[[kind]],
			[[page_id]]=excluded.[[page_id]], [[block_id]]=excluded.[[block_id]],
			[[comment_id]]=excluded.[[comment_id]], [[actor_id]]=excluded.[[actor_id]],
			[[actor_connection]]=excluded.[[actor_connection]],
			[[title]]=excluded.[[title]], [[preview]]=excluded.[[preview]], [[preview_spans]]=excluded.[[preview_spans]],
			[[target]]=excluded.[[target]], [[occurred_at]]=excluded.[[occurred_at]],
			[[updated_at]]=excluded.[[updated_at]], [[personal]]=max([[personal]], excluded.[[personal]])
		WHERE excluded.[[occurred_at]] >= [[occurred_at]]
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	// An open screen waiting on its workspace hears of it (workspace_page_list_wait.go).
	noticed := make(map[[2]string]struct{}, len(normalized))
	for _, projection := range normalized {
		noticed[[2]string{projection.WorkspaceID, projection.UserID}] = struct{}{}
	}
	noteWorkspaceNotices(noticed)
	known := make(map[string]struct{}, len(existing))
	for _, identity := range existing {
		known[productNotificationIdentityKey(identity.WorkspaceID, identity.UserID, identity.ActivityKey)] = struct{}{}
	}
	fresh := make([]productNotificationProjection, 0, len(normalized))
	for _, projection := range normalized {
		key := productNotificationIdentityKey(projection.WorkspaceID, projection.UserID, projection.ActivityKey)
		if _, seen := known[key]; !seen {
			fresh = append(fresh, projection)
		}
	}
	if err := enqueueNotificationEmails(app, fresh, now); err != nil {
		return 0, err
	}
	if err := enqueueWebPushDeliveries(app, fresh, now); err != nil {
		return 0, err
	}
	if err := enqueueSlackNotificationDeliveries(app, fresh, now); err != nil {
		return 0, err
	}
	return len(normalized) - len(existing), nil
}

func upsertProductNotification(
	ctx context.Context,
	app core.App,
	projection productNotificationProjection,
	now int64,
) (bool, error) {
	inserted, err := upsertProductNotifications(
		ctx, app, []productNotificationProjection{projection}, now,
	)
	return inserted == 1, err
}
