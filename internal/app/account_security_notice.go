package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An account's security changing — its password, its email addresses, its
// 2-step verification, its passkeys, a sign-in with a recovery code — tells
// the account by mail, what changed, when and from where, and what to do if
// it wasn't them (2026-10-19: nothing was sent, so someone who took an
// account over went unnoticed). The mail goes to the account's address — and
// to the address it had, when that changed — after the change is done, in
// the background: a mail that can't go leaves the change as it is and is
// kept in the server's log. It carries no token or code. A change someone
// else made — an administrator, the server's command line — says who
// (2026-10-26: their password resets sent nothing).

type accountSecurityChange string

const (
	securityChangePassword      accountSecurityChange = "password"
	securityChangePasswordSet   accountSecurityChange = "password_set"
	securityChangePasswordGone  accountSecurityChange = "password_gone"
	securityChangePasswordReset accountSecurityChange = "password_reset"
	// An administrator or the command line gave a one-time password.
	securityChangePasswordResetBy accountSecurityChange = "password_reset_by"
	securityChangeEmailAdded      accountSecurityChange = "email_added"
	securityChangeEmailRemoved    accountSecurityChange = "email_removed"
	securityChangePrimaryEmail    accountSecurityChange = "primary_email"
	securityChangeTwoStepOn       accountSecurityChange = "two_step_on"
	securityChangeTwoStepOff      accountSecurityChange = "two_step_off"
	securityChangeRecoveryCodes   accountSecurityChange = "recovery_codes"
	securityChangeRecoverySignIn  accountSecurityChange = "recovery_sign_in"
	securityChangePasskeyAdded    accountSecurityChange = "passkey_added"
	securityChangePasskeyRemoved  accountSecurityChange = "passkey_removed"
)

// noticeAccountSecurityChange mails the account that a change was made;
// detail is the address an email change concerns, and also tells the
// addresses besides the account's own.
func noticeAccountSecurityChange(event *core.RequestEvent, userID string, change accountSecurityChange, detail string, also ...string) {
	facts := requestNoticeFacts(event)
	facts.Detail = detail
	if notice, ok := accountSecurityNoticeOf(event.App, userID, change, facts, event.Request, also...); ok {
		sendAccountSecurityNoticeSoon(event.App, notice)
	}
}

// requestNoticeFacts are where a change made by a request came from; its
// device is named once the notice's language is known (accountSecurityNoticeOf).
func requestNoticeFacts(event *core.RequestEvent) accountSecurityNoticeFacts {
	return accountSecurityNoticeFacts{Address: event.RealIP(), Origin: mailServerOrigin(event.App, event.Request),
		userAgent: event.Request.UserAgent(), pageOrigin: event.Request.Header.Get("Origin")}
}

// accountSecurityNoticeMail makes the notice and answers what sends it, or
// nil where nothing goes (no mail settings, no such account). A request sends
// it in the background (sendAccountSecurityNoticeSoon); the command line,
// which ends when it returns, at once. request, nil on the command line,
// chooses the language where the account chose none.
func accountSecurityNoticeMail(app core.App, userID string, change accountSecurityChange, facts accountSecurityNoticeFacts,
	request *http.Request, also ...string) func() error {
	notice, ok := accountSecurityNoticeOf(app, userID, change, facts, request, also...)
	if !ok {
		return nil
	}
	return func() error { return notice.send(app) }
}

// accountSecurityNoticeOf is the notice a change makes, unless nothing goes.
func accountSecurityNoticeOf(app core.App, userID string, change accountSecurityChange, facts accountSecurityNoticeFacts,
	request *http.Request, also ...string) (accountSecurityNoticeSent, bool) {
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return accountSecurityNoticeSent{}, false
	}
	if _, ok := accountSecurityNoticeSettings(app); !ok {
		return accountSecurityNoticeSent{}, false
	}
	language, err := serverTextLanguage(app, userID, request)
	if err != nil {
		language = accountLanguageEn
	}
	// When, on the account's own clock (person_clock.go), and from what, in the notice's language.
	facts.Name, facts.At = strings.TrimSpace(user.GetString("name")), time.Now().In(personLocation(app, userID))
	if facts.Device == "" {
		facts.Device = clientDeviceSummary(facts.userAgent, facts.pageOrigin, language)
	}
	recipients := []string{}
	for _, address := range append([]string{user.Email()}, also...) {
		address = strings.TrimSpace(address)
		if address != "" && !containsString(recipients, address) {
			recipients = append(recipients, address)
		}
	}
	if len(recipients) == 0 {
		return accountSecurityNoticeSent{}, false
	}
	return accountSecurityNoticeSent{UserID: userID, Change: change, Language: language, Facts: facts, Recipients: recipients}, true
}

type accountSecurityNoticeFacts struct {
	Name, Detail, Device, Address, Origin string
	// Who made the change when it wasn't the account: an administrator by
	// name, or the server's command line; Device and Address are theirs.
	ByAdministrator string
	ByCommandLine   bool
	At              time.Time
	// The request's user agent and page origin, which name Device.
	userAgent, pageOrigin string
}

// accountSecurityNotice is a notice's subject and text in the account's language.
func accountSecurityNotice(change accountSecurityChange, language string, facts accountSecurityNoticeFacts) (string, string) {
	korean := language == accountLanguageKo
	what := accountSecurityWhat(change, korean, facts.Detail)
	when := personClockText(facts.At, facts.At.Location())
	where := strings.Trim(strings.Join([]string{facts.Device, facts.Address}, " · "), " ·")
	reset := strings.TrimRight(facts.Origin, "/") + "/"
	// The subject is the first sentence of what changed.
	headline, _, _ := strings.Cut(what, ". ")
	headline = strings.TrimSuffix(headline, ".")
	someoneElse := facts.ByAdministrator != "" || facts.ByCommandLine
	// A reset by its link has already signed every device out.
	signedOut := change == securityChangePasswordReset
	if korean {
		lines := []string{"리펜 계정에서 " + what, "", "언제: " + when}
		if facts.ByAdministrator != "" {
			lines = append(lines, "누가: 서버 관리자 "+facts.ByAdministrator)
		} else if facts.ByCommandLine {
			lines = append(lines, "누가: 서버의 명령줄(leafen account)")
		}
		if where != "" {
			lines = append(lines, "어디서: "+where)
		}
		switch {
		case someoneElse:
			lines = append(lines, "", "모르는 일이면 서버 관리자에게 알리세요.")
		case signedOut:
			lines = append(lines, "", "직접 한 일이면 이 메일은 무시해도 됩니다.",
				"아니라면 바로 비밀번호를 재설정하세요(로그인 화면의 “비밀번호를 잊으셨나요?”).")
		default:
			lines = append(lines, "", "직접 한 일이면 이 메일은 무시해도 됩니다.",
				"아니라면 바로 비밀번호를 재설정하고(로그인 화면의 “비밀번호를 잊으셨나요?”), 설정 → 계정에서 모든 기기에서 로그아웃하세요.")
		}
		if facts.Origin != "" {
			lines = append(lines, reset)
		}
		return "리펜 계정 보안 알림: " + headline, strings.Join(lines, "\r\n")
	}
	lines := []string{"On your Leafen account, " + what, "", "When: " + when}
	if facts.ByAdministrator != "" {
		lines = append(lines, "By: the server’s administrator "+facts.ByAdministrator)
	} else if facts.ByCommandLine {
		lines = append(lines, "By: the server’s command line (leafen account)")
	}
	if where != "" {
		lines = append(lines, "From: "+where)
	}
	switch {
	case someoneElse:
		lines = append(lines, "", "If you don’t know about it, tell the server’s administrator.")
	case signedOut:
		lines = append(lines, "", "If this was you, you can ignore this email.",
			"If it wasn’t, reset your password again now (“Forgot your password?” on the login page).")
	default:
		lines = append(lines, "", "If this was you, you can ignore this email.",
			"If it wasn’t, reset your password now (“Forgot your password?” on the login page) and log out of all devices in Settings → Account.")
	}
	if facts.Origin != "" {
		lines = append(lines, reset)
	}
	return "Leafen account security: " + headline, strings.Join(lines, "\r\n")
}

func accountSecurityWhat(change accountSecurityChange, korean bool, detail string) string {
	type pair struct{ ko, en string }
	texts := map[accountSecurityChange]pair{
		securityChangePassword: {"비밀번호가 바뀌었습니다.", "your password was changed."},
		// A first password and one forgotten are said as such (2026-10-26:
		// both mails said the password was changed).
		securityChangePasswordSet:  {"비밀번호를 정했습니다.", "a password was set."},
		securityChangePasswordGone: {"비밀번호를 삭제했습니다. 이제 로그인된 기기로 로그인합니다.", "the password was removed; the account logs in by its devices."},
		// A reset signs every device out, and says so (2026-10-26: it advised doing it).
		securityChangePasswordReset: {"비밀번호가 재설정되었습니다. 모든 기기에서 로그아웃되었습니다.", "your password was reset, and every device was logged out."},
		securityChangePasswordResetBy: {"비밀번호가 일회용 비밀번호로 재설정되었습니다. 모든 기기에서 로그아웃되었고, 다음 로그인에서 새 비밀번호를 정합니다.",
			"your password was reset to a one-time password; every device was logged out, and a new password is chosen at the next login."},
		securityChangeEmailAdded:     {"이메일 주소가 추가되었습니다: %s", "an email address was added: %s"},
		securityChangeEmailRemoved:   {"이메일 주소가 제거되었습니다: %s", "an email address was removed: %s"},
		securityChangePrimaryEmail:   {"기본 이메일이 바뀌었습니다: %s", "your primary email was changed to %s"},
		securityChangeTwoStepOn:      {"2단계 인증이 켜졌습니다.", "2-step verification was turned on."},
		securityChangeTwoStepOff:     {"2단계 인증이 꺼졌습니다.", "2-step verification was turned off."},
		securityChangeRecoveryCodes:  {"복구 코드가 새로 만들어졌습니다. 전에 받은 코드는 사용할 수 없습니다.", "new recovery codes were made; the earlier ones no longer work."},
		securityChangeRecoverySignIn: {"복구 코드로 로그인되었습니다.", "someone logged in with a recovery code."},
		securityChangePasskeyAdded:   {"패스키가 추가되었습니다.", "a passkey was added."},
		securityChangePasskeyRemoved: {"패스키가 제거되었습니다.", "a passkey was removed."},
	}
	text := texts[change].en
	if korean {
		text = texts[change].ko
	}
	if strings.Contains(text, "%s") {
		text = fmt.Sprintf(text, detail)
	}
	return text
}

var leafenAppDeviceName = mcpWords{"리펜 앱", "Leafen app"}

// clientDeviceSummary names the device a request came from as its person
// reads it, in their language: Leafen's own app on its system ("macOS · 리펜
// 앱") when the request comes from the app's window — the desktop (macOS,
// Windows) and phone (iOS, Android) apps' page, whose origin is the packaged
// client's (packaged_client.go); its user agent is a browser's (the Mac
// app's ends as Safari's, safariUserAgentName), and a browser's page can't
// send that origin. Else the browser and its system (userAgentSummary), or
// nothing (2026-11-23: an agent asked its person to approve the desktop
// app's sign-in as "macOS · Safari").
func clientDeviceSummary(agent, origin, language string) string {
	if !isPackagedClientOrigin(origin) {
		return userAgentSummary(agent)
	}
	return strings.Trim(userAgentSystem(agent)+" · "+leafenAppDeviceName.in(language), " ·")
}

// userAgentSystem names a browser's system ("macOS"), or nothing.
func userAgentSystem(agent string) string {
	for _, known := range []struct{ mark, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Mac OS X", "macOS"},
		{"Windows", "Windows"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(agent, known.mark) {
			return known.name
		}
	}
	return ""
}

// userAgentSummary names a browser's system and browser ("macOS · Chrome"),
// or nothing; clientDeviceSummary names a request's.
func userAgentSummary(agent string) string {
	system := userAgentSystem(agent)
	browser := ""
	for _, known := range []struct{ mark, name string }{
		{"Edg/", "Edge"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(agent, known.mark) {
			browser = known.name
			break
		}
	}
	return strings.Trim(system+" · "+browser, " ·")
}
