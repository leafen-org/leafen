package app

import (
	"bufio"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Every refusal and failure under apiRoot comes in one form,
// {"error": {"code", "message", "reference", …}} (diagnostic.go writes it;
// api_error_details.go adds what was not found and which fields are wrong).
// An answer a route makes some other way is put in that form here, at the
// one place every API request passes (2026-11-16, an agent calling every
// operation: a missing preview or frame answered in the framework's own
// {data,message,status}, a missing avatar with no body at all, the live
// collaboration route with the plain text "unauthorized"):
//
//   - an error a handler or middleware returns without answering — the
//     framework's own, a panic's, any other — is answered with its status;
//   - an answer of 400 or more with no body, or a plain-text one, is held
//     back and answered in the form, its text as the message;
//   - a body sent as JSON that doesn't read is refused before its route, as
//     Notion's API refuses it (api_error_unreadable_json.go).
//
// An answer meant for a page or a frame (HTML) and one already in JSON go as
// they are; so does a request that upgrades its connection (the live
// collaboration socket).

const (
	errorCodeNotFound       = "LEAFEN_NOT_FOUND"
	errorCodeRequestRefused = "LEAFEN_REQUEST_REFUSED"
	errorCodeRequestFailed  = "LEAFEN_REQUEST_FAILED"
	// apiErrorFormHeldBytes is how much of a plain-text refusal is kept as
	// its message.
	apiErrorFormHeldBytes = 512
)

// apiErrorFormPriority runs it outside the framework's panic recovery, so a
// panic's answer is put in the form too.
const apiErrorFormPriority = apis.DefaultPanicRecoverMiddlewarePriority - 1

func registerAPIErrorForm(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenAPIErrorForm", Priority: apiErrorFormPriority, Func: keepAPIErrorForm,
	})
}

func keepAPIErrorForm(event *core.RequestEvent) error {
	request := event.Request
	if !strings.HasPrefix(request.URL.Path, apiRoot+"/") || request.Header.Get("Upgrade") != "" {
		return event.Next()
	}
	if refused, err := refuseAPIUnreadableJSON(event); refused {
		return err
	}
	writer := &apiErrorFormWriter{ResponseWriter: event.Response}
	event.Response = writer
	if request.Body != nil && request.Body != http.NoBody {
		kept := &apiBodyKept{ReadCloser: request.Body}
		request.Body = kept
		event.Set(apiBodyKeptKey, kept)
	}
	noteUnreadQuery(request, writer.Header())
	err := event.Next()
	event.Response = writer.ResponseWriter
	switch {
	case writer.held == http.StatusServiceUnavailable && writer.Header().Get("Retry-After") != "" && len(writer.body) == 0:
		// A route told busy with no body of its own (failedStatus) is the
		// shared busy answer, which the app sends again.
		return apiErrorWithCause(event, writer.held, errorCodeServerBusy, serverBusyMessage,
			"api_error_form", "answer", errorCodeServerBusy, err)
	case writer.held != 0:
		return apiErrorWithCause(event, writer.held, apiErrorFormCode(writer.held), apiErrorFormMessage(writer.held, string(writer.body)),
			"api_error_form", "answer", apiErrorFormCode(writer.held), err)
	case err != nil && !writer.passed:
		status, message := http.StatusInternalServerError, ""
		var framework *router.ApiError
		if errors.As(err, &framework) {
			status, message = framework.Status, framework.Message
		}
		return apiErrorWithCause(event, status, apiErrorFormCode(status), apiErrorFormMessage(status, message),
			"api_error_form", "returned", apiErrorFormCode(status), err)
	}
	return err
}

// apiErrorFormCode is the code of an answer whose route named none.
func apiErrorFormCode(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return errorCodeUnauthorized
	case status == http.StatusForbidden:
		return errorCodePermissionDenied
	case status == http.StatusNotFound:
		return errorCodeNotFound
	case status == http.StatusTooManyRequests:
		return errorCodeRateLimited
	case status >= http.StatusInternalServerError:
		return errorCodeRequestFailed
	}
	return errorCodeRequestRefused
}

// apiErrorFormMessage is the text a route gave, one line of words, or its
// status's own words when it gave none.
func apiErrorFormMessage(status int, text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || strings.ContainsFunc(text, func(r rune) bool { return !unicode.IsPrint(r) }) {
		text = http.StatusText(status)
	}
	if text == "" {
		text = "The request failed."
	}
	if last := text[len(text)-1]; last != '.' && last != '!' && last != '?' {
		text += "."
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// apiBodyKeptKey keeps the request's body as it was first read, up to
// apiErrorBodyBytes, for an error to name its fields by
// (api_error_details.go) whoever read it and whatever they put in its
// place.
const apiBodyKeptKey = "leafenAPIBodyKept"

type apiBodyKept struct {
	io.ReadCloser
	read  []byte
	ended bool
}

func (kept *apiBodyKept) Read(data []byte) (int, error) {
	count, err := kept.ReadCloser.Read(data)
	if !kept.ended {
		if room := apiErrorBodyBytes + 1 - len(kept.read); room > 0 {
			kept.read = append(kept.read, data[:min(count, room)]...)
		}
		kept.ended = err == io.EOF
	}
	return count, err
}

// Reread lets the router's body be read again, as it does.
func (kept *apiBodyKept) Reread() {
	if rereader, rereads := kept.ReadCloser.(router.Rereader); rereads {
		rereader.Reread()
	}
}

// apiErrorFormWriter holds back an answer of 400 or more that has no body
// or a plain-text one.
type apiErrorFormWriter struct {
	http.ResponseWriter
	held   int
	body   []byte
	passed bool
}

func (w *apiErrorFormWriter) WriteHeader(status int) {
	if w.passed || w.held != 0 {
		return
	}
	if status >= http.StatusBadRequest && apiErrorFormReplaces(w.Header().Get("Content-Type")) {
		w.held = status
		return
	}
	if status >= http.StatusOK {
		w.passed = true
	}
	w.ResponseWriter.WriteHeader(status)
}

// apiErrorFormReplaces says whether an answer of this type is put in the
// form: none, or plain text.
func apiErrorFormReplaces(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "text/plain"
}

func (w *apiErrorFormWriter) Write(data []byte) (int, error) {
	if !w.passed && w.held == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.held != 0 {
		if room := apiErrorFormHeldBytes - len(w.body); room > 0 {
			w.body = append(w.body, data[:min(room, len(data))]...)
		}
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}

func (w *apiErrorFormWriter) Flush() {
	_ = w.FlushError()
}

func (w *apiErrorFormWriter) FlushError() error {
	if w.held != 0 {
		return nil
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *apiErrorFormWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *apiErrorFormWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Written and Status answer as the router's own writer does, for an answer
// held back too.
func (w *apiErrorFormWriter) Written() bool {
	return w.passed || w.held != 0
}

func (w *apiErrorFormWriter) Status() int {
	if w.held != 0 {
		return w.held
	}
	if tracker, tracks := w.ResponseWriter.(router.StatusTracker); tracks {
		return tracker.Status()
	}
	return 0
}
