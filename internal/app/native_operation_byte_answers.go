package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http/httptest"
	"strings"
	"unicode/utf8"
)

// An operation's answer comes back as it came. JSON is its body; words — an
// answer whose content type says text (text/*, JSON, XML and the like) or
// names none, in UTF-8 — are its text, the last line's newline too
// (2026-11-10: an app reading a note "맑음\n" got "맑음", and writing it back
// would change the file); and bytes — a file's download, an export, a backup,
// a picture — come as body_base64, with their content type, size and file
// name, as leafen_call takes bytes in (2026-11-22: a file's download came
// back as text with its bytes mangled). Bytes come back up to
// mcpAttachmentInlineMaximumBytes, as many as notion-download-attachment
// gives a file's words inline; a larger answer keeps only that many while
// it is made, and says its size and how to download it, as Notion's MCP
// gives a file as an address rather than its bytes — an agent's leafen_call
// keeps it whole besides and gives a download address of its own
// (native_operation_answer_downloads.go). An agent's leafen_call holds its
// words and JSON to the same size: a larger one comes as its size, a note
// and that download address, not whole into the conversation (2026-11-22: a
// 587 KB CSV came back as 586,836 characters of text while a 300 KB file of
// bytes came as an address). An app's call reads its words and JSON whole.

// nativeOperationRecorder records an operation's answer as httptest's
// recorder does, keeping of an answer that comes back no larger than
// mcpAttachmentInlineMaximumBytes no more than that, and counting the rest.
type nativeOperationRecorder struct {
	*httptest.ResponseRecorder
	total int
	// downloads keeps a larger answer whole in spool, where the call asks for
	// it: an agent's leafen_call, whose every answer comes back no larger.
	downloads *operationAnswerDownloads
	spool     *operationAnswerSpool
}

func newNativeOperationRecorder() *nativeOperationRecorder {
	return &nativeOperationRecorder{ResponseRecorder: httptest.NewRecorder()}
}

// heldToSize tells whether the answer comes back no larger than
// mcpAttachmentInlineMaximumBytes: every answer of an agent's leafen_call,
// and any other call's answer of bytes.
func (recorder *nativeOperationRecorder) heldToSize() bool {
	if recorder.downloads != nil {
		return true
	}
	contentType := recorder.Header().Get("Content-Type")
	return contentType != "" && !nativeOperationAnswerWords(contentType)
}

func (recorder *nativeOperationRecorder) Write(data []byte) (int, error) {
	recorder.total += len(data)
	kept := data
	if recorder.heldToSize() {
		// What came before this is all kept in Body, being no more than
		// comes back.
		if recorder.spool == nil && recorder.downloads != nil && recorder.total > mcpAttachmentInlineMaximumBytes {
			if recorder.spool = recorder.downloads.open(); recorder.spool != nil {
				recorder.spool.write(recorder.Body.Bytes())
			}
		}
		if recorder.spool != nil {
			recorder.spool.write(data)
		}
		room := max(0, mcpAttachmentInlineMaximumBytes+1-recorder.Body.Len())
		kept = data[:min(len(data), room)]
	}
	if _, err := recorder.ResponseRecorder.Write(kept); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (recorder *nativeOperationRecorder) WriteString(text string) (int, error) {
	return recorder.Write([]byte(text))
}

// nativeOperationAnswerWords tells whether a content type says words: text,
// JSON or XML of any kind, and the like.
func nativeOperationAnswerWords(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	switch {
	case strings.HasPrefix(mediaType, "text/"), strings.HasSuffix(mediaType, "+json"), strings.HasSuffix(mediaType, "+xml"):
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript", "application/x-ndjson", "application/yaml",
		"application/x-yaml", "application/x-www-form-urlencoded", "application/graphql", "application/sql":
		return true
	}
	return false
}

// nativeOperationAnswerJSON tells whether a content type says JSON, or the
// answer names none.
func nativeOperationAnswerJSON(contentType string) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	return contentType == "" || mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// readAnswer reads a recorded answer into the result: JSON as its body,
// words as its text, and bytes as body_base64; an answer larger than comes
// back (heldToSize), as its content type, its size and how to download it.
func (result *nativeOperationResult) readAnswer(recorder *nativeOperationRecorder, method, path, query string) {
	header, whole := recorder.Header(), recorder.Body.Bytes()
	contentType, complete := header.Get("Content-Type"), recorder.total == len(whole)
	raw := bytes.TrimSpace(whole)
	if len(raw) == 0 || complete && nativeOperationAnswerJSON(contentType) && json.Unmarshal(raw, &result.Body) == nil {
		return
	}
	if complete && (contentType == "" || nativeOperationAnswerWords(contentType)) && utf8.Valid(whole) {
		result.Text = string(whole)
		return
	}
	result.ContentType, result.Bytes = contentType, recorder.total
	if _, values, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		result.Filename = values["filename"]
	}
	if complete && recorder.total <= mcpAttachmentInlineMaximumBytes {
		result.BodyBase64 = base64.StdEncoding.EncodeToString(whole)
		return
	}
	target := apiRoot + path
	if query != "" {
		target += "?" + strings.TrimPrefix(query, "?")
	}
	result.Note = fmt.Sprintf("This answer is %d bytes, more than the %d bytes an answer gives back: download it instead. "+
		"A file Leafen keeps: notion-get-file-download-urls with its ID gives a download address. Anything else: send the same request, "+
		"%s %s, over HTTP with a Leafen connection key (lmc_…) as the bearer token.",
		recorder.total, mcpAttachmentInlineMaximumBytes, strings.ToUpper(method), target)
}

// nativeOperationAppAnswer is an operation's answer as an app's worker reads
// it from leafen_api: its status, body and text, and an answer of bytes as
// body_base64 with what they are.
func nativeOperationAppAnswer(result nativeOperationResult) any {
	return struct {
		Status      int    `json:"status"`
		Body        any    `json:"body"`
		Text        string `json:"text"`
		BodyBase64  string `json:"body_base64,omitempty"`
		ContentType string `json:"content_type,omitempty"`
		Bytes       int    `json:"bytes,omitempty"`
		Filename    string `json:"filename,omitempty"`
		Note        string `json:"note,omitempty"`
	}{result.Status, result.Body, result.Text, result.BodyBase64, result.ContentType, result.Bytes, result.Filename, result.Note}
}
