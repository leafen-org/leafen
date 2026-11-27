package app

import (
	"context"
	"database/sql"
	"errors"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone with an account here whom a workspace's owner adds joins at once,
// and hears of it by email when the server sends email, as Notion's added
// member does ("They'll receive an email with a link to sign in and join
// your workspace", Add members help): who added them to which workspace,
// the owner's note, and the server's address. Once per addition, within the
// limits invitations keep; an addition made again within a day of its email
// isn't mailed again, and one the person has left before it goes isn't sent.
// Each addition leaves a line in the person's inbox as well
// (workspace_member_added_notices.go).

type storedWorkspaceMemberAddedEmail struct {
	WorkspaceID string `db:"workspace_id"`
	UserID      string `db:"user_id"`
	ActorID     string `db:"actor_id"`
	Language    string `db:"language"`
	Message     string `db:"invite_message"`
	Attempts    int    `db:"mail_attempts"`
}

// queueWorkspaceMemberAddedEmail records that the actor added the person,
// with the actor's note, and queues its email when the server sends email.
func queueWorkspaceMemberAddedEmail(app core.App, workspaceID, actorID, memberID, message string, now time.Time) error {
	member, err := app.FindRecordById("users", memberID)
	if err != nil {
		return err
	}
	status := "none"
	if address := member.Email(); address != "" {
		if status, _, err = inviteEmailStatus(app, actorID, address, now); err != nil {
			return err
		}
	}
	language, err := serverTextLanguage(app, memberID, nil)
	if err != nil {
		return err
	}
	nowMillis := now.UnixMilli()
	// Added again within a day of its email, the person isn't mailed again.
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_membership_notifications}}
			([[workspace_id]], [[user_id]], [[kind]], [[created]], [[actor_id]], [[language]], [[invite_message]],
			 [[mail_status]], [[mail_attempts]], [[mail_next_at]], [[mail_error]])
		VALUES ({:workspace}, {:user}, 'member_added', {:now}, {:actor}, {:language}, {:message}, {:status}, 0, {:now}, '')
		ON CONFLICT ([[workspace_id]], [[user_id]], [[kind]]) DO UPDATE SET
			[[actor_id]] = excluded.[[actor_id]], [[language]] = excluded.[[language]],
			[[invite_message]] = excluded.[[invite_message]],
			[[mail_status]] = CASE WHEN [[mail_status]] = 'pending' OR ([[mail_status]] = 'sent' AND [[created]] > {:dayAgo})
				THEN [[mail_status]] ELSE excluded.[[mail_status]] END,
			[[mail_attempts]] = CASE WHEN [[mail_status]] = 'pending' OR ([[mail_status]] = 'sent' AND [[created]] > {:dayAgo})
				THEN [[mail_attempts]] ELSE 0 END,
			[[mail_next_at]] = CASE WHEN [[mail_status]] = 'pending' OR ([[mail_status]] = 'sent' AND [[created]] > {:dayAgo})
				THEN [[mail_next_at]] ELSE excluded.[[mail_next_at]] END,
			[[mail_error]] = CASE WHEN [[mail_status]] = 'pending' OR ([[mail_status]] = 'sent' AND [[created]] > {:dayAgo})
				THEN [[mail_error]] ELSE '' END,
			[[created]] = CASE WHEN [[mail_status]] = 'pending' OR ([[mail_status]] = 'sent' AND [[created]] > {:dayAgo})
				THEN [[created]] ELSE excluded.[[created]] END
	`).Bind(dbx.Params{
		"workspace": workspaceID, "user": memberID, "now": nowMillis, "actor": actorID, "language": language,
		"message": message, "status": status, "dayAgo": nowMillis - 24*time.Hour.Milliseconds(),
	}).Execute(); err != nil {
		return err
	}
	if status == "pending" {
		notifyNotificationEmailRunnerAfterCommit(app)
	}
	// Each addition is a line in the inbox too (workspace_member_added_notices.go).
	return notifyWorkspaceMemberAdded(app, workspaceID, actorID, memberID, now)
}

// workspaceMemberAddedEmailCounts counts the added emails an actor queued in
// the last hour and an address was sent in the last day, for the limits
// invitations share.
func workspaceMemberAddedEmailCounts(app core.App, actorID, recipient string, now time.Time) (int, int, error) {
	nowMillis := now.UnixMilli()
	var byActor, byAddress int
	err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_workspace_membership_notifications}}
				WHERE [[actor_id]] = {:actor} AND [[mail_status]] != 'none' AND [[created]] > {:hourAgo}),
			(SELECT COUNT(*) FROM {{leafen_workspace_membership_notifications}} AS [[n]]
				JOIN {{users}} AS [[u]] ON [[u]].[[id]] = [[n]].[[user_id]]
				WHERE lower([[u]].[[email]]) = {:recipient} AND [[n]].[[mail_status]] != 'none' AND [[n]].[[created]] > {:dayAgo})
	`).Bind(dbx.Params{
		"actor": actorID, "recipient": normalizePageShareEmail(recipient),
		"hourAgo": nowMillis - time.Hour.Milliseconds(), "dayAgo": nowMillis - 24*time.Hour.Milliseconds(),
	}).Row(&byActor, &byAddress)
	return byActor, byAddress, err
}

// nextWorkspaceMemberAddedEmailDue is when the next added email is due, or -1.
func nextWorkspaceMemberAddedEmailDue(ctx context.Context, app core.App) (int64, error) {
	var due int64 = -1
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[mail_next_at]]), -1) FROM {{leafen_workspace_membership_notifications}}
		WHERE [[mail_status]] = 'pending'
	`).WithContext(ctx).Row(&due)
	return due, err
}

// sendDueWorkspaceMemberAddedEmails sends the added emails that are due.
// Only an unsent one is retried, so an uncertain send never repeats.
func sendDueWorkspaceMemberAddedEmails(ctx context.Context, app core.App, now time.Time) error {
	rows := []storedWorkspaceMemberAddedEmail{}
	if err := app.DB().NewQuery(`
		SELECT [[workspace_id]], [[user_id]], [[actor_id]], [[language]], [[invite_message]], [[mail_attempts]]
		FROM {{leafen_workspace_membership_notifications}}
		WHERE [[mail_status]] = 'pending' AND [[mail_next_at]] <= {:now}
		ORDER BY [[mail_next_at]], [[workspace_id]], [[user_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": pageInviteEmailBatch}).All(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	settle := func(row storedWorkspaceMemberAddedEmail, status, code string, attempts int, next int64) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_workspace_membership_notifications}} SET [[mail_status]] = {:status}, [[mail_error]] = {:code},
				[[mail_attempts]] = {:attempts}, [[mail_next_at]] = {:next}
			WHERE [[workspace_id]] = {:workspace} AND [[user_id]] = {:user} AND [[kind]] = 'member_added'
		`).WithContext(ctx).Bind(dbx.Params{
			"status": status, "code": code, "attempts": attempts, "next": next, "workspace": row.WorkspaceID, "user": row.UserID,
		}).Execute()
		return err
	}
	retry := func(row storedWorkspaceMemberAddedEmail, code string) error {
		if row.Attempts+1 >= pageInviteEmailMaxAttempts {
			return settle(row, "failed", code, row.Attempts+1, 0)
		}
		return settle(row, "pending", code, row.Attempts+1, now.Add(time.Minute<<row.Attempts).UnixMilli())
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return err
	}
	configured := notificationEmailConfigured(settings)
	password := ""
	var passwordErr error
	if configured {
		password, passwordErr = decryptAdministratorSetting(app, settings.SMTPPasswordCipher)
	}
	turnErrors := []error{}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(turnErrors, err)...)
		}
		if !configured {
			turnErrors = append(turnErrors, settle(row, "skipped", "email_unavailable", row.Attempts, 0))
			continue
		}
		var memberships int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_memberships}}
			WHERE [[workspace_id]] = {:workspace} AND [[user_id]] = {:user}`).
			Bind(dbx.Params{"workspace": row.WorkspaceID, "user": row.UserID}).Row(&memberships); err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		if memberships == 0 {
			turnErrors = append(turnErrors, settle(row, "skipped", "membership_ended", row.Attempts, 0))
			continue
		}
		// Nor to an account being deleted, deleted or suspended
		// (sendNotificationEmailBatch).
		if active, err := accountMayAct(app, row.UserID); err != nil {
			turnErrors = append(turnErrors, err)
			continue
		} else if !active {
			turnErrors = append(turnErrors, settle(row, "skipped", "recipient_inactive", row.Attempts, 0))
			continue
		}
		if passwordErr != nil {
			turnErrors = append(turnErrors, retry(row, "smtp_settings_unreadable"))
			continue
		}
		recipient, message, err := buildWorkspaceMemberAddedEmail(app, settings, row)
		if errors.Is(err, sql.ErrNoRows) {
			// The person, the actor or the workspace is gone.
			turnErrors = append(turnErrors, settle(row, "skipped", "account_missing", row.Attempts, 0))
			continue
		} else if err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		if recipient == "" {
			turnErrors = append(turnErrors, settle(row, "skipped", "address_missing", row.Attempts, 0))
			continue
		}
		outcome, sendErr := sendConfiguredSMTP(settings, password, recipient, message)
		switch outcome {
		case smtpDeliveryAccepted:
			turnErrors = append(turnErrors, settle(row, "sent", "", row.Attempts+1, 0))
		case smtpDeliveryUncertain:
			logNotificationEmailFailure(app, "workspace-added-delivery-uncertain", sendErr)
			turnErrors = append(turnErrors, settle(row, "failed", "delivery_uncertain", row.Attempts+1, 0))
		default:
			logNotificationEmailFailure(app, "workspace-added-delivery-failed", sendErr)
			turnErrors = append(turnErrors, retry(row, "delivery_failed"))
		}
	}
	return errors.Join(turnErrors...)
}

// buildWorkspaceMemberAddedEmail writes the email: who added the person to
// which workspace, the note, and the server's address when it has one.
func buildWorkspaceMemberAddedEmail(app core.App, settings storedAdministratorSettings,
	row storedWorkspaceMemberAddedEmail) (string, []byte, error) {
	member, err := app.FindRecordById("users", row.UserID)
	if err != nil {
		return "", nil, err
	}
	recipient := member.Email()
	if recipient == "" {
		return "", nil, nil
	}
	workspace, err := app.FindRecordById(workspacesCollection, row.WorkspaceID)
	if err != nil {
		return "", nil, err
	}
	actor, err := app.FindRecordById("users", row.ActorID)
	if err != nil {
		return "", nil, err
	}
	name := notificationEmailLine(accountLabel(actor), notificationEmailTitleRunes)
	workspaceName := quotedEmailWorkspaceName(row.Language, notificationEmailLine(workspace.GetString("name"), notificationEmailTitleRunes))
	link := othersLinkOrigin()
	note := strings.ReplaceAll(strings.ReplaceAll(row.Message, "\r\n", "\n"), "\r", "\n")
	if note != "" {
		note = strings.ReplaceAll(note, "\n", "\r\n") + "\r\n\r\n"
	}
	var subject string
	var body strings.Builder
	if row.Language == accountLanguageKo {
		subject = name + "님이 " + workspaceName + "에 회원님을 추가했습니다"
		body.WriteString(nameWithEmail(name, actor.Email(), "") + "님이 리펜의 " + workspaceName +
			"에 회원님을 멤버로 추가했습니다.\r\n\r\n")
		body.WriteString(note)
		body.WriteString("리펜을 열면 워크스페이스 목록에 있습니다.\r\n")
		if link != "" {
			body.WriteString(link + "\r\n")
		}
		body.WriteString("\r\n--\r\n이 메일은 회원님이 워크스페이스에 추가되어 보냈습니다.\r\n")
	} else {
		subject = name + " added you to " + workspaceName
		body.WriteString(nameWithEmail(name, actor.Email(), " ") + " added you to " + workspaceName +
			" on Leafen as a member.\r\n\r\n")
		body.WriteString(note)
		body.WriteString("Open Leafen to find it in your workspaces.\r\n")
		if link != "" {
			body.WriteString(link + "\r\n")
		}
		body.WriteString("\r\n--\r\nYou’re getting this email because you were added to a workspace.\r\n")
	}
	from := (&mail.Address{Name: settings.SenderName, Address: settings.SenderAddress}).String()
	headers := []string{
		"From: " + from,
		"To: " + (&mail.Address{Address: recipient}).String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Auto-Submitted: auto-generated",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return recipient, []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String()), nil
}
