package app

import (
	"bytes"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// notionPublicAPIMaxBodyBytes is the most a JSON body to the public API
// holds, as Notion's (seen 2026-11-10: a body of 512,000 bytes is read, one
// of 512,001 refused, in UTF-16 too; a body of another type isn't read at
// all).
const notionPublicAPIMaxBodyBytes = 512_000

// notionPublicAPIMaxReadBytes is the most a body within
// notionPublicAPIMaxBodyBytes holds once handed on in UTF-8
// (notionDecodeRequestBody): UTF-16 grows by half at most.
const notionPublicAPIMaxReadBytes = notionPublicAPIMaxBodyBytes / 2 * 3

// notionBodyParserBody is Notion's answer to a body it can't take at all,
// which isn't its API's error shape, with its keys in Notion's order.
type notionBodyParserBody struct {
	IsNotionError bool   `json:"isNotionError"`
	ErrorID       string `json:"errorId"`
	Name          string `json:"name"`
	Message       string `json:"message"`
	Code          int    `json:"code,omitzero"`
}

// refuseNotionOversizedJSON refuses a JSON body past
// notionPublicAPIMaxBodyBytes before anything else of the request but its
// charset (refuseNotionBodyByType): Notion's refuses it without a token,
// with an unknown one, without Notion-Version, on an unknown path and
// whatever the method (seen 2026-11-10). A body read here is handed on as
// read.
func refuseNotionOversizedJSON(event *core.RequestEvent) (bool, error) {
	if event.Request.Body == nil || event.Request.Body == http.NoBody {
		return false, nil
	}
	if length := event.Request.ContentLength; length > notionPublicAPIMaxBodyBytes {
		return true, notionPayloadTooLarge(event)
	} else if length >= 0 {
		return false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(event.Request.Body, notionPublicAPIMaxBodyBytes+1))
	if err == nil && len(raw) > notionPublicAPIMaxBodyBytes {
		return true, notionPayloadTooLarge(event)
	}
	event.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), event.Request.Body))
	return false, nil
}

func notionPayloadTooLarge(event *core.RequestEvent) error {
	return notionBodyParserRefused(event, http.StatusRequestEntityTooLarge, "PayloadTooLargeError", "Request body too large.", http.StatusRequestEntityTooLarge)
}

// notionBodyParserRefused answers a body Notion refuses before reading it:
// past its bound (413, with its code) or in a charset it doesn't read (415,
// "Something went wrong." without one). Its errorId is a UUID of its own,
// not the request's id, and it names no request_id, as Notion's (seen
// 2026-11-11).
func notionBodyParserRefused(event *core.RequestEvent, status int, name, message string, code int) error {
	reference := uuid.NewString()
	notionRequestID(event)
	notionAnswerWithoutRequestID(event)
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	diagnostic := "payload_too_large"
	if status == http.StatusUnsupportedMediaType {
		diagnostic = "unsupported_charset"
	}
	logAPIDiagnostic(
		event, status, diagnostic, reference,
		diagnosticOperation(event), "request", diagnostic, nil, diagnosticSourcePosition(1),
	)
	return event.JSON(status, notionBodyParserBody{ErrorID: reference, Name: name, Message: message, Code: code})
}
