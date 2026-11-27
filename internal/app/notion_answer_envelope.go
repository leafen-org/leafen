package app

import (
	"bytes"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// The public API's answers are put in Notion's envelope here, at one place
// every /v1 request passes, as Notion's are (seen 2026-11-11, Notion-Version
// 2026-03-11): each request has its own id, a UUID, in the
// X-Notion-Request-Id header of whatever is answered; a JSON answer says
// "application/json; charset=utf-8"; and every JSON object answered — an
// object, a list or an error — names that id as its last key, request_id,
// whatever its endpoint put there; its objects' keys are in Notion's order
// (notionAnswerShaped). A body refused before it is read
// (413, 415: notionBodyParserRefused), a multipart body refused
// (notionMultipartRefused) and a data source's list of templates
// (serveNotionDataSourceTemplates) carry no request_id, as Notion's don't.

const (
	notionRequestIDKey          = "leafenNotionRequestID"
	notionAnswerWithoutIDKey    = "leafenNotionAnswerWithoutRequestID"
	notionJSONAnswerContentType = "application/json; charset=utf-8"
)

// notionRequestID is the request's own id, made the first time it is asked
// for and named in its answer's X-Notion-Request-Id header.
func notionRequestID(event *core.RequestEvent) string {
	if id, ok := event.Get(notionRequestIDKey).(string); ok && id != "" {
		return id
	}
	id := uuid.NewString()
	event.Set(notionRequestIDKey, id)
	event.Response.Header().Set(notionRequestIDHeader, id)
	return id
}

// notionAnswerWithoutRequestID marks an answer that names no request_id.
func notionAnswerWithoutRequestID(event *core.RequestEvent) {
	event.Set(notionAnswerWithoutIDKey, true)
}

// serveNotionAnswer serves a public API request in Notion's envelope.
func serveNotionAnswer(event *core.RequestEvent, serve func() error) error {
	original := event.Response
	writer := &notionAnswerWriter{ResponseWriter: original, status: http.StatusOK}
	event.Response = writer
	notionRequestID(event)
	err := serve()
	if err != nil && !writer.decided && isSQLiteBusyError(err) {
		// A write a route passed on unanswered is answered as any that
		// waited out its time (notionCompatibilityErrorWithData).
		err = notionRequestNotCompleted(event, err)
	}
	event.Response = original
	if writer.buffering {
		// Its objects read in Notion's order, with its request_id last
		// (notion_answer_key_order.go).
		without, _ := event.Get(notionAnswerWithoutIDKey).(bool)
		body := notionAnswerShaped(writer.body.Bytes(), notionRequestID(event), !without)
		original.Header().Set("Content-Length", strconv.Itoa(len(body)))
		original.WriteHeader(writer.status)
		_, _ = original.Write(body)
	}
	return err
}

// notionAnswerWriter holds a JSON answer until its request_id is named; any
// other answer passes as it is written.
type notionAnswerWriter struct {
	http.ResponseWriter
	status    int
	decided   bool
	buffering bool
	body      bytes.Buffer
}

func (writer *notionAnswerWriter) WriteHeader(status int) {
	if writer.decided {
		return
	}
	writer.decided, writer.status = true, status
	media, _, err := mime.ParseMediaType(writer.Header().Get("Content-Type"))
	if err == nil && media == "application/json" {
		writer.Header().Set("Content-Type", notionJSONAnswerContentType)
		writer.buffering = true
		return
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *notionAnswerWriter) Write(data []byte) (int, error) {
	if !writer.decided {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.buffering {
		return writer.body.Write(data)
	}
	return writer.ResponseWriter.Write(data)
}

// Flush passes on an answer that isn't held.
func (writer *notionAnswerWriter) Flush() {
	if writer.decided && !writer.buffering {
		if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// Written and Status say what the request's handlers answered, held or not.
func (writer *notionAnswerWriter) Written() bool { return writer.decided }

func (writer *notionAnswerWriter) Status() int {
	if !writer.decided {
		return 0
	}
	return writer.status
}

func (writer *notionAnswerWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
