package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A server administrator sends a test message through the saved SMTP
// settings before relying on them, whether or not sending is switched on
// yet. The message goes to the address given, or else to the administrator.
// One test runs at a time, and a failure is logged with a reference the
// console shows.

func registerAdministratorSMTPTestRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/admin/settings/smtp-test", sendAdministratorSMTPTest)
}

func sendAdministratorSMTPTest(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	administrator, _ := authenticatedUser(event)
	input := administratorSMTPTestInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the recipient address.")
	}
	recipient := strings.TrimSpace(input.Recipient)
	if recipient == "" {
		recipient = administrator.Email()
	}
	recipient, ok := normalizeEmailIdentity(recipient)
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the recipient address.")
	}
	release := acquireProductOperationLock("administrator-smtp-test", "default")
	defer release()
	settings, err := loadAdministratorSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the administrator settings.", err)
	}
	if settings.SMTPHost == "" || settings.SenderAddress == "" {
		return apiError(event, http.StatusConflict, errorCodeAdministratorSettingsInvalid, "Save the SMTP host and sender address first.")
	}
	password, err := decryptAdministratorSetting(event.App, settings.SMTPPasswordCipher)
	if err != nil {
		return administratorSettingsSecretError(event, err)
	}
	language, err := serverTextLanguage(event.App, administrator.Id, event.Request)
	if err != nil {
		language = requestTextLanguageIn(event.App, event.Request)
	}
	subject, body := administratorSMTPTestContent(language)
	message := buildAuthEmail(settings, recipient, subject, body)
	outcome, err := sendConfiguredSMTP(settings, password, recipient, message)
	if outcome == smtpDeliverySettledFailure {
		// Which step failed says what to fix: the host and port, the
		// encryption, the user name and password, or an address.
		code := smtpFailureCode(err)
		return apiErrorWithCause(event, http.StatusBadGateway, code,
			"Couldn't send the test email. Check the SMTP settings.", "administrator-smtp-test", "send",
			code, err)
	}
	result := administratorSMTPTestResult{Outcome: administratorSMTPTestAccepted, Recipient: recipient}
	if outcome == smtpDeliveryUncertain {
		result.Outcome = administratorSMTPTestUncertain
	}
	return event.JSON(http.StatusOK, result)
}

// administratorSMTPTestContent is the test email in the sending
// administrator's language.
func administratorSMTPTestContent(language string) (string, string) {
	if language == accountLanguageKo {
		return "리펜 테스트 메일", "리펜 서버의 SMTP 설정으로 보낸 테스트 메일입니다. 이 메일을 받았다면 이메일 전송이 동작합니다."
	}
	return "Leafen test email", "This test email was sent with the Leafen server’s SMTP settings. If you received it, email delivery works."
}
