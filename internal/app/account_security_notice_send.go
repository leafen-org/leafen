package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// accountSecurityNoticeSent is a security notice as it goes: to whom, in
// which language, saying what. A request sends it after it answers, and a
// notice the server's stop kept from going is sent as the server starts
// again (product_afterwork_kept.go), saying when the change was made.
type accountSecurityNoticeSent struct {
	UserID     string                     `json:"userId"`
	Change     accountSecurityChange      `json:"change"`
	Language   string                     `json:"language"`
	Facts      accountSecurityNoticeFacts `json:"facts"`
	Recipients []string                   `json:"recipients"`
}

// accountSecurityNoticeSettings are the server's mail settings, when it mails.
func accountSecurityNoticeSettings(app core.App) (storedAdministratorSettings, bool) {
	settings, err := loadAdministratorSettings(app)
	if err != nil || !settings.SMTPEnabled || settings.SMTPHost == "" || settings.SenderAddress == "" {
		return storedAdministratorSettings{}, false
	}
	return settings, true
}

// send mails the notice with the server's mail settings now: none, and
// nothing goes. A failure is kept in the server's log.
func (notice accountSecurityNoticeSent) send(app core.App) (failed error) {
	defer func() {
		if recover() != nil {
			failed = fmt.Errorf("the notice couldn't be sent")
		}
	}()
	settings, ok := accountSecurityNoticeSettings(app)
	if !ok {
		return nil
	}
	// The time the change was made, on the account's clock (person_clock.go).
	facts := notice.Facts
	facts.At = facts.At.In(personLocation(app, notice.UserID))
	subject, body := accountSecurityNotice(notice.Change, notice.Language, facts)
	password, err := decryptAdministratorSetting(app, settings.SMTPPasswordCipher)
	if err != nil {
		app.Logger().Warn("leafen account security notice not sent", "change", string(notice.Change), "cause", "smtp-password")
		return err
	}
	for _, recipient := range notice.Recipients {
		if _, err := sendConfiguredSMTP(settings, password, recipient, buildAuthEmail(settings, recipient, subject, body)); err != nil {
			app.Logger().Warn("leafen account security notice not sent", "change", string(notice.Change), "cause", err.Error())
			failed = err
		}
	}
	return failed
}

// sendAccountSecurityNoticeSoon sends a request's notice after it answers.
func sendAccountSecurityNoticeSoon(app core.App, notice accountSecurityNoticeSent) {
	encoded, err := json.Marshal(notice)
	if err != nil {
		return
	}
	productAfterworkOf(app).run(afterworkSecurityNotice, afterworkInput(string(encoded)), func(context.Context) error {
		return notice.send(app)
	})
}

// resumeAccountSecurityNotice sends again a notice the server's stop kept
// from going.
func resumeAccountSecurityNotice(app core.App, encoded string) {
	notice := accountSecurityNoticeSent{}
	if err := json.Unmarshal([]byte(encoded), &notice); err != nil || len(notice.Recipients) == 0 {
		app.Logger().Warn("leafen account security notice left by the last stop is unreadable")
		return
	}
	sendAccountSecurityNoticeSoon(app, notice)
}
