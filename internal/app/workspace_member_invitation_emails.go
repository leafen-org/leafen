package app

import (
	"context"
	"errors"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An invited address hears of its workspace invitation by email when the
// server sends email, as a page invitation does: once, retried a few times,
// and within the limits page and workspace invitations share per inviter and
// per address. An invitation cancelled or taken up meanwhile, or an address
// an account verifies, is no longer mailed.

type storedWorkspaceInvitationEmail struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	InviterID   string `db:"inviter_id"`
	Email       string `db:"email"`
	Language    string `db:"language"`
	Status      string `db:"status"`
	Attempts    int    `db:"mail_attempts"`
}

// inviteEmailRateLimited is the reason an invitation keeps when the limits
// held its email back, so whoever invited hears it didn't go rather than
// that it did — or that the server sends no mail, when it does.
const inviteEmailRateLimited = "rate_limited"

// inviteEmailStatus is how a new invitation's email starts: "pending" when
// the server sends email and the inviter and the address are within their
// limits, "none" otherwise — with inviteEmailRateLimited as its reason when
// it is the limits that hold it back.
func inviteEmailStatus(app core.App, inviterID, email string, now time.Time) (string, string, error) {
	settings, err := loadAdministratorSettings(app)
	if err != nil || !notificationEmailConfigured(settings) {
		return "none", "", err
	}
	byInviter, byAddress, err := inviteEmailCounts(app, inviterID, email, now)
	if err != nil {
		return "", "", err
	}
	if byInviter >= pageInviteEmailsPerInviterHour || byAddress >= pageInviteEmailsPerAddressDay {
		return "none", inviteEmailRateLimited, nil
	}
	return "pending", "", nil
}

// inviteEmailCounts counts the invitation emails, of pages and workspaces,
// and the emails of people added to a workspace, an inviter queued in the
// last hour and an address was sent in the last day; one the limits held
// back went nowhere and isn't counted.
func inviteEmailCounts(app core.App, inviterID, recipient string, now time.Time) (int, int, error) {
	nowMillis := now.UnixMilli()
	var byInviter, byAddress int
	if err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_page_invite_emails}}
				WHERE [[inviter_id]] = {:inviter} AND [[created_at]] > {:hourAgo} AND [[error_code]] != {:held}) +
			(SELECT COUNT(*) FROM {{leafen_workspace_invitations}}
				WHERE [[inviter_id]] = {:inviter} AND [[mail_status]] != 'none' AND [[created_at]] > {:hourAgo}),
			(SELECT COUNT(*) FROM {{leafen_page_invite_emails}}
				WHERE [[recipient_email]] = {:recipient} AND [[created_at]] > {:dayAgo} AND [[error_code]] != {:held}) +
			(SELECT COUNT(*) FROM {{leafen_workspace_invitations}}
				WHERE [[email]] = {:recipient} AND [[mail_status]] != 'none' AND [[created_at]] > {:dayAgo})
	`).Bind(dbx.Params{
		"inviter": inviterID, "recipient": normalizePageShareEmail(recipient), "held": inviteEmailRateLimited,
		"hourAgo": nowMillis - time.Hour.Milliseconds(), "dayAgo": nowMillis - 24*time.Hour.Milliseconds(),
	}).Row(&byInviter, &byAddress); err != nil {
		return 0, 0, err
	}
	addedByInviter, addedToAddress, err := workspaceMemberAddedEmailCounts(app, inviterID, recipient, now)
	return byInviter + addedByInviter, byAddress + addedToAddress, err
}

// nextWorkspaceInvitationEmailDue is when the next invitation email is due,
// or -1.
func nextWorkspaceInvitationEmailDue(ctx context.Context, app core.App) (int64, error) {
	var due int64 = -1
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[mail_next_at]]), -1) FROM {{leafen_workspace_invitations}} WHERE [[mail_status]] = 'pending'
	`).WithContext(ctx).Row(&due)
	return due, err
}

// pruneSettledWorkspaceInvitations forgets invitations taken up or
// cancelled long enough ago that no limit counts them.
func pruneSettledWorkspaceInvitations(ctx context.Context, app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_workspace_invitations}}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_workspace_invitations}}
			WHERE [[status]] != 'pending' AND [[updated_at]] < {:before} AND [[mail_status]] != 'pending'
			ORDER BY [[updated_at]], [[id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"before": now.Add(-notificationEmailRetention).UnixMilli(), "limit": notificationEmailCleanupLimit,
	}).Execute()
	return err
}

// sendDueWorkspaceInvitationEmails sends the invitation emails that are
// due. Only an unsent one is retried, so an uncertain send never repeats.
func sendDueWorkspaceInvitationEmails(ctx context.Context, app core.App, now time.Time) error {
	if err := pruneSettledWorkspaceInvitations(ctx, app, now); err != nil {
		return err
	}
	rows := []storedWorkspaceInvitationEmail{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[inviter_id]], [[email]], [[language]], [[status]], [[mail_attempts]]
		FROM {{leafen_workspace_invitations}}
		WHERE [[mail_status]] = 'pending' AND [[mail_next_at]] <= {:now}
		ORDER BY [[mail_next_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": pageInviteEmailBatch}).All(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	settle := func(row storedWorkspaceInvitationEmail, status, code string, attempts int, next int64) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_workspace_invitations}} SET [[mail_status]] = {:status}, [[mail_error]] = {:code},
				[[mail_attempts]] = {:attempts}, [[mail_next_at]] = {:next}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": status, "code": code, "attempts": attempts, "next": next, "now": now.UnixMilli(), "id": row.ID,
		}).Execute()
		return err
	}
	retry := func(row storedWorkspaceInvitationEmail, code string) error {
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
		if row.Status != "pending" {
			turnErrors = append(turnErrors, settle(row, "skipped", "invitation_settled", row.Attempts, 0))
			continue
		}
		if account, err := pageShareEmailAccount(app, row.Email); err != nil {
			turnErrors = append(turnErrors, err)
			continue
		} else if account != "" {
			turnErrors = append(turnErrors, settle(row, "skipped", "account_exists", row.Attempts, 0))
			continue
		}
		if passwordErr != nil {
			turnErrors = append(turnErrors, retry(row, "smtp_settings_unreadable"))
			continue
		}
		message, err := buildWorkspaceInvitationEmail(app, settings, row)
		if err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		outcome, sendErr := sendConfiguredSMTP(settings, password, row.Email, message)
		switch outcome {
		case smtpDeliveryAccepted:
			turnErrors = append(turnErrors, settle(row, "sent", "", row.Attempts+1, 0))
		case smtpDeliveryUncertain:
			logNotificationEmailFailure(app, "workspace-invite-delivery-uncertain", sendErr)
			turnErrors = append(turnErrors, settle(row, "failed", "delivery_uncertain", row.Attempts+1, 0))
		default:
			logNotificationEmailFailure(app, "workspace-invite-delivery-failed", sendErr)
			turnErrors = append(turnErrors, retry(row, "delivery_failed"))
		}
	}
	return errors.Join(turnErrors...)
}

// buildWorkspaceInvitationEmail writes the invitation: who invited the
// address to which workspace, and the server's address when it has one,
// where signing up with the address joins it.
func buildWorkspaceInvitationEmail(app core.App, settings storedAdministratorSettings,
	row storedWorkspaceInvitationEmail) ([]byte, error) {
	workspace, err := app.FindRecordById(workspacesCollection, row.WorkspaceID)
	if err != nil {
		return nil, err
	}
	inviter, err := app.FindRecordById("users", row.InviterID)
	if err != nil {
		return nil, err
	}
	name := notificationEmailLine(accountLabel(inviter), notificationEmailTitleRunes)
	workspaceName := quotedEmailWorkspaceName(row.Language, notificationEmailLine(workspace.GetString("name"), notificationEmailTitleRunes))
	link := othersLinkOrigin()
	if link != "" {
		// The link carries the invitation's sign-up token, which makes the
		// account where sign-up is closed (signup_invitations.go).
		token, err := signupInvitationToken(app, signupInvitationWorkspace, row.ID)
		if err != nil {
			return nil, err
		}
		if token != "" {
			link += "/#invitation=" + token
		}
	}
	// The inviter's note follows the opening line, as a page invitation's.
	var note string
	if err := app.DB().NewQuery(`SELECT [[invite_message]] FROM {{leafen_workspace_invitations}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": row.ID}).Row(&note); err != nil {
		return nil, err
	}
	note = strings.ReplaceAll(strings.ReplaceAll(note, "\r\n", "\n"), "\r", "\n")
	if note != "" {
		note = strings.ReplaceAll(note, "\n", "\r\n") + "\r\n\r\n"
	}
	var subject string
	var body strings.Builder
	if row.Language == accountLanguageKo {
		subject = name + "님이 " + workspaceName + "에 회원님을 초대했습니다"
		body.WriteString(nameWithEmail(name, inviter.Email(), "") + "님이 리펜의 " + workspaceName +
			"에 회원님을 멤버로 초대했습니다.\r\n\r\n")
		body.WriteString(note)
		body.WriteString("이 주소(" + row.Email + ")로 가입하면 워크스페이스에 들어갑니다.\r\n")
		body.WriteString(invitationLinkLine(row.Language, link))
		body.WriteString("\r\n--\r\n이 초대를 기대하지 않았다면 이 메일은 무시해도 됩니다.\r\n")
	} else {
		subject = name + " invited you to " + workspaceName
		body.WriteString(nameWithEmail(name, inviter.Email(), " ") + " invited you to join " + workspaceName +
			" on Leafen as a member.\r\n\r\n")
		body.WriteString(note)
		body.WriteString("Sign up with this address (" + row.Email + ") to join it.\r\n")
		body.WriteString(invitationLinkLine(row.Language, link))
		body.WriteString("\r\n--\r\nIf you didn’t expect this invitation, you can ignore this email.\r\n")
	}
	from := (&mail.Address{Name: settings.SenderName, Address: settings.SenderAddress}).String()
	headers := []string{
		"From: " + from,
		"To: " + (&mail.Address{Address: row.Email}).String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Auto-Submitted: auto-generated",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String()), nil
}

// invitationLinkLine is where an invitation mail says the way in: its link, or
// on a server with no address of its own, which can't say where it is, that the
// inviter can.
func invitationLinkLine(language, link string) string {
	if link != "" {
		return link + "\r\n"
	}
	if language == accountLanguageKo {
		return "이 서버의 주소는 초대한 분께 물어보세요.\r\n"
	}
	return "Ask the person who invited you for this server’s address.\r\n"
}
