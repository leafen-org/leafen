package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
)

const (
	diagnosticReferencePrefix = "LF-"
	diagnosticUnknownCode     = "LEAFEN_UNKNOWN"
	// statusClientClosedRequest is nginx's code for a request whose client
	// left before its answer.
	statusClientClosedRequest = 499
)

type apiFailureSpec struct {
	Status    int
	Code      string
	Message   string
	Stage     string
	CauseCode string
}

var diagnosticFallbackSequence atomic.Uint64

var productionDiagnosticLogger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelWarn,
}))

func newDiagnosticReference() string {
	value := make([]byte, 9)
	if _, err := rand.Read(value); err != nil {
		sequence := diagnosticFallbackSequence.Add(1)
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", time.Now().UnixNano(), sequence)))
		copy(value, digest[:len(value)])
	}
	encoded := strings.ToUpper(hex.EncodeToString(value))
	return diagnosticReferencePrefix + encoded[:12] + "-" + encoded[12:]
}

func diagnosticSourcePosition(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok || line < 1 {
		return "unresolved"
	}
	normalized := strings.ReplaceAll(file, "\\", "/")
	for _, marker := range []string{"/internal/", "/organization/", "/cmd/", "/web/", "/shared/"} {
		if index := strings.LastIndex(normalized, marker); index >= 0 {
			return fmt.Sprintf("%s:%d", normalized[index+1:], line)
		}
	}
	return "unresolved"
}

func diagnosticOperation(event *core.RequestEvent) string {
	if event == nil || event.Request == nil {
		return "http.unknown"
	}
	method := strings.ToLower(event.Request.Method)
	switch method {
	case "get", "post", "put", "patch", "delete", "options", "head":
		return "http." + method
	default:
		return "http.unknown"
	}
}

func safeDiagnosticToken(value string) string {
	if value == "" || len(value) > 64 {
		return diagnosticUnknownCode
	}
	for index, current := range value {
		valid := current >= 'A' && current <= 'Z' ||
			current >= 'a' && current <= 'z' ||
			current >= '0' && current <= '9' ||
			(index > 0 && (current == '_' || current == '.' || current == ':' || current == '-'))
		if !valid {
			return diagnosticUnknownCode
		}
	}
	return value
}

func logAPIDiagnostic(
	event *core.RequestEvent,
	status int,
	code string,
	reference string,
	operation string,
	stage string,
	causeCode string,
	cause error,
	sourcePosition string,
) {
	if event == nil || event.App == nil {
		return
	}
	// A request its client gave up on, as a page does when it asks again,
	// fails because the client left, not because the server did: it is noted
	// as canceled, not reported as a failure. One a stopping server ended is
	// its failure; one it turned away before any of its work, or whose write
	// it stopped with none of it kept, did nothing and is noted so
	// (server_stopping.go, request_write_stop.go).
	left, turnedAway, writeStopped := requestClientLeft(event), requestTurnedAwayStopping(event), requestWriteStopped(event)
	if left || turnedAway || writeStopped {
		note := "leafen request canceled"
		switch {
		case turnedAway:
			note = "leafen request turned away: the server is stopping"
		case writeStopped:
			note = "leafen request's write stopped: the server is stopping"
		}
		event.App.Logger().Info(note,
			"diagnosticReference", reference,
			"code", safeDiagnosticToken(code),
			"operation", safeDiagnosticToken(operation),
			"stage", safeDiagnosticToken(stage),
			"sourcePosition", sourcePosition,
		)
		return
	}
	logDiagnostic(
		event.App, status, code, reference, operation, stage, causeCode, cause, sourcePosition,
	)
}

type diagnosticSourceCause interface {
	DiagnosticSourcePosition() string
}

type diagnosticStageCause interface {
	DiagnosticStage() string
}

func diagnosticCauseContext(cause error, stage, sourcePosition string) (string, string) {
	var positioned diagnosticSourceCause
	if errors.As(cause, &positioned) && positioned.DiagnosticSourcePosition() != "" {
		sourcePosition = positioned.DiagnosticSourcePosition()
	}
	var staged diagnosticStageCause
	if errors.As(cause, &staged) && staged.DiagnosticStage() != "" {
		stage = staged.DiagnosticStage()
	}
	return stage, sourcePosition
}

func logDiagnostic(
	app core.App,
	status int,
	code string,
	reference string,
	operation string,
	stage string,
	causeCode string,
	cause error,
	sourcePosition string,
) {
	defer func() {
		_ = recover()
	}()
	if app == nil {
		return
	}
	stage, sourcePosition = diagnosticCauseContext(cause, stage, sourcePosition)
	attributes := []any{
		"diagnosticReference", reference,
		"code", safeDiagnosticToken(code),
		"operation", safeDiagnosticToken(operation),
		"stage", safeDiagnosticToken(stage),
		"status", status,
		"sourcePosition", sourcePosition,
	}
	if causeCode != "" {
		attributes = append(attributes, "causeCode", safeDiagnosticToken(causeCode))
	}
	if cause != nil {
		attributes = append(attributes, "causeClass", diagnosticCauseClass(cause))
	}
	if status >= http.StatusInternalServerError {
		app.Logger().Error("leafen request failed", attributes...)
		if !app.IsDev() {
			productionDiagnosticLogger.Error("leafen request failed", attributes...)
		}
		return
	}
	app.Logger().Warn("leafen request failed", attributes...)
	if !app.IsDev() {
		productionDiagnosticLogger.Warn("leafen request failed", attributes...)
	}
}

func logDurableJobDiagnostic(
	app core.App,
	code string,
	reference string,
	jobID string,
	operation string,
	stage string,
	cause error,
) {
	// Where its caller is, not this line (2026-11-16: every job failure the
	// server logged named diagnostic.go as where it stopped).
	logDurableJobDiagnosticAt(app, diagnosticSourcePosition(1), code, reference, jobID, operation, stage, "", cause)
}

func logDurableJobDiagnosticWithCause(
	app core.App,
	code string,
	reference string,
	jobID string,
	operation string,
	stage string,
	causeCode string,
	cause error,
) {
	logDurableJobDiagnosticAt(app, diagnosticSourcePosition(1), code, reference, jobID, operation, stage, causeCode, cause)
}

// logDurableJobDiagnosticAt is where background work says it failed — each
// runner's turn (runner_turn_wait.go) and each durable job. Work only called
// off, as the server's shutdown calls off all of it, didn't fail and says
// nothing here (onlyCalledOff).
func logDurableJobDiagnosticAt(
	app core.App, sourcePosition, code, reference, jobID, operation, stage, causeCode string, cause error, extra ...any,
) {
	defer func() {
		_ = recover()
	}()
	if app == nil || onlyCalledOff(cause) {
		return
	}
	// What a caller adds beside them: a backup's failure in the place's own words (backup_failure_reason.go).
	attributes := append(durableJobDiagnosticAttributes(sourcePosition, code, reference, jobID, operation, stage, causeCode, cause), extra...)
	app.Logger().Error("leafen durable job failed", attributes...)
	if !app.IsDev() {
		productionDiagnosticLogger.Error("leafen durable job failed", attributes...)
	}
}

// durableJobDiagnosticAttributes are what a job's log line says of it and
// its cause.
func durableJobDiagnosticAttributes(sourcePosition, code, reference, jobID, operation, stage, causeCode string, cause error) []any {
	stage, sourcePosition = diagnosticCauseContext(cause, stage, sourcePosition)
	attributes := []any{
		"diagnosticReference", reference,
		"code", safeDiagnosticToken(code),
		"operation", safeDiagnosticToken(operation),
		"stage", safeDiagnosticToken(stage),
		"sourcePosition", sourcePosition,
	}
	if jobID != "" {
		attributes = append(attributes, "jobId", safeDiagnosticToken(jobID))
	}
	if causeCode == "" {
		causeCode = diagnosticCauseCode(cause, code)
	}
	if causeCode != "" {
		attributes = append(attributes, "causeCode", safeDiagnosticToken(causeCode))
	}
	if cause != nil {
		attributes = append(attributes, "causeClass", diagnosticCauseClass(cause))
	}
	// A record a save refused names the fields, never their values.
	var refused validation.Errors
	if errors.As(cause, &refused) {
		attributes = append(attributes, "causeFields", strings.Join(slices.Sorted(maps.Keys(refused)), ","))
	}
	// A Notion move's stop names what it was making, and why; no other
	// job's cause says its words (notion_import_left_out.go).
	return append(attributes, notionImportDiagnosticAttributes(cause)...)
}

func writeAPIError(
	event *core.RequestEvent,
	status int,
	code string,
	message string,
	operation string,
	stage string,
	causeCode string,
	cause error,
	sourcePosition string,
) error {
	reference := newDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference, operation, stage, causeCode, cause,
		sourcePosition,
	)
}

func writeAPIErrorWithReference(
	event *core.RequestEvent,
	status int,
	code string,
	message string,
	reference string,
	operation string,
	stage string,
	causeCode string,
	cause error,
	sourcePosition string,
) error {
	// A request its client gave up on failed because the client left (a
	// newer read of the page list retires the one under way): the request
	// log keeps it as the client's, 499 as proxies write it, not as a server
	// failure (2026-11-14: retired list reads showed there as causeless 500s).
	// One that failed while the server stops is the stopping server's, told
	// the server is away, its client still waiting (server_stopping.go); told
	// busy when the stop ended its write with none of it kept, so the app
	// sends it again (request_write_stop.go). One that failed because the
	// database stayed busy is told busy too (api_error_busy.go) — the rules
	// every failure's answer goes by, whatever its shape (failureAnswer). A
	// refusal for a reason is said as itself (api_error_refused_not_changed.go).
	status, code, message = refusalAnswer(event, status, code, message, cause)
	status, code, message = failureAnswer(event, status, code, message, cause)
	// A body sent as JSON that doesn't read is said so (api_error_unreadable_json.go).
	if status == http.StatusBadRequest && code != errorCodeInvalidJSON && apiBodyUnreadableJSON(event) {
		code, message = errorCodeInvalidJSON, notionInvalidJSONMessage
	} else {
		// An agent's request lacking what its operation needs is told so (api_error_missing.go).
		status, code, message = apiErrorSaysMissing(event, status, code, message)
	}
	logAPIDiagnostic(
		event,
		status,
		code,
		reference,
		operation,
		stage,
		causeCode,
		cause,
		sourcePosition,
	)
	body := errorBody{Code: code, Message: message, Reference: reference}
	body.Target, body.Fields = apiErrorDetails(event, status, code, message)
	body.Reason, body.Cause = apiErrorBecauseSaid(event)
	return event.JSON(status, errorResponse{Error: body})
}

func apiError(event *core.RequestEvent, status int, code string, message string) error {
	return writeAPIError(
		event,
		status,
		code,
		message,
		diagnosticOperation(event),
		"response",
		code,
		nil,
		diagnosticSourcePosition(1),
	)
}

// apiErrorFrom is apiError carrying what failed: the error writer reads the
// cause, so a database that stayed busy is told busy (api_error_busy.go), and
// the log line names the cause's class. A route answering 500 because of an
// error says it through this one (2026-11-18: 287 routes dropped theirs, so a
// busy database there was a causeless 500).
func apiErrorFrom(event *core.RequestEvent, status int, code string, message string, cause error) error {
	return writeAPIError(
		event,
		status,
		code,
		message,
		diagnosticOperation(event),
		"response",
		code,
		cause,
		diagnosticSourcePosition(1),
	)
}

func apiErrorAndFlush(event *core.RequestEvent, status int, code string, message string) error {
	if err := apiError(event, status, code, message); err != nil {
		return err
	}
	err := http.NewResponseController(event.Response).Flush()
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func apiErrorWithCause(
	event *core.RequestEvent,
	status int,
	code string,
	message string,
	operation string,
	stage string,
	causeCode string,
	cause error,
) error {
	return writeAPIError(
		event,
		status,
		code,
		message,
		operation,
		stage,
		causeCode,
		cause,
		diagnosticSourcePosition(1),
	)
}

// diagnosticCauseCode is the deepest error code a job's failure carries —
// an error whose words are a LEAFEN_ code, as errOrganizationStorageStateInvalid —
// other than the failure's own: what stopped it, never the words of any
// other error, which may hold what people wrote (2026-11-07: a move's failure
// logged only "*app.workspaceImportFailureError").
func diagnosticCauseCode(cause error, outer string) string {
	if cause == nil {
		return ""
	}
	inner := ""
	switch wrapped := cause.(type) {
	case interface{ Unwrap() []error }:
		for _, next := range wrapped.Unwrap() {
			if inner = diagnosticCauseCode(next, outer); inner != "" {
				break
			}
		}
	case interface{ Unwrap() error }:
		inner = diagnosticCauseCode(wrapped.Unwrap(), outer)
	}
	if inner != "" {
		return inner
	}
	text := cause.Error()
	if text != outer && strings.HasPrefix(text, "LEAFEN_") && strings.Trim(text, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
		return text
	}
	return ""
}
