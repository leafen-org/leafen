package app

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const authSMTPTimeout = 5 * time.Second

type smtpDeliveryOutcome string

const (
	smtpDeliveryAccepted       smtpDeliveryOutcome = "accepted"
	smtpDeliverySettledFailure smtpDeliveryOutcome = "settled_failure"
	smtpDeliveryUncertain      smtpDeliveryOutcome = "uncertain"
)

func deliverAuthMessage(
	app core.App,
	settings storedAdministratorSettings,
	origin string,
	recipient string,
	kind string,
	language string,
	token string,
) (smtpDeliveryOutcome, error) {
	if !settings.SMTPEnabled || settings.SMTPHost == "" || settings.SenderAddress == "" {
		return smtpDeliverySettledFailure, errAuthMessageInvalid
	}
	password, err := decryptAdministratorSetting(app, settings.SMTPPasswordCipher)
	if err != nil {
		return smtpDeliverySettledFailure, err
	}
	subject, route, body := authMessageContent(kind, language)
	if route == "" {
		return smtpDeliverySettledFailure, errAuthMessageInvalid
	}
	link := strings.TrimRight(origin, "/") + route + "#token=" + token
	message := buildAuthEmail(settings, recipient, subject, body+"\r\n\r\n"+link)
	return sendConfiguredSMTP(settings, password, recipient, message)
}

// authMessageContent is an account email's subject, the page its link
// opens, and the line above the link, in the account's language.
func authMessageContent(kind, language string) (string, string, string) {
	korean := language == accountLanguageKo
	switch {
	case kind == authMessageVerification && korean:
		return "리펜 이메일 확인", "/verify-email", "아래 링크를 열어 이메일 주소를 확인해 주세요."
	case kind == authMessageVerification:
		return "Verify your Leafen email", "/verify-email", "Open the link below to verify your email address."
	case kind == authMessagePasswordReset && korean:
		return "리펜 비밀번호 재설정", "/reset-password", "아래 링크를 열어 비밀번호를 재설정해 주세요."
	case kind == authMessagePasswordReset:
		return "Reset your Leafen password", "/reset-password", "Open the link below to reset your password."
	default:
		return "", "", ""
	}
}

func buildAuthEmail(settings storedAdministratorSettings, recipient, subject, body string) []byte {
	from := (&mail.Address{Name: settings.SenderName, Address: settings.SenderAddress}).String()
	to := (&mail.Address{Address: recipient}).String()
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n")
}

// The step of a delivery that failed: what the administrator's test says,
// and what the log of a real delivery names (2026-10-26: a failed test said
// only that it couldn't be tested).
type smtpStep string

const (
	smtpStepConnect    smtpStep = "connect"
	smtpStepEncryption smtpStep = "encryption"
	smtpStepSignIn     smtpStep = "sign-in"
	smtpStepSender     smtpStep = "sender"
	smtpStepRecipient  smtpStep = "recipient"
	smtpStepMessage    smtpStep = "message"
)

type smtpStepError struct {
	step smtpStep
	err  error
}

func (failure smtpStepError) Error() string {
	return "smtp " + string(failure.step) + ": " + failure.err.Error()
}
func (failure smtpStepError) Unwrap() error { return failure.err }

func smtpFailedAt(step smtpStep, err error) error { return smtpStepError{step: step, err: err} }

func sendConfiguredSMTP(settings storedAdministratorSettings, password, recipient string, message []byte) (smtpDeliveryOutcome, error) {
	address := net.JoinHostPort(settings.SMTPHost, fmt.Sprint(settings.SMTPPort))
	dialer := &net.Dialer{Timeout: authSMTPTimeout}
	var connection net.Conn
	var err error
	if settings.SMTPTLSMode == administratorSMTPTLSTLS {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, authTLSConfig(settings.SMTPHost))
	} else {
		connection, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		// Reached, but no TLS the other way: the encryption's setting.
		var reaching *net.OpError
		if settings.SMTPTLSMode == administratorSMTPTLSTLS && !(errors.As(err, &reaching) && reaching.Op == "dial") {
			return smtpDeliverySettledFailure, smtpFailedAt(smtpStepEncryption, err)
		}
		return smtpDeliverySettledFailure, smtpFailedAt(smtpStepConnect, err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(authSMTPTimeout)); err != nil {
		return smtpDeliverySettledFailure, smtpFailedAt(smtpStepConnect, err)
	}
	client, err := smtp.NewClient(connection, settings.SMTPHost)
	if err != nil {
		return smtpDeliverySettledFailure, smtpFailedAt(smtpStepConnect, err)
	}
	defer client.Close()
	if settings.SMTPTLSMode == administratorSMTPTLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return smtpDeliverySettledFailure, smtpFailedAt(smtpStepEncryption, errors.New("the server offers no STARTTLS"))
		}
		if err := client.StartTLS(authTLSConfig(settings.SMTPHost)); err != nil {
			return smtpDeliverySettledFailure, smtpFailedAt(smtpStepEncryption, err)
		}
	}
	if settings.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.SMTPUsername, password, settings.SMTPHost)); err != nil {
			// Go sends a password over an encrypted connection only.
			if strings.Contains(err.Error(), "unencrypted connection") {
				return smtpDeliverySettledFailure, smtpFailedAt(smtpStepEncryption, err)
			}
			return smtpDeliverySettledFailure, smtpFailedAt(smtpStepSignIn, err)
		}
	}
	if err := client.Mail(settings.SenderAddress); err != nil {
		return smtpDeliverySettledFailure, smtpFailedAt(smtpStepSender, err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return smtpDeliverySettledFailure, smtpFailedAt(smtpStepRecipient, err)
	}
	writer, err := client.Data()
	if err != nil {
		return smtpDeliverySettledFailure, smtpFailedAt(smtpStepMessage, err)
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return smtpDeliveryUncertain, smtpFailedAt(smtpStepMessage, err)
	}
	if err := writer.Close(); err != nil {
		return smtpDeliveryUncertain, smtpFailedAt(smtpStepMessage, err)
	}
	// A successful DATA close includes the server's acceptance response. QUIT
	// is connection cleanup and cannot make that accepted external effect safe
	// to retry.
	_ = client.Quit()
	return smtpDeliveryAccepted, nil
}

// smtpFailureCode is the error code a failed delivery's step gives: the
// server not reached or not answering, encryption, sign-in, or the sender
// or recipient refused.
func smtpFailureCode(err error) string {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return errorCodeEmailServerTimeout
	}
	var failure smtpStepError
	if !errors.As(err, &failure) {
		return errorCodeEmailDeliveryFailed
	}
	switch failure.step {
	case smtpStepConnect:
		return errorCodeEmailServerUnreachable
	case smtpStepEncryption:
		return errorCodeEmailEncryptionFailed
	case smtpStepSignIn:
		return errorCodeEmailSignInRefused
	case smtpStepSender:
		return errorCodeEmailSenderRefused
	case smtpStepRecipient:
		return errorCodeEmailRecipientRefused
	}
	return errorCodeEmailDeliveryFailed
}

func authTLSConfig(host string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
}
