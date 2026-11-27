package app

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
)

// notionBodyKind is how the public API reads a request's body, by its
// Content-Type alone, as Notion's does (seen 2026-11-10).
type notionBodyKind int

const (
	// notionBodyUnread is no body, or one of a type Notion doesn't read:
	// text/plain, a form, text/json, application/merge-patch+json,
	// octet-stream, no type or one that doesn't parse. It reads as an empty
	// body, whatever it holds and however large.
	notionBodyUnread notionBodyKind = iota
	// notionBodyJSON is application/json in any case, in UTF-8 or the
	// UTF-16 its charset names.
	notionBodyJSON
	// notionBodyJSONCharsetRefused is application/json in a charset Notion
	// doesn't read, refused before anything else of the request.
	notionBodyJSONCharsetRefused
	// notionBodyMultipart is multipart/*, read by the endpoint that takes a
	// file (refuseNotionMultipartBody elsewhere).
	notionBodyMultipart
)

const (
	notionMultipartNoBoundary = "A `multipart/form-data` request names its boundary in the `Content-Type` header."
	notionMultipartInvalid    = "This `multipart/form-data` request can't be read: check its headers, form data and boundaries."
	// notionUnreadBodyDrainBytes is how much of a body Notion doesn't read
	// is still taken off the connection before answering, so a client
	// still sending it gets the answer (Notion answered a 6 MB one).
	notionUnreadBodyDrainBytes = 8 << 20
)

// notionJSONDecode turns a JSON body into UTF-8; nil is a body in UTF-8.
type notionJSONDecode func(raw []byte) ([]byte, error)

// notionRequestBodyKind is how a request's body is read and, for JSON in
// UTF-16, how it turns into UTF-8. A request without a body (no
// Content-Length and no Transfer-Encoding, as a GET) is read as none,
// whatever its type says.
func notionRequestBodyKind(request *http.Request) (notionBodyKind, notionJSONDecode) {
	if len(request.TransferEncoding) == 0 && request.Header.Get("Content-Length") == "" && request.ContentLength <= 0 {
		return notionBodyUnread, nil
	}
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	switch {
	case err != nil:
		return notionBodyUnread, nil
	case mediaType == "application/json":
		if decode, read := notionJSONCharset(strings.ToLower(params["charset"])); read {
			return notionBodyJSON, decode
		}
		return notionBodyJSONCharsetRefused, nil
	case strings.HasPrefix(mediaType, "multipart/"):
		return notionBodyMultipart, nil
	}
	return notionBodyUnread, nil
}

// notionJSONCharset is whether Notion reads JSON in charset and, unless it
// is UTF-8, how: UTF-8 and UTF-16 only, named "utf-…" ("UTF-8", "utf-8",
// "utf-16le" and "utf-16be" are read; "utf8", "utf-32", "ascii" and
// "latin1" aren't; seen 2026-11-10), its punctuation aside. UTF-16 without
// a byte order mark is little-endian unless its first hundred characters
// look big-endian (seen: big-endian without one is read).
func notionJSONCharset(charset string) (notionJSONDecode, bool) {
	if charset == "" {
		return nil, true
	}
	if !strings.HasPrefix(charset, "utf-") {
		return nil, false
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, charset)
	little16 := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	big16 := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	switch name {
	case "utf8":
		return nil, true
	case "utf16":
		return func(raw []byte) ([]byte, error) {
			if notionLooksBigEndian16(raw) {
				return notionDecodeUTF(big16, raw)
			}
			return notionDecodeUTF(little16, raw)
		}, true
	case "utf16le":
		return func(raw []byte) ([]byte, error) { return notionDecodeUTF(little16, raw) }, true
	case "utf16be":
		return func(raw []byte) ([]byte, error) { return notionDecodeUTF(big16, raw) }, true
	}
	return nil, false
}

func notionDecodeUTF(charset encoding.Encoding, raw []byte) ([]byte, error) {
	text, err := charset.NewDecoder().Bytes(raw)
	return bytes.TrimPrefix(text, []byte("\uFEFF")), err
}

// notionLooksBigEndian16 is UTF-16 with a big-endian byte order mark, or
// without one more of whose first hundred characters read as ASCII
// big-endian than little-endian.
func notionLooksBigEndian16(raw []byte) bool {
	if len(raw) >= 2 && (raw[0] == 0xFE && raw[1] == 0xFF || raw[0] == 0xFF && raw[1] == 0xFE) {
		return raw[0] == 0xFE
	}
	big, little := 0, 0
	for index := 0; index+2 <= len(raw) && index < 200; index += 2 {
		if raw[index] == 0 && raw[index+1] != 0 {
			big++
		}
		if raw[index] != 0 && raw[index+1] == 0 {
			little++
		}
	}
	return big > little
}

// refuseNotionBodyByType reads a public API request's body as its
// Content-Type says, before anything else of the request, token and address
// too (Notion's order, seen 2026-11-10): JSON in a charset Notion doesn't
// read is refused 415 whatever its size, JSON past
// notionPublicAPIMaxBodyBytes 413 (refuseNotionOversizedJSON), JSON in
// UTF-16 is handed on in UTF-8, and a body of a type Notion
// doesn't read is handed on empty. A multipart body is left to the endpoint
// (refuseNotionMultipartBody), and an OAuth token request keeps its body as
// sent.
func refuseNotionBodyByType(event *core.RequestEvent) (bool, error) {
	kind, decode := notionRequestBodyKind(event.Request)
	switch kind {
	case notionBodyJSONCharsetRefused:
		return true, notionBodyParserRefused(event, http.StatusUnsupportedMediaType, "UnknownApiError", "Something went wrong.", 0)
	case notionBodyJSON:
		if refused, err := refuseNotionOversizedJSON(event); refused {
			return true, err
		}
		if decode != nil {
			notionDecodeRequestBody(event.Request, decode)
		}
	case notionBodyUnread:
		switch event.Request.URL.Path {
		case notionAPIOAuthTokenPath, notionAPIOAuthRevokePath, notionAPIOAuthIntrospectPath:
		default:
			notionEmptyRequestBody(event.Request)
		}
	}
	return false, nil
}

// notionDecodeRequestBody hands a JSON body on in UTF-8. Its size was
// bounded before (refuseNotionOversizedJSON).
func notionDecodeRequestBody(request *http.Request, decode notionJSONDecode) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, notionPublicAPIMaxBodyBytes+1))
	if err != nil || len(raw) > notionPublicAPIMaxBodyBytes {
		request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), request.Body))
		return
	}
	text, err := decode(raw)
	if err != nil {
		text = raw
	}
	request.Body = io.NopCloser(bytes.NewReader(text))
	request.ContentLength = int64(len(text))
	request.Header.Set("Content-Length", strconv.Itoa(len(text)))
	request.Header.Set("Content-Type", "application/json")
}

// notionEmptyRequestBody hands a request on with an empty body, the one it
// came with taken off the connection.
func notionEmptyRequestBody(request *http.Request) {
	if request.Body != nil && request.Body != http.NoBody {
		_, _ = io.Copy(io.Discard, io.LimitReader(request.Body, notionUnreadBodyDrainBytes))
	}
	request.Body = http.NoBody
	request.ContentLength = 0
	request.Header.Del("Content-Length")
}

// refuseNotionMultipartBody reads a multipart body sent to an endpoint that
// takes no file as Notion's does (seen 2026-11-10: on a known address,
// before the token): multipart/form-data without a boundary, another
// multipart type and a body that doesn't read as one are refused, and so is
// a field or a file; a body of no parts is an empty body.
func refuseNotionMultipartBody(event *core.RequestEvent) (bool, error) {
	if kind, _ := notionRequestBodyKind(event.Request); kind != notionBodyMultipart {
		return false, nil
	}
	mediaType, params, _ := mime.ParseMediaType(event.Request.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" && params["boundary"] == "" {
		return true, notionMultipartRefused(event, notionMultipartNoBoundary)
	}
	if mediaType != "multipart/form-data" {
		return true, notionMultipartRefused(event, notionMultipartInvalid)
	}
	reader := multipart.NewReader(io.LimitReader(event.Request.Body, notionPublicAPIMaxBodyBytes), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			notionEmptyRequestBody(event.Request)
			return false, nil
		}
		if err != nil {
			return true, notionMultipartRefused(event, notionMultipartInvalid)
		}
		// A part that isn't form data isn't counted.
		disposition, dispositionParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if disposition != "form-data" {
			continue
		}
		if _, file := dispositionParams["filename"]; file {
			return true, notionMultipartRefused(event, "Too many files")
		}
		return true, notionMultipartRefused(event, "Too many fields")
	}
}

// notionMultipartRefused answers as Notion refuses a multipart body: its
// error shape without a request_id (the request's id is in its header).
func notionMultipartRefused(event *core.RequestEvent, message string) error {
	reference := notionRequestID(event)
	notionAnswerWithoutRequestID(event)
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	logAPIDiagnostic(
		event, http.StatusBadRequest, notionErrorValidation, reference,
		diagnosticOperation(event), "request", "multipart_refused", nil, diagnosticSourcePosition(1),
	)
	return event.JSON(http.StatusBadRequest, struct {
		Object  string `json:"object"`
		Status  int    `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Object: "error", Status: http.StatusBadRequest, Code: notionErrorValidation, Message: message})
}
