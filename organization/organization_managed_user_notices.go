package organization

import (
	"context"
	"errors"
	"mime"
	"net/mail"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Notion tells managed users by email when their organization resets their
// password or removes them from a workspace (its help, read 2026-10-02).
// Those notices wait in a queue the email runner sends, as page invitations
// do (page_share_invite_emails.go); without email settings they are skipped.

const (
	managedUserNoticePasswordReset    = "password_reset"
	managedUserNoticeWorkspaceRemoved = "workspace_removed"
	managedUserNoticeBatch            = 16
	managedUserNoticeMaxAttempts      = 5
)

type storedManagedUserNotice struct {
	ID               string `db:"id"`
	UserID           string `db:"user_id"`
	Kind             string `db:"kind"`
	OrganizationName string `db:"organization_name"`
	WorkspaceName    string `db:"workspace_name"`
	Attempts         int    `db:"attempts"`
}

// queueManagedUserNotice queues one email to a managed user, sent after the
// transaction commits.
func queueManagedUserNotice(app core.App, organizationID, userID, kind, workspaceName string) error {
	var organizationName string
	if err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_organizations}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": organizationID}).Row(&organizationName); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_managed_user_notices}}
			([[id]], [[organization_id]], [[user_id]], [[kind]], [[organization_name]], [[workspace_name]],
			 [[status]], [[attempts]], [[next_attempt_at]], [[created_at]], [[updated_at]])
		VALUES ({:id}, {:organizationId}, {:userId}, {:kind}, {:organizationName}, {:workspaceName},
			'pending', 0, {:now}, {:now}, {:now})
	`).Bind(dbx.Params{
		"id": security.RandomString(32), "organizationId": organizationID, "userId": userID, "kind": kind,
		"organizationName": organizationName, "workspaceName": workspaceName, "now": now,
	}).Execute(); err != nil {
		return err
	}
	leafen.NotifyNotificationEmailRunnerAfterCommit(app)
	return nil
}

// nextManagedUserNoticeDue is when the next queued notice is due, or -1.
func nextNoticeDue(ctx context.Context, app core.App) (int64, error) {
	var due int64 = -1
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[next_attempt_at]]), -1) FROM {{leafen_managed_user_notices}} WHERE [[status]] = 'pending'
	`).WithContext(ctx).Row(&due)
	return due, err
}

// sendDueManagedUserNotices sends the notices that are due; only an unsent
// one is retried, so an uncertain send never repeats.
func sendDueNotices(ctx context.Context, app core.App, now time.Time) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_managed_user_notices}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_managed_user_notices}}
			WHERE [[status]] != 'pending' AND [[updated_at]] < {:before} ORDER BY [[updated_at]], [[id]] LIMIT {:limit})
	`).WithContext(ctx).Bind(dbx.Params{
		"before": now.Add(-leafen.NotificationEmailRetention).UnixMilli(), "limit": leafen.NotificationEmailCleanupLimit,
	}).Execute(); err != nil {
		return err
	}
	rows := []storedManagedUserNotice{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[user_id]], [[kind]], [[organization_name]], [[workspace_name]], [[attempts]]
		FROM {{leafen_managed_user_notices}}
		WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": managedUserNoticeBatch}).All(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	settle := func(row storedManagedUserNotice, status, code string, attempts int, next int64) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_managed_user_notices}} SET [[status]] = {:status}, [[error_code]] = {:code},
				[[attempts]] = {:attempts}, [[next_attempt_at]] = {:next}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": status, "code": code, "attempts": attempts, "next": next, "now": now.UnixMilli(), "id": row.ID,
		}).Execute()
		return err
	}
	retry := func(row storedManagedUserNotice, code string) error {
		if row.Attempts+1 >= managedUserNoticeMaxAttempts {
			return settle(row, "failed", code, row.Attempts+1, 0)
		}
		return settle(row, "pending", code, row.Attempts+1, now.Add(time.Minute<<row.Attempts).UnixMilli())
	}
	settings, err := leafen.LoadAdministratorSettings(app)
	if err != nil {
		return err
	}
	configured := leafen.NotificationEmailConfigured(settings)
	password := ""
	var passwordErr error
	if configured {
		password, passwordErr = leafen.DecryptAdministratorSetting(app, settings.SMTPPasswordCipher)
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
		user, err := app.FindRecordById("users", row.UserID)
		if err != nil || strings.TrimSpace(user.Email()) == "" {
			turnErrors = append(turnErrors, settle(row, "skipped", "recipient_missing", row.Attempts, 0))
			continue
		}
		if passwordErr != nil {
			turnErrors = append(turnErrors, retry(row, "smtp_settings_unreadable"))
			continue
		}
		language, err := leafen.ServerTextLanguage(app, row.UserID, nil)
		if err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		recipient := strings.TrimSpace(user.Email())
		outcome, sendErr := leafen.SendConfiguredSMTP(settings, password, recipient, buildManagedUserNotice(settings, recipient, language, row))
		switch outcome {
		case leafen.SMTPDeliveryAccepted:
			turnErrors = append(turnErrors, settle(row, "sent", "", row.Attempts+1, 0))
		case leafen.SMTPDeliveryUncertain:
			leafen.LogNotificationEmailFailure(app, "managed-user-notice-uncertain", sendErr)
			turnErrors = append(turnErrors, settle(row, "failed", "delivery_uncertain", row.Attempts+1, 0))
		default:
			leafen.LogNotificationEmailFailure(app, "managed-user-notice-failed", sendErr)
			turnErrors = append(turnErrors, retry(row, "delivery_failed"))
		}
	}
	return errors.Join(turnErrors...)
}

// buildManagedUserNotice writes the notice in the person's language, with
// the server's address when it has one.
func buildManagedUserNotice(settings leafen.StoredAdministratorSettings, recipient, language string, row storedManagedUserNotice) []byte {
	organization := leafen.NotificationEmailLine(row.OrganizationName, leafen.NotificationEmailTitleRunes)
	workspace := leafen.NotificationEmailLine(row.WorkspaceName, leafen.NotificationEmailTitleRunes)
	korean := language == leafen.AccountLanguageKo
	var subject string
	var body strings.Builder
	switch {
	case row.Kind == managedUserNoticePasswordReset && korean:
		subject = "리펜 비밀번호를 다시 정해 주세요"
		body.WriteString(organization + " 관리자가 리펜 계정의 비밀번호 재설정을 요청했습니다.\r\n")
		body.WriteString("다음에 리펜을 열 때 지금 비밀번호를 입력하고 새 비밀번호를 정하세요.\r\n")
	case row.Kind == managedUserNoticePasswordReset:
		subject = "Reset your Leafen password"
		body.WriteString("An administrator of " + organization + " asked you to reset your Leafen password.\r\n")
		body.WriteString("Next time you open Leafen, enter your current password and choose a new one.\r\n")
	case korean:
		subject = "리펜 워크스페이스에서 제거되었습니다"
		body.WriteString(organization + " 관리자가 회원님을 리펜 워크스페이스 " + workspace + "에서 제거했습니다.\r\n")
	default:
		subject = "You were removed from a Leafen workspace"
		body.WriteString("An administrator of " + organization + " removed you from the Leafen workspace " + workspace + ".\r\n")
	}
	if origin := leafen.ConfiguredPublicOrigin(); origin != "" {
		body.WriteString("\r\n" + origin + "\r\n")
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
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String())
}

// managedUserNotices sends the emails queued for managed people.
type managedUserNotices struct{}

func (managedUserNotices) NextDue(ctx context.Context, app core.App) (int64, error) {
	return nextNoticeDue(ctx, app)
}

func (managedUserNotices) SendDue(ctx context.Context, app core.App, now time.Time) error {
	return sendDueNotices(ctx, app, now)
}
