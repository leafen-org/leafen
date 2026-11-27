package app

import (
	"errors"
	"mime"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// When scheduled backups keep failing: from the second failure in a row the
// server's administrators hear of it by mail, when the server sends mail, at
// most once a day while it goes on, and see a band at the top of the app
// until a backup succeeds again.

const (
	scheduledBackupAlertFailures = 2
	scheduledBackupAlertEvery    = 24 * time.Hour
)

// recordScheduledBackupFailure counts a failed run and, from the second in
// a row, mails the administrators once a day while it goes on.
func recordScheduledBackupFailure(app core.App, code string, now time.Time) error {
	return recordScheduledBackupRunFailure(app, administratorBackupProgress{ErrorCode: code}, now)
}

// recordScheduledBackupRunFailure is recordScheduledBackupFailure for a run
// as it ended, whose mail says why in the place's words and the common cause
// of that answer (backup_failure_reason.go).
func recordScheduledBackupRunFailure(app core.App, run administratorBackupProgress, now time.Time) error {
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[failure_count]] = [[failure_count]] + 1
		WHERE [[id]] = 'default'`).Execute(); err != nil {
		return err
	}
	var failures, alertedAt int64
	if err := app.DB().NewQuery(`SELECT [[failure_count]], [[alerted_at]] FROM {{leafen_scheduled_backups}} WHERE [[id]] = 'default'`).
		Row(&failures, &alertedAt); err != nil {
		return err
	}
	if failures < scheduledBackupAlertFailures || (alertedAt > 0 && now.Sub(time.UnixMilli(alertedAt)) < scheduledBackupAlertEvery) {
		return nil
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil || !notificationEmailConfigured(settings) {
		return err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[alerted_at]] = {:now} WHERE [[id]] = 'default'`).
		Bind(dbx.Params{"now": now.UnixMilli()}).Execute(); err != nil {
		return err
	}
	return mailServerAdministrators(app, settings, "scheduled-backup-alert-failed", func(recipient, language string) []byte {
		return buildScheduledBackupRunAlert(settings, recipient, language, run, failures)
	})
}

// mailServerAdministrators mails each of the server's administrators, in
// their language, with the server's mail settings.
func mailServerAdministrators(app core.App, settings storedAdministratorSettings, failure string, build func(recipient, language string) []byte) error {
	password, err := decryptAdministratorSetting(app, settings.SMTPPasswordCipher)
	if err != nil {
		return err
	}
	administrators := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_administrators}} ORDER BY [[user_id]]`).Column(&administrators); err != nil {
		return err
	}
	failed := []error{}
	for _, id := range administrators {
		user, err := app.FindRecordById("users", id)
		if err != nil || strings.TrimSpace(user.Email()) == "" {
			continue
		}
		language, err := serverTextLanguage(app, id, nil)
		if err != nil {
			language = ""
		}
		recipient := strings.TrimSpace(user.Email())
		if outcome, err := sendConfiguredSMTP(settings, password, recipient, build(recipient, language)); outcome != smtpDeliveryAccepted {
			logNotificationEmailFailure(app, failure, err)
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

// clearScheduledBackupFailures ends the count once a backup succeeds.
func clearScheduledBackupFailures(app core.App) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[failure_count]] = 0, [[alerted_at]] = 0
		WHERE [[id]] = 'default'`).Execute()
	return err
}

// buildScheduledBackupAlert writes the mail in the administrator's language.
func buildScheduledBackupAlert(settings storedAdministratorSettings, recipient, language, code string, failures int64) []byte {
	return buildScheduledBackupRunAlert(settings, recipient, language, administratorBackupProgress{ErrorCode: code}, failures)
}

// buildScheduledBackupRunAlert is the mail of a run as it ended: why, what
// the place answered and that answer's common cause, as the screen says them.
func buildScheduledBackupRunAlert(settings storedAdministratorSettings, recipient, language string, run administratorBackupProgress, failures int64) []byte {
	count := strconv.FormatInt(failures, 10)
	korean := language == accountLanguageKo
	var subject string
	var body strings.Builder
	if korean {
		subject = "리펜 예약 백업이 계속 실패하고 있습니다"
		body.WriteString("리펜 예약 백업이 " + count + "번 연속 실패했습니다(" + scheduledBackupAlertReason(run.ErrorCode, true) + ").\r\n")
	} else {
		subject = "Leafen’s scheduled backups keep failing"
		body.WriteString("Leafen’s scheduled backup failed " + count + " times in a row (" + scheduledBackupAlertReason(run.ErrorCode, false) + ").\r\n")
	}
	if cause := backupFailureCauseSay(run.ErrorCause, korean); cause != "" {
		body.WriteString(cause + "\r\n")
	}
	if run.ErrorReason != "" {
		details := "Details: "
		if korean {
			details = "자세히: "
		}
		body.WriteString(details + run.ErrorReason + "\r\n")
	}
	if korean {
		body.WriteString("설정 → 서버 → 백업에서 백업 위치를 확인해 주세요. 백업이 다시 되면 이 알림은 멈춥니다.\r\n")
	} else {
		body.WriteString("Check the backup location in Settings → Server → Backups. These mails stop once a backup succeeds.\r\n")
	}
	if origin := othersLinkOrigin(); origin != "" {
		body.WriteString("\r\n" + origin + "\r\n")
	}
	return serverAdministratorMail(settings, recipient, subject, body.String())
}

// serverAdministratorMail is a plain-text mail from the server.
func serverAdministratorMail(settings storedAdministratorSettings, recipient, subject, body string) []byte {
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
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

// scheduledBackupAlertReason says why the last run failed, as the settings
// screen does.
func scheduledBackupAlertReason(code string, korean bool) string {
	reasons := map[string][2]string{
		errorCodeScheduledBackupFolderUnavailable:  {"the backup location can’t be reached", "백업 위치에 연결할 수 없음"},
		errorCodeScheduledBackupNoSpace:            {"no space left there", "공간이 부족함"},
		errorCodeScheduledBackupWebDAVUnauthorized: {"the WebDAV server refused access", "WebDAV 서버가 접근을 거부함"},
		errorCodeScheduledBackupS3Unauthorized:     {"the storage refused access", "저장소가 접근을 거부함"},
		errorCodeScheduledBackupPlaceMissing:       {"the backup location isn’t there", "백업 위치가 없음"},
		errorCodeScheduledBackupPlaceMoved:         {"the backup location’s address sends elsewhere", "백업 위치의 주소가 다른 곳으로 넘겨 보냄"},
		errorCodeScheduledBackupPlaceUnreachable:   {"the backup location doesn’t answer", "백업 위치가 응답하지 않음"},
	}
	reason, ok := reasons[code]
	if !ok {
		reason = [2]string{"the backup couldn’t be written", "백업을 저장하지 못함"}
	}
	if korean {
		return reason[1]
	}
	return reason[0]
}
