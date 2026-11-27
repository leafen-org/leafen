package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"html"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notificationEmailTitleRunes   = 120
	notificationEmailPreviewRunes = 200
)

// configuredPublicOrigin is the server's public address, which emails,
// invitations, shares and agents' connections use: the address the
// administrator wrote first while no way out is chosen
// (server_tunnel_start_address.go), then the chosen way's (a tunnel of their
// own before a quick one, as only one runs). Without either an email carries
// no links and the others fall back to the address a request came to.
func configuredPublicOrigin() string {
	if written := standingStartAddress(); written != "" {
		return written
	}
	return chosenServerOrigin()
}

// othersLinkOrigin is the public address as links other people open carry it
// — mails, invitations, Slack messages: none where it is only this computer's
// (LEAFEN_PUBLIC_URL=http://127.0.0.1…), which no one else opens, so such a
// message says what one sent with no address says (2026-11-22). A home or
// office network's address stays: people there open it.
func othersLinkOrigin() string {
	origin := configuredPublicOrigin()
	if parsed, err := url.Parse(origin); err == nil && mcpLoopbackHost(parsed.Host) {
		return ""
	}
	return origin
}

// configuredServerAddress is the public address the administrator wrote
// (LEAFEN_PUBLIC_URL), if they did.
func configuredServerAddress() string {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("LEAFEN_PUBLIC_URL")), "/")
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return ""
	}
	return parsed.String()
}

func notificationEmailLine(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return value
}

func notificationEmailActor(language string, actors map[string]string, actorID string) string {
	if name := notificationEmailLine(actors[actorID], 80); name != "" {
		return name
	}
	if language == accountLanguageKo {
		return "누군가"
	}
	return "Someone"
}

// untitledNotificationPageTitle names an untitled page in a message as the
// app names it on screen ("새 페이지", "New page").
func untitledNotificationPageTitle(language string) string {
	if language == accountLanguageKo {
		return "새 페이지"
	}
	return "New page"
}

// quotedEmailTitle sets a page's, a teamspace's or an app's name apart from
// the sentence around it (2026-10-16: "운영 관리자님의 워크스페이스에 있는
// 시작하기에 회원님을" ran together), and names an untitled one in the
// reader's language as a page; a caller naming something else gives its own
// untitled name (quotedEmailWorkspaceName, untitledNotificationPageTitleOf).
func quotedEmailTitle(language string, title string) string {
	if title == "" {
		title = untitledNotificationPageTitle(language)
	}
	return "“" + title + "”"
}

// quotedEmailWorkspaceName sets a workspace's name apart as quotedEmailTitle
// does, an unnamed one as the app calls it on screen ("제목 없는 워크스페이스",
// "Untitled workspace"), not as a page.
func quotedEmailWorkspaceName(language string, name string) string {
	if name != "" {
		return quotedEmailTitle(language, name)
	}
	if language == accountLanguageKo {
		return quotedEmailTitle(language, "제목 없는 워크스페이스")
	}
	return quotedEmailTitle(language, "Untitled workspace")
}

// untitledNotificationPageTitleOf names an untitled page in a message by its
// kind, as the app names it (web/src/lib/pageTitle.ts
// untitledPageDisplayTitle): a full-page database "새 데이터베이스", one of
// several sources by them ("A 및 B"), a view block's page "제목 없음", else a
// page.
func untitledNotificationPageTitleOf(language string, kind pageNameKind) string {
	switch {
	case kind.Kind == pageKindDatabase && !kind.ViewBlock && len(kind.SourceTitles) > 1:
		return untitledDatabaseSourcesNameIn(language, kind.SourceTitles)
	case kind.Kind == pageKindDatabase && kind.ViewBlock && language == accountLanguageKo:
		return "제목 없음"
	case kind.Kind == pageKindDatabase && kind.ViewBlock:
		return "Untitled"
	case kind.Kind == pageKindDatabase && language == accountLanguageKo:
		return "새 데이터베이스"
	case kind.Kind == pageKindDatabase:
		return "New database"
	}
	return untitledNotificationPageTitle(language)
}

// notificationEmailHeadline is a notice's headline in words, a database
// page's title naming a page closed to its reader "@찾을 수 없음"
// (notification_reader_mentions.go).
func notificationEmailHeadline(language string, item storedNotificationEmailItem, actor string) string {
	plain := notificationEmailLine(closedMentionsInTitle(item.Title, language), notificationEmailTitleRunes)
	if plain == "" && noticeNamesItsItem(item.ActivityKey) {
		plain = untitledNotificationPageTitleOf(language, item.NameKind)
	}
	title := quotedEmailTitle(language, plain)
	if plain == "" {
		plain = strings.Trim(title, "“”")
	}
	switch {
	// An automation left unnamed is its maker's, as the inbox reads it.
	case strings.HasPrefix(item.ActivityKey, "automation-notification:") && item.Title == "" && item.ActorID != "" && language == accountLanguageKo:
		return "자동화 알림: " + actor + "님의 자동화 기능"
	case strings.HasPrefix(item.ActivityKey, "automation-notification:") && item.Title == "" && item.ActorID != "":
		return "Automation: " + actor + "’s automation"
	case strings.HasPrefix(item.ActivityKey, teamspaceJoinRequestActivityPrefix) && language == accountLanguageKo:
		return actor + "님이 " + title + " 팀스페이스 참가를 요청했습니다"
	case strings.HasPrefix(item.ActivityKey, teamspaceJoinRequestActivityPrefix):
		return actor + " asked to join the " + title + " teamspace"
	case strings.HasPrefix(item.ActivityKey, teamspaceJoinApprovedActivityPrefix) && language == accountLanguageKo:
		return actor + "님이 " + title + " 팀스페이스 참가를 승인했습니다"
	case strings.HasPrefix(item.ActivityKey, teamspaceJoinApprovedActivityPrefix):
		return actor + " approved your request to join the " + title + " teamspace"
	case strings.HasPrefix(item.ActivityKey, workspaceMemberAddedActivityPrefix) && language == accountLanguageKo:
		return actor + "님이 회원님을 " + quotedEmailWorkspaceName(language, item.Title) + "에 추가했습니다"
	case strings.HasPrefix(item.ActivityKey, workspaceMemberAddedActivityPrefix):
		return actor + " added you to " + quotedEmailWorkspaceName(language, item.Title)
	case strings.HasPrefix(item.ActivityKey, pagePermissionNoticePrefix) && language == accountLanguageKo:
		return actor + "님이 " + title + "의 권한을 업데이트했습니다"
	case strings.HasPrefix(item.ActivityKey, pagePermissionNoticePrefix):
		return actor + " updated permission for " + title
	case item.Kind == "system":
		return systemNoticeHeadline(language, item.ActivityKey, title, plain)
	case language == accountLanguageKo && item.Kind == "mention":
		return actor + "님이 " + title + "에서 회원님을 멘션했습니다"
	case language == accountLanguageKo && item.Kind == "page_edit":
		return actor + "님이 " + title + " 페이지를 업데이트했습니다"
	case language == accountLanguageKo && item.Kind == "reminder":
		return "리마인더: " + plain
	case language == accountLanguageKo && item.Kind == "invite":
		return actor + "님이 " + title + "에 회원님을 초대했습니다"
	case language == accountLanguageKo && item.Kind == "access_request":
		return actor + "님이 " + title + "에 대한 액세스를 요청했습니다"
	case item.Kind == "access_request":
		return actor + " requested access to " + title
	case item.Kind == "invite":
		return actor + " invited you to " + title
	case item.Kind == "page_edit":
		return actor + " updated " + title
	case item.Kind == "reminder":
		return "Reminder: " + plain
	case language == accountLanguageKo:
		return actor + "님이 " + title + "에 댓글을 남겼습니다"
	case item.Kind == "mention":
		return actor + " mentioned you in " + title
	default:
		return actor + " commented in " + title
	}
}

// systemNoticeHeadline words a notice Leafen itself sends, by what it is
// about; one it has no words for reads as its title.
func systemNoticeHeadline(language, activityKey, title, plain string) string {
	ko := language == accountLanguageKo
	switch {
	case strings.HasPrefix(activityKey, "app-revoked:") && ko:
		return title + " 앱이 목록에서 삭제되어 중지되었습니다"
	case strings.HasPrefix(activityKey, "app-revoked:"):
		return title + " was revoked by its catalog and stopped"
	case strings.HasPrefix(activityKey, "app-stopped:") && ko:
		return title + " 앱이 예산을 계속 초과해 중지되었습니다"
	case strings.HasPrefix(activityKey, "app-stopped:"):
		return title + " kept going over its budget and stopped"
	case strings.HasPrefix(activityKey, appRequestNoticeKind) && ko:
		return title + " 앱 설치 요청"
	case strings.HasPrefix(activityKey, appRequestNoticeKind):
		return "Request for " + title
	case strings.HasPrefix(activityKey, "app-request-approved:") && ko:
		return "이제 " + title + " 앱을 사용할 수 있습니다"
	case strings.HasPrefix(activityKey, "app-request-approved:"):
		return "You can now use " + title
	case strings.HasPrefix(activityKey, "app-request-declined:") && ko:
		return title + " 앱 요청이 거부되었습니다"
	case strings.HasPrefix(activityKey, "app-request-declined:"):
		return "Your request for " + title + " was declined"
	case strings.HasPrefix(activityKey, "app-notice:") && ko:
		return title + " 앱의 알림"
	case strings.HasPrefix(activityKey, "app-notice:"):
		return "Notice from " + title
	case strings.HasPrefix(activityKey, deadlineActivityPrefix) && ko:
		return "놓치면 안 되는 기한: " + plain
	case strings.HasPrefix(activityKey, deadlineActivityPrefix):
		return "A deadline you can’t miss: " + plain
	case strings.HasPrefix(activityKey, "automation-notification:") && ko:
		return "자동화 알림: " + plain
	case strings.HasPrefix(activityKey, "automation-notification:"):
		return "Automation: " + plain
	}
	return plain
}

// buildNotificationEmail writes one plain-text message for a person's
// notifications, with a one-click unsubscribe when the server has a public
// address.
func buildNotificationEmail(
	settings storedAdministratorSettings,
	recipient string,
	language string,
	items []storedNotificationEmailItem,
	actors map[string]string,
	origin string,
	unsubscribe string,
) []byte {
	subject := ""
	if len(items) == 1 {
		subject = notificationEmailHeadline(language, items[0], notificationEmailItemActor(language, actors, items[0]))
	} else if language == accountLanguageKo {
		subject = "리펜 새 알림 " + strconv.Itoa(len(items)) + "개"
	} else {
		subject = strconv.Itoa(len(items)) + " new notifications in Leafen"
	}
	var body strings.Builder
	for index, item := range items {
		if index > 0 {
			body.WriteString("\r\n")
		}
		body.WriteString(notificationEmailHeadline(language, item, notificationEmailItemActor(language, actors, item)))
		body.WriteString("\r\n")
		preview := notificationEmailLine(item.Preview, notificationEmailPreviewRunes)
		if item.Kind == "invite" {
			preview = strings.ReplaceAll(strings.ReplaceAll(item.Preview, "\r\n", "\n"), "\r", "\n")
			preview = inviteMessageQuoted(item.Kind, strings.ReplaceAll(preview, "\n", "\r\n  "))
		}
		if preview != "" {
			body.WriteString("  " + preview + "\r\n")
		}
		if strings.HasPrefix(item.ActivityKey, deadlineActivityPrefix) && language == accountLanguageKo {
			body.WriteString("  리펜에서 [확인]을 누를 때까지 다시 알립니다.\r\n")
		} else if strings.HasPrefix(item.ActivityKey, deadlineActivityPrefix) {
			body.WriteString("  Leafen tells you again until you press Acknowledge there.\r\n")
		}
		if origin != "" && strings.HasPrefix(item.Target, "/") {
			body.WriteString("  " + origin + item.Target + "\r\n")
		}
	}
	body.WriteString("\r\n--\r\n")
	if language == accountLanguageKo {
		body.WriteString("설정 → 알림에서 이메일 알림을 바꿀 수 있습니다.\r\n")
		if unsubscribe != "" {
			body.WriteString("활동 이메일 받지 않기: " + unsubscribe + "\r\n")
		}
	} else {
		body.WriteString("Change email notifications in Settings → Notifications.\r\n")
		if unsubscribe != "" {
			body.WriteString("Stop activity emails: " + unsubscribe + "\r\n")
		}
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
	if unsubscribe != "" {
		headers = append(headers,
			"List-Unsubscribe: <"+unsubscribe+">",
			"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		)
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String())
}

func notificationEmailUnsubscribeKey(secret string) []byte {
	digest := sha256.Sum256([]byte("leafen-notification-email-unsubscribe-v1\x00" + secret))
	return digest[:]
}

// notificationEmailUnsubscribeToken names one person and can only turn their
// activity emails off, so it may live in an email indefinitely.
func notificationEmailUnsubscribeToken(app core.App, userID string) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	return userID + "." + notificationEmailUnsubscribeSignature(secret, userID), nil
}

func notificationEmailUnsubscribeUser(app core.App, request *http.Request) (string, bool, error) {
	token := strings.TrimSpace(request.URL.Query().Get("token"))
	userID, signature, found := strings.Cut(token, ".")
	if !found || userID == "" || len(userID) > maxWorkspacePermissionIdentifierLength {
		return "", false, nil
	}
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", false, err
	}
	if !hmac.Equal([]byte(signature), []byte(notificationEmailUnsubscribeSignature(secret, userID))) {
		return "", false, nil
	}
	if _, err := app.FindRecordById("users", userID); err != nil {
		return "", false, nil
	}
	return userID, true, nil
}

func notificationEmailUnsubscribeSignature(secret, userID string) string {
	mac := hmac.New(sha256.New, notificationEmailUnsubscribeKey(secret))
	mac.Write([]byte(userID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func notificationEmailUnsubscribePage(language, message, form string) string {
	lang := "ko"
	if language != accountLanguageKo {
		lang = "en"
	}
	return "<!doctype html><html lang=\"" + lang + "\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Leafen</title></head>" +
		"<body style=\"font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem\">" +
		"<p>" + html.EscapeString(message) + "</p>" + form + "</body></html>"
}

func notificationEmailUnsubscribeResponse(event *core.RequestEvent, status int, language, message, form string) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Referrer-Policy", "no-referrer")
	return event.HTML(status, notificationEmailUnsubscribePage(language, message, form))
}

// notificationEmailUnsubscribeLanguage is the account's language for its
// unsubscribe pages, or the language the request asks for when the account
// cannot be read.
func notificationEmailUnsubscribeLanguage(event *core.RequestEvent, userID string) string {
	language, err := serverTextLanguage(event.App, userID, event.Request)
	if err != nil {
		return requestTextLanguageIn(event.App, event.Request)
	}
	return language
}

// notificationEmailUnsubscribeInvalid answers a link that names no account
// in the language the request asks for; a call, which no email's link is,
// hears it as the API's error (api_page_refusals.go).
func notificationEmailUnsubscribeInvalid(event *core.RequestEvent) error {
	language := requestTextLanguageIn(event.App, event.Request)
	message := "이 링크를 사용할 수 없습니다."
	if language != accountLanguageKo {
		message = "This link cannot be used."
	}
	return apiPageRefusal(event, http.StatusNotFound, errorCodeNotificationEmailLinkNotFound, notificationEmailUnsubscribeCallWords, func() error {
		return notificationEmailUnsubscribeResponse(event, http.StatusNotFound, language, message, "")
	}, apiErrorField{Name: "?token", Problem: "not one of an email Leafen sent"})
}

const (
	errorCodeNotificationEmailLinkNotFound = "LEAFEN_NOTIFICATION_EMAIL_LINK_NOT_FOUND"
	notificationEmailUnsubscribeCallWords  = "Check token: that unsubscribe link isn't one of an email Leafen sent. This is the page " +
		"an activity email's link opens; PATCH /account/notifications with emailActivity false stops the person's activity emails."
)

// notificationEmailUnsubscribeLabel names the emails an unsubscribe link
// stops: activity emails, or one of the digests.
func notificationEmailUnsubscribeLabel(request *http.Request, language string) string {
	kind := request.URL.Query().Get("kind")
	switch {
	case language == accountLanguageKo && kind == notificationDigestPageUpdates:
		return "페이지 업데이트 이메일"
	case language == accountLanguageKo && kind == notificationDigestWorkspace:
		return "워크스페이스 요약 이메일"
	case language == accountLanguageKo:
		return "활동 이메일"
	case kind == notificationDigestPageUpdates:
		return "page update emails"
	case kind == notificationDigestWorkspace:
		return "workspace digest emails"
	default:
		return "activity emails"
	}
}

// confirmNotificationEmailUnsubscribe only asks; a link scanner that follows
// the email link changes nothing.
func confirmNotificationEmailUnsubscribe(event *core.RequestEvent) error {
	userID, valid, err := notificationEmailUnsubscribeUser(event.App, event.Request)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed, "Couldn’t load the notification settings.", err)
	}
	if !valid {
		return notificationEmailUnsubscribeInvalid(event)
	}
	language := notificationEmailUnsubscribeLanguage(event, userID)
	message, button := notificationEmailUnsubscribeLabel(event.Request, language)+"을 더 받지 않을까요?", "이메일 받지 않기"
	if language != accountLanguageKo {
		message, button = "Stop Leafen "+notificationEmailUnsubscribeLabel(event.Request, language)+"?", "Stop emails"
	}
	return notificationEmailUnsubscribeResponse(event, http.StatusOK, language, message,
		`<form method="post"><button type="submit">`+html.EscapeString(button)+`</button></form>`)
}

// unsubscribeNotificationEmail turns activity emails off, from the confirm
// page or a mail client's one-click unsubscribe.
func unsubscribeNotificationEmail(event *core.RequestEvent) error {
	userID, valid, err := notificationEmailUnsubscribeUser(event.App, event.Request)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed, "Couldn’t save the notification settings.", err)
	}
	if !valid {
		return notificationEmailUnsubscribeInvalid(event)
	}
	off := false
	input := notificationSettingsInput{EmailActivity: &off}
	switch event.Request.URL.Query().Get("kind") {
	case notificationDigestPageUpdates:
		input = notificationSettingsInput{EmailPageUpdates: &off}
	case notificationDigestWorkspace:
		input = notificationSettingsInput{EmailDigest: &off}
	}
	if _, err := saveNotificationSettings(event.App, userID, input, time.Now().UTC()); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed, "Couldn’t save the notification settings.", err)
	}
	language := notificationEmailUnsubscribeLanguage(event, userID)
	message := notificationEmailUnsubscribeLabel(event.Request, language) + "을 더 보내지 않습니다. 설정 → 알림에서 다시 켤 수 있습니다."
	if language != accountLanguageKo {
		message = "Leafen " + notificationEmailUnsubscribeLabel(event.Request, language) + " are off. Turn them back on in Settings → Notifications."
	}
	return notificationEmailUnsubscribeResponse(event, http.StatusOK, language, message, "")
}
