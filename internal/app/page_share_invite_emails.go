package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"mime"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An address a page is shared with that no account has verified is invited
// by email when the server can send email, as Notion emails someone
// invited: once for the share, retried a few times, and within limits per
// inviter and per address so the server can't be used to mail anyone at
// will — limits workspace invitations share. One the limits hold back is
// kept as skipped with its reason, so the share window says it didn't go.
// A share taken back, or an address an account verifies meanwhile, is no
// longer mailed.

const (
	pageInviteEmailsPerInviterHour = 20
	pageInviteEmailsPerAddressDay  = 3
	pageInviteEmailMaxAttempts     = 5
	pageInviteEmailBatch           = 16
)

type storedPageInviteEmail struct {
	ID             string `db:"id"`
	ShareID        string `db:"share_id"`
	WorkspaceID    string `db:"workspace_id"`
	PageID         string `db:"page_id"`
	InviterID      string `db:"inviter_id"`
	RecipientEmail string `db:"recipient_email"`
	Language       string `db:"language"`
	Attempts       int    `db:"attempts"`
}

// queuePageInviteEmail queues the invitation email for an address no
// account has verified, or keeps the one the limits hold back as skipped.
func queuePageInviteEmail(ctx context.Context, app core.App, workspaceID, pageID, inviterID, email, shareID string, now time.Time) error {
	recipient := normalizePageShareEmail(email)
	nowMillis := now.UnixMilli()
	status, reason, err := inviteEmailStatus(app, inviterID, recipient, now)
	if err != nil || status == "none" && reason == "" {
		return err
	}
	due := nowMillis
	if status == "none" {
		status, due = "skipped", 0
	}
	language, err := serverTextLanguageIn(ctx, app, inviterID)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte("page-invite-email\x00" + shareID))
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_invite_emails}}
			([[id]], [[share_id]], [[workspace_id]], [[page_id]], [[inviter_id]], [[recipient_email]], [[language]],
			 [[status]], [[attempts]], [[next_attempt_at]], [[error_code]], [[created_at]], [[updated_at]])
		VALUES ({:id}, {:shareId}, {:workspaceId}, {:pageId}, {:inviter}, {:recipient}, {:language},
			{:status}, 0, {:due}, {:reason}, {:now}, {:now})
		ON CONFLICT ([[share_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"id": hex.EncodeToString(digest[:16]), "shareId": shareID, "workspaceId": workspaceID, "pageId": pageID,
		"inviter": inviterID, "recipient": recipient, "language": language, "status": status, "reason": reason,
		"due": due, "now": nowMillis,
	}).Execute(); err != nil {
		return err
	}
	if status == "pending" {
		notifyNotificationEmailRunnerAfterCommit(app)
	}
	return nil
}

// nextPageInviteEmailDue is when the next queued invitation is due, or -1.
func nextPageInviteEmailDue(ctx context.Context, app core.App) (int64, error) {
	var due int64 = -1
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[next_attempt_at]]), -1) FROM {{leafen_page_invite_emails}} WHERE [[status]] = 'pending'
	`).WithContext(ctx).Row(&due)
	return due, err
}

// pruneSettledPageInviteEmails forgets invitations settled long enough ago
// that no limit counts them.
func pruneSettledPageInviteEmails(ctx context.Context, app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_invite_emails}}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_page_invite_emails}}
			WHERE [[status]] != 'pending' AND [[updated_at]] < {:before}
			ORDER BY [[updated_at]], [[id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"before": now.Add(-notificationEmailRetention).UnixMilli(), "limit": notificationEmailCleanupLimit,
	}).Execute()
	return err
}

// sendDuePageInviteEmails sends the invitations that are due. Only an unsent
// invitation is retried, so an uncertain send never repeats.
func sendDuePageInviteEmails(ctx context.Context, app core.App, now time.Time) error {
	if err := pruneSettledPageInviteEmails(ctx, app, now); err != nil {
		return err
	}
	rows := []storedPageInviteEmail{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[share_id]], [[workspace_id]], [[page_id]], [[inviter_id]], [[recipient_email]],
			[[language]], [[attempts]]
		FROM {{leafen_page_invite_emails}}
		WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": pageInviteEmailBatch}).All(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	settle := func(row storedPageInviteEmail, status, code string, attempts int, next int64) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_invite_emails}} SET [[status]] = {:status}, [[error_code]] = {:code},
				[[attempts]] = {:attempts}, [[next_attempt_at]] = {:next}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": status, "code": code, "attempts": attempts, "next": next, "now": now.UnixMilli(), "id": row.ID,
		}).Execute()
		return err
	}
	retry := func(row storedPageInviteEmail, code string) error {
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
		skip, err := pageInviteEmailSkip(app, row)
		if err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		if skip != "" {
			turnErrors = append(turnErrors, settle(row, "skipped", skip, row.Attempts, 0))
			continue
		}
		if passwordErr != nil {
			turnErrors = append(turnErrors, retry(row, "smtp_settings_unreadable"))
			continue
		}
		message, err := buildPageInviteEmail(app, settings, row)
		if err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		outcome, sendErr := sendConfiguredSMTP(settings, password, row.RecipientEmail, message)
		switch outcome {
		case smtpDeliveryAccepted:
			turnErrors = append(turnErrors, settle(row, "sent", "", row.Attempts+1, 0))
		case smtpDeliveryUncertain:
			logNotificationEmailFailure(app, "invite-delivery-uncertain", sendErr)
			turnErrors = append(turnErrors, settle(row, "failed", "delivery_uncertain", row.Attempts+1, 0))
		default:
			logNotificationEmailFailure(app, "invite-delivery-failed", sendErr)
			turnErrors = append(turnErrors, retry(row, "delivery_failed"))
		}
	}
	return errors.Join(turnErrors...)
}

// pageInviteEmailSkip names why an invitation no longer goes out, or "".
func pageInviteEmailSkip(app core.App, row storedPageInviteEmail) (string, error) {
	var shares int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_page_permissions}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": row.ShareID}).Row(&shares); err != nil {
		return "", err
	}
	if shares == 0 {
		return "share_removed", nil
	}
	account, err := pageShareEmailAccount(app, row.RecipientEmail)
	if err != nil {
		return "", err
	}
	if account != "" {
		return "account_exists", nil
	}
	return "", nil
}

// buildPageInviteEmail writes the invitation: who invited the address to
// which page of which workspace, and the page's address when the server has
// one, where signing up with the address opens it.
func buildPageInviteEmail(app core.App, settings storedAdministratorSettings, row storedPageInviteEmail) ([]byte, error) {
	page, err := app.FindRecordById(pagesCollection, row.PageID)
	if err != nil {
		return nil, err
	}
	workspace, err := app.FindRecordById(workspacesCollection, row.WorkspaceID)
	if err != nil {
		return nil, err
	}
	inviter, err := app.FindRecordById("users", row.InviterID)
	if err != nil {
		return nil, err
	}
	// An untitled page is called by its kind, a full-page database "새
	// 데이터베이스", as the app and the notice emails call it.
	plainTitle := notificationEmailLine(page.GetString("title"), notificationEmailTitleRunes)
	if plainTitle == "" {
		kind, err := pageNameKindOf(app, page)
		if err != nil {
			return nil, err
		}
		plainTitle = untitledNotificationPageTitleOf(row.Language, kind)
	}
	title := quotedEmailTitle(row.Language, plainTitle)
	name := notificationEmailLine(accountLabel(inviter), notificationEmailTitleRunes)
	workspaceName := quotedEmailWorkspaceName(row.Language, notificationEmailLine(workspace.GetString("name"), notificationEmailTitleRunes))
	link := ""
	if origin := othersLinkOrigin(); origin != "" {
		target, err := pageNotificationTarget(app, row.PageID)
		if err != nil {
			return nil, err
		}
		if target == "" {
			target = "/p/" + url.PathEscape(row.PageID)
		}
		link = origin + target
		token, err := signupInvitationToken(app, signupInvitationPage, row.ShareID)
		if err != nil {
			return nil, err
		}
		if token != "" {
			link += "#invitation=" + token
		}
	}
	var inviteMessage string
	if err := app.DB().NewQuery(`SELECT [[invite_message]] FROM {{leafen_page_permissions}} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": row.ShareID}).Row(&inviteMessage); err != nil {
		return nil, err
	}
	inviteMessage = strings.ReplaceAll(strings.ReplaceAll(inviteMessage, "\r\n", "\n"), "\r", "\n")
	var subject string
	var body strings.Builder
	if row.Language == accountLanguageKo {
		subject = name + "님이 " + title + " 페이지에 회원님을 초대했습니다"
		body.WriteString(nameWithEmail(name, inviter.Email(), "") + "님이 리펜의 " + workspaceName + "에 있는 " + title +
			" 페이지에 회원님을 초대했습니다.\r\n\r\n")
		if inviteMessage != "" {
			body.WriteString(strings.ReplaceAll(inviteMessage, "\n", "\r\n") + "\r\n\r\n")
		}
		body.WriteString("이 주소(" + row.RecipientEmail + ")로 가입하면 페이지를 볼 수 있습니다.\r\n")
		body.WriteString(invitationLinkLine(row.Language, link))
		body.WriteString("\r\n--\r\n이 초대를 기대하지 않았다면 이 메일은 무시해도 됩니다.\r\n")
	} else {
		subject = name + " invited you to " + title
		body.WriteString(nameWithEmail(name, inviter.Email(), " ") + " invited you to " + title + " in " + workspaceName +
			" on Leafen.\r\n\r\n")
		if inviteMessage != "" {
			body.WriteString(strings.ReplaceAll(inviteMessage, "\n", "\r\n") + "\r\n\r\n")
		}
		body.WriteString("Sign up with this address (" + row.RecipientEmail + ") to open the page.\r\n")
		body.WriteString(invitationLinkLine(row.Language, link))
		body.WriteString("\r\n--\r\nIf you didn’t expect this invitation, you can ignore this email.\r\n")
	}
	from := (&mail.Address{Name: settings.SenderName, Address: settings.SenderAddress}).String()
	headers := []string{
		"From: " + from,
		"To: " + (&mail.Address{Address: row.RecipientEmail}).String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Auto-Submitted: auto-generated",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String()), nil
}
