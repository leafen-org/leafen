package app

import (
	"mime"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A few API routes answer a person's browser with a page of their own: the
// Notion tab handing in what the formats reader read
// (notion_migration_formats_answer.go), an activity email's unsubscribe link
// (notification_email_message.go) and an outside sign-in coming back
// (sign_in_callback_page.go). A program's call to one — an agent's REST
// request or leafen_call, which no browser opens — hears a refusal as the
// API's JSON error, with its code, its words and the field it is about,
// never the page's HTML, from which it can't read why (2026-11-26:
// leafen_call POST /notion-migration-formats with an empty body answered 400
// with the page). What such a route answers when it does what it is for
// stays its page, whoever asks.

// apiRequestIsPage is whether a request is a browser's for a page: a
// navigation, one that takes HTML, or a form a page sends.
func apiRequestIsPage(request *http.Request) bool {
	if request.Header.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(request.Header.Get("Accept"), "text/html") {
		return true
	}
	mediaType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return mediaType == "application/x-www-form-urlencoded" || mediaType == "multipart/form-data"
}

// apiPageRefusal answers a page route's refusal: its page, to a browser's
// page; to a call, the API's error with this status, code and words, and the
// fields named.
func apiPageRefusal(event *core.RequestEvent, status int, code, words string, page func() error, fields ...apiErrorField) error {
	if apiRequestIsPage(event.Request) {
		return page()
	}
	if len(fields) > 0 {
		apiErrorAbout(event, nil, fields...)
	}
	return apiError(event, status, code, words)
}
