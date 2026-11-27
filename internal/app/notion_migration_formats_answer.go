package app

import (
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// What the person's Notion tab shows when the reader hands its reading in
// (notion_migration_formats_read.go). Taken, the tab goes straight back to
// the Notion page it came from. Otherwise it stays on a page saying why, in
// the person's language — their account's when the key names their
// migration, the browser's otherwise — with a way back to that Notion page
// (2026-11-18: an ended key's page said so in English only, with a link
// reading just "Notion").

// notionMigrationFormatsWords is one sentence of that page, in English and
// Korean.
type notionMigrationFormatsWords [2]string

var (
	notionMigrationFormatsTakenWords = notionMigrationFormatsWords{
		"Leafen has what Notion’s pages and views look like.",
		"리펜이 Notion 페이지와 보기의 꾸밈을 받았습니다."}
	notionMigrationFormatsUnreadableWords = notionMigrationFormatsWords{
		"Leafen couldn’t read what was sent.",
		"리펜이 보내온 내용을 읽지 못했습니다."}
	notionMigrationFormatsEndedWords = notionMigrationFormatsWords{
		"This reader’s key has ended. Ask your agent for a new one.",
		"이 꾸밈 읽기의 키가 끝났습니다. 에이전트에게 새로 받아 달라고 하세요."}
	notionMigrationFormatsKeyUnreadableWords = notionMigrationFormatsWords{
		"Leafen couldn’t read this reader’s key.",
		"리펜이 이 꾸밈 읽기의 키를 확인하지 못했습니다."}
	notionMigrationFormatsKeepFailedWords = notionMigrationFormatsWords{
		"Leafen couldn’t keep what was read.",
		"리펜이 읽은 것을 저장하지 못했습니다."}
	// When the reading came in, in the person's clock.
	notionMigrationFormatsUsedWords = notionMigrationFormatsWords{
		"Leafen already has this reader’s reading, from %s. To read again, ask your agent for a new one.",
		"리펜이 이 꾸밈 읽기가 넘긴 것을 이미 받았습니다(%s). 다시 읽으려면 에이전트에게 새로 받아 달라고 하세요."}
	notionMigrationFormatsBusyWords = notionMigrationFormatsWords{
		serverBusyMessage,
		"서버가 바쁩니다. 잠시 뒤 다시 해 보세요."}
)

// notionMigrationFormatsCallsRefused lets the reader's hand-in through and
// refuses a call to it (api_page_refusals.go): only the reader sends it, as a
// form from the person's Notion tab, so that what it read never passes
// through the agent.
func notionMigrationFormatsCallsRefused(event *core.RequestEvent) error {
	return apiPageRefusal(event, http.StatusBadRequest, errorCodeNotionMigrationInvalid, notionMigrationFormatsCallWords, event.Next,
		apiErrorField{Name: "key", Problem: "the reader's own, sent with what it read as a form from the person's Notion tab"})
}

const notionMigrationFormatsCallWords = "Check key: this takes what the formats reader read, handed in by the reader itself as a form " +
	"from the person's Notion tab with its key — not a call, so that what it read never passes through you. " +
	"leafen_notion_migration_formats gives the reader and Leafen's page to run it from in your browser, then says what came in."

// notionMigrationFormatsLanguage is 1 (Korean) or 0 (English): the
// migration owner's language when the key named their migration, as Leafen
// writes them other texts, else the browser's.
func notionMigrationFormatsLanguage(app core.App, ownerID string, request *http.Request) int {
	language := requestTextLanguageIn(app, request)
	if ownerID != "" {
		if chosen, err := serverTextLanguage(app, ownerID, request); err == nil {
			language = chosen
		}
	}
	if language == accountLanguageKo {
		return 1
	}
	return 0
}

// notionMigrationFormatsBack is the Notion page the reader's tab came from,
// to send it back to; any other address is not followed.
func notionMigrationFormatsBack(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	switch strings.ToLower(parsed.Host) {
	case "app.notion.com", "www.notion.so", "notion.so":
		return parsed.String()
	}
	return ""
}

// notionMigrationFormatsFailure answers a read the server failed to take as
// notionMigrationFormatsAnswer does, through the rules of every failure's
// answer (answeredFailure): logged with its cause, and told busy — 503 with
// Retry-After and the shared busy words — when the database stayed busy.
func notionMigrationFormatsFailure(
	event *core.RequestEvent, back string, language int, words notionMigrationFormatsWords, cause error,
) error {
	status, code, _ := answeredFailure(event, http.StatusInternalServerError, errorCodeNotionMigrationFailed, words[0], cause,
		diagnosticSourcePosition(1))
	if answeredBusy(code) {
		words = notionMigrationFormatsBusyWords
	}
	return notionMigrationFormatsAnswer(event, status, back, language, words)
}

// notionMigrationFormatsAnswer sends a taken reading's tab back to Notion,
// and otherwise answers with the page saying why.
func notionMigrationFormatsAnswer(
	event *core.RequestEvent, status int, back string, language int, words notionMigrationFormatsWords,
) error {
	if back != "" && status == http.StatusOK {
		return event.Redirect(http.StatusSeeOther, back)
	}
	way := back
	if way == "" {
		way = "https://www.notion.so/"
	}
	backWords := [2]string{"Back to Notion", "Notion으로 돌아가기"}[language]
	nonce := security.RandomString(24)
	header := event.Response.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	lang := [2]string{"en", "ko"}[language]
	return event.HTML(status, `<!doctype html><html lang="`+lang+`"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width, initial-scale=1"><title>Leafen</title>`+
		`<style nonce="`+nonce+`">body{margin:64px auto;max-width:420px;padding:0 16px;font:14px/1.5 -apple-system,system-ui,sans-serif;color:#37352f;background:#fff}`+
		`a{color:#2383e2}@media (prefers-color-scheme:dark){body{color:#d4d4d4;background:#191919}a{color:#529cca}}</style></head><body>`+
		`<p>`+html.EscapeString(words[language])+`</p><p><a href="`+html.EscapeString(way)+`">`+html.EscapeString(backWords)+`</a></p></body></html>`)
}
