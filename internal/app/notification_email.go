package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notificationEmailDelay        = 5 * time.Minute
	notificationEmailLease        = 2 * time.Minute
	notificationEmailBatchLimit   = 20
	notificationEmailMaxAttempts  = 5
	notificationEmailRetention    = 30 * 24 * time.Hour
	notificationEmailCleanupLimit = 100
)

type storedNotificationEmail struct {
	NotificationID string `db:"notification_id"`
	WorkspaceID    string `db:"workspace_id"`
	UserID         string `db:"user_id"`
	Attempts       int    `db:"attempts"`
}

type storedNotificationEmailItem struct {
	ID          string `db:"id"`
	Kind        string `db:"kind"`
	ActivityKey string `db:"activity_key"`
	PageID      string `db:"page_id"`
	ActorID     string `db:"actor_id"`
	// BlockID and CommentID name what it came from, which its reader reads
	// it by (notificationEmailItemsAsRead).
	BlockID   string `db:"block_id"`
	CommentID string `db:"comment_id"`
	Title     string `db:"title"`
	Preview   string `db:"preview"`
	// PreviewSpans is the preview with its mentions, when the line has them.
	PreviewSpans string `db:"preview_spans"`
	Target       string `db:"target"`
	OccurredAt   int64  `db:"occurred_at"`
	ReadAt       int64  `db:"read_at"`
	ArchivedAt   int64  `db:"archived_at"`
	// ActorConnection names the connection that wrote it, when one did
	// (noticeConnection).
	ActorConnection string `db:"actor_connection"`
	// NameKind is its page's kind, which names it while untitled
	// (notificationEmailItemsAsRead).
	NameKind pageNameKind `db:"-"`
}

// enqueueNotificationEmails gives each new personal notification the email
// it becomes if its recipient has not seen it by the time it is due. Someone
// who asked for every email gets it at once.
func enqueueNotificationEmails(
	app core.App,
	projections []productNotificationProjection,
	now int64,
) error {
	settingsByUser := map[string]storedNotificationSettings{}
	queued := false
	for _, projection := range projections {
		if !projection.Personal {
			continue
		}
		// Someone opening a shared page is told in the inbox alone: Notion's
		// activity emails name comments, mentions, page invitations, reminders,
		// access requests and database property changes (seen 2026-11-05).
		if strings.HasPrefix(projection.ActivityKey, "page-share-access:") {
			continue
		}
		// Being added to a workspace has its own email (workspace_member_added_emails.go);
		// a page's permission line none, a share given emailing its invitation.
		if strings.HasPrefix(projection.ActivityKey, workspaceMemberAddedActivityPrefix) ||
			pagePermissionNotice(projection.ActivityKey) {
			continue
		}
		settings, known := settingsByUser[projection.UserID]
		if !known {
			var err error
			if settings, err = loadNotificationSettings(app, projection.UserID); err != nil {
				return err
			}
			settingsByUser[projection.UserID] = settings
		}
		// A deadline someone must not miss is emailed at once, whatever
		// the person chose for activity emails (deadline_notices.go).
		deadline := deadlineNoticeOfActivity(projection.ActivityKey) != ""
		if !settings.EmailActivity && !deadline {
			continue
		}
		due := now + notificationEmailDelay.Milliseconds()
		if settings.EmailAlways || deadline {
			due = now
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_notification_emails}} (
				[[notification_id]], [[workspace_id]], [[user_id]], [[status]], [[due_at]],
				[[attempts]], [[next_attempt_at]], [[created_at]], [[updated_at]]
			) VALUES ({:id}, {:workspaceId}, {:userId}, 'pending', {:due}, 0, {:due}, {:now}, {:now})
			ON CONFLICT ([[notification_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": projection.ID, "workspaceId": projection.WorkspaceID, "userId": projection.UserID,
			"due": due, "now": now,
		}).Execute(); err != nil {
			return err
		}
		queued = true
	}
	if queued {
		notifyNotificationEmailRunnerAfterCommit(app)
	}
	return nil
}

func skipPendingNotificationEmails(app core.App, userID, reason string, now time.Time) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_emails}}
		SET [[status]] = 'skipped', [[error_code]] = {:reason}, [[lease_token]] = '',
			[[lease_until]] = 0, [[settled_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[user_id]] = {:userId} AND [[status]] = 'pending'
	`).Bind(dbx.Params{"userId": userID, "reason": reason, "now": now.UnixMilli()}).Execute()
	return err
}

func releaseExpiredNotificationEmailLeases(ctx context.Context, app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_emails}}
		SET [[status]] = 'pending', [[lease_token]] = '', [[lease_until]] = 0, [[updated_at]] = {:now}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_notification_emails}}
			INDEXED BY [[idx_leafen_notification_emails_claimed]]
			WHERE [[status]] = 'claimed' AND [[lease_until]] <= {:now}
			ORDER BY [[lease_until]], [[notification_id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now.UnixMilli(), "limit": notificationEmailCleanupLimit,
	}).Execute()
	return err
}

// dueNotificationEmailRecipients lists the people whose emails are due,
// earliest first.
func dueNotificationEmailRecipients(ctx context.Context, app core.App, now time.Time, limit int) ([]string, error) {
	userIDs := []string{}
	err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_notification_emails}}
		INDEXED BY [[idx_leafen_notification_emails_due]]
		WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now}
		GROUP BY [[user_id]] ORDER BY MIN([[next_attempt_at]]), [[user_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": limit}).Column(&userIDs)
	return userIDs, err
}

func nextNotificationEmailDue(ctx context.Context, app core.App) (int64, error) {
	var due int64 = -1
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[next_attempt_at]]), -1) FROM {{leafen_notification_emails}}
		INDEXED BY [[idx_leafen_notification_emails_due]]
		WHERE [[status]] = 'pending'
	`).WithContext(ctx).Row(&due)
	return due, err
}

// claimNotificationEmails leases one person's due emails so a single batch
// sends them; a lease that lapses returns them to the queue.
func claimNotificationEmails(
	ctx context.Context,
	app core.App,
	userID string,
	now time.Time,
) (string, []storedNotificationEmail, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, err
	}
	lease := hex.EncodeToString(tokenBytes)
	rows := []storedNotificationEmail{}
	err := app.RunInTransaction(func(txApp core.App) error {
		if err := txApp.DB().NewQuery(`
			SELECT [[notification_id]], [[workspace_id]], [[user_id]], [[attempts]]
			FROM {{leafen_notification_emails}}
			INDEXED BY [[idx_leafen_notification_emails_due]]
			WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now} AND [[user_id]] = {:userId}
			ORDER BY [[next_attempt_at]], [[notification_id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now.UnixMilli(), "userId": userID, "limit": notificationEmailBatchLimit,
		}).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_notification_emails}}
				SET [[status]] = 'claimed', [[lease_token]] = {:lease}, [[lease_until]] = {:until},
					[[updated_at]] = {:now}
				WHERE [[notification_id]] = {:id} AND [[status]] = 'pending'
			`).WithContext(ctx).Bind(dbx.Params{
				"id": row.NotificationID, "lease": lease,
				"until": now.Add(notificationEmailLease).UnixMilli(), "now": now.UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	return lease, rows, err
}

// settleNotificationEmails ends the claimed emails of one lease as sent,
// skipped or failed, or returns them for a later attempt.
func settleNotificationEmails(
	ctx context.Context,
	app core.App,
	lease string,
	rows []storedNotificationEmail,
	status string,
	code string,
	now time.Time,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		for _, row := range rows {
			attempts := row.Attempts
			settledAt := now.UnixMilli()
			nextStatus := status
			nextAttemptAt := int64(0)
			if status == "retry" {
				attempts++
				nextStatus = "pending"
				settledAt = 0
				nextAttemptAt = now.Add(notificationEmailRetryDelay(attempts)).UnixMilli()
				if attempts >= notificationEmailMaxAttempts {
					nextStatus = "failed"
					settledAt = now.UnixMilli()
					nextAttemptAt = 0
				}
			} else if status == "sent" || status == "failed" {
				attempts++
			}
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_notification_emails}}
				SET [[status]] = {:status}, [[attempts]] = {:attempts},
					[[next_attempt_at]] = CASE WHEN {:status} = 'pending'
						THEN MAX([[due_at]], {:nextAttemptAt}) ELSE [[next_attempt_at]] END,
					[[lease_token]] = '', [[lease_until]] = 0, [[error_code]] = {:errorCode},
					[[settled_at]] = {:settledAt}, [[updated_at]] = {:now}
				WHERE [[notification_id]] = {:id} AND [[status]] = 'claimed' AND [[lease_token]] = {:lease}
			`).WithContext(ctx).Bind(dbx.Params{
				"id": row.NotificationID, "lease": lease, "status": nextStatus, "attempts": attempts,
				"nextAttemptAt": nextAttemptAt, "errorCode": code, "settledAt": settledAt,
				"now": now.UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}

func notificationEmailRetryDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return time.Minute
	case attempt == 2:
		return 5 * time.Minute
	case attempt == 3:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func pruneSettledNotificationEmails(ctx context.Context, app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notification_emails}}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_notification_emails}}
			INDEXED BY [[idx_leafen_notification_emails_settled]]
			WHERE [[settled_at]] > 0 AND [[settled_at]] < {:before}
			ORDER BY [[settled_at]], [[notification_id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"before": now.Add(-notificationEmailRetention).UnixMilli(), "limit": notificationEmailCleanupLimit,
	}).Execute()
	return err
}

// sendNotificationEmailBatch sends one person's claimed emails as a single
// message. A notification already read or archived, one whose page the person
// can no longer open, and everything after activity emails were turned off
// is skipped — but a deadline's notice is skipped only once it is
// acknowledged; only an unsent message is retried, so an uncertain send never
// repeats.
func sendNotificationEmailBatch(
	ctx context.Context,
	app core.App,
	lease string,
	rows []storedNotificationEmail,
	now time.Time,
) error {
	if len(rows) == 0 {
		return nil
	}
	userID := rows[0].UserID
	skip := func(code string) error {
		return settleNotificationEmails(ctx, app, lease, rows, "skipped", code, now)
	}
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return skip("recipient_missing")
	}
	recipient := strings.TrimSpace(user.Email())
	if recipient == "" {
		return skip("recipient_missing")
	}
	if active, err := accountMayAct(app, userID); err != nil {
		return err
	} else if !active {
		return skip("recipient_inactive")
	}
	settings, err := loadNotificationSettings(app, userID)
	if err != nil {
		return err
	}
	items, err := loadNotificationEmailItems(ctx, app, rows)
	if err != nil {
		return err
	}
	sending := []storedNotificationEmail{}
	skipped := []storedNotificationEmail{}
	off := []storedNotificationEmail{}
	messageItems := []storedNotificationEmailItem{}
	for _, row := range rows {
		item, found := items[row.NotificationID]
		notice := deadlineNoticeOfActivity(item.ActivityKey)
		if !settings.EmailActivity && notice == "" {
			off = append(off, row)
			continue
		}
		seen := found && !settings.EmailAlways && (item.ReadAt != 0 || item.ArchivedAt != 0)
		if found && notice != "" {
			var acknowledged int
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_deadline_acknowledgements}} WHERE [[notice_id]] = {:id} AND [[user_id]] = {:userId}`).
				Bind(dbx.Params{"id": notice, "userId": userID}).Row(&acknowledged); err != nil {
				return err
			}
			seen = acknowledged > 0
		}
		if found && !seen {
			if authErr := authorizeNotificationSubject(app, userID, item.PageID, item.Target); authErr != nil {
				if !isPermissionDenied(authErr) {
					return authErr
				}
				found = false
			}
		}
		if !found || seen {
			skipped = append(skipped, row)
			continue
		}
		sending = append(sending, row)
		messageItems = append(messageItems, item)
	}
	if len(off) > 0 {
		if err := settleNotificationEmails(ctx, app, lease, off, "skipped", "email_activity_off", now); err != nil {
			return err
		}
	}
	if len(skipped) > 0 {
		if err := settleNotificationEmails(ctx, app, lease, skipped, "skipped", "seen_or_unavailable", now); err != nil {
			return err
		}
	}
	if len(sending) == 0 {
		return nil
	}
	administrator, err := loadAdministratorSettings(app)
	if err != nil {
		return err
	}
	if !notificationEmailConfigured(administrator) {
		return settleNotificationEmails(ctx, app, lease, sending, "skipped", "email_unavailable", now)
	}
	password, err := decryptAdministratorSetting(app, administrator.SMTPPasswordCipher)
	if err != nil {
		return settleNotificationEmails(ctx, app, lease, sending, "retry", "smtp_settings_unreadable", now)
	}
	language, err := serverTextLanguage(app, userID, nil)
	if err != nil {
		return err
	}
	actors, err := loadNotificationEmailActors(app, userID, language, messageItems)
	if err != nil {
		return err
	}
	location := accountLocation(app, userID)
	for index := range messageItems {
		item := &messageItems[index]
		item.Preview = notificationLineAsItStands(item.Preview, item.PreviewSpans, language, location, now)
	}
	unsubscribe := ""
	if origin := othersLinkOrigin(); origin != "" {
		token, err := notificationEmailUnsubscribeToken(app, userID)
		if err != nil {
			return err
		}
		unsubscribe = origin + apiRoot + "/notification-email/unsubscribe?token=" + token
	}
	message := buildNotificationEmail(administrator, recipient, language, messageItems, actors,
		othersLinkOrigin(), unsubscribe)
	outcome, sendErr := sendConfiguredSMTP(administrator, password, recipient, message)
	switch outcome {
	case smtpDeliveryAccepted:
		return settleNotificationEmails(ctx, app, lease, sending, "sent", "", now)
	case smtpDeliveryUncertain:
		logNotificationEmailFailure(app, "delivery-uncertain", sendErr)
		return settleNotificationEmails(ctx, app, lease, sending, "failed", "delivery_uncertain", now)
	default:
		logNotificationEmailFailure(app, "delivery-failed", sendErr)
		return settleNotificationEmails(ctx, app, lease, sending, "retry", "delivery_failed", now)
	}
}

func loadNotificationEmailItems(
	ctx context.Context,
	app core.App,
	rows []storedNotificationEmail,
) (map[string]storedNotificationEmailItem, error) {
	params := dbx.Params{"userId": rows[0].UserID}
	placeholders := make([]string, 0, len(rows))
	for index, row := range rows {
		name := "id" + strconv.Itoa(index)
		params[name] = row.NotificationID
		placeholders = append(placeholders, "{:"+name+"}")
	}
	items := []storedNotificationEmailItem{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[kind]], [[activity_key]], [[page_id]], [[block_id]], [[comment_id]], [[actor_id]], [[title]], [[preview]],
			[[preview_spans]], [[target]], [[occurred_at]], [[read_at]], [[archived_at]], [[actor_connection]]
		FROM {{leafen_notifications}}
		WHERE [[user_id]] = {:userId} AND [[id]] IN (` + strings.Join(placeholders, ",") + `)
			AND ` + notificationShownSQL("") + `
	`).WithContext(ctx).Bind(params).All(&items); err != nil {
		return nil, err
	}
	notificationEmailItemsAsRead(app, rows[0].UserID, items)
	byID := make(map[string]storedNotificationEmailItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	return byID, nil
}

// loadNotificationEmailActors names the notices' actors as their reader reads
// them, in their language: a connection's comment's notice by the connection
// (notificationEmailItemActor).
func loadNotificationEmailActors(app core.App, readerID, language string, items []storedNotificationEmailItem) (map[string]string, error) {
	names := map[string]string{}
	commentIDs := make([]string, 0, len(items))
	for _, item := range items {
		if notificationFromComment(item.Kind, item.CommentID) {
			commentIDs = append(commentIDs, item.CommentID)
		}
	}
	for commentID, author := range notificationConnectionAuthors(app, commentIDs) {
		names[notificationConnectionActorKey(commentID)] = notificationConnectionAuthorWords(language, author)
	}
	for _, item := range items {
		if item.ActorID == "" {
			continue
		}
		if _, known := names[item.ActorID]; known {
			continue
		}
		actor, err := app.FindRecordById("users", item.ActorID)
		if err != nil {
			names[item.ActorID] = ""
			continue
		}
		names[item.ActorID] = accountLabel(actor)
	}
	return names, nil
}

func notificationEmailConfigured(settings storedAdministratorSettings) bool {
	return settings.SMTPEnabled && settings.SMTPHost != "" && settings.SenderAddress != ""
}

func logNotificationEmailFailure(app core.App, stage string, cause error) {
	logDurableJobDiagnostic(
		app, errorCodeEmailDeliveryFailed, newDiagnosticReference(), "",
		"notification.email.send", stage, cause,
	)
}
