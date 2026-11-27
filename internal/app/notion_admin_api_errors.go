package app

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminAPIVersion           = "2026-06-01"
	notionAdminErrorValidation      = "validation_error"
	notionAdminErrorUnauthorized    = "unauthorized"
	notionAdminErrorMissingScope    = "missing_scope"
	notionAdminErrorForbidden       = "forbidden"
	notionAdminErrorNotFound        = "not_found"
	notionAdminErrorRateLimited     = "rate_limited"
	notionAdminErrorInternal        = "internal_server_error"
	notionAdminErrorConflict        = "conflict_error"
	notionAdminUnauthorizedMessage  = "Unauthorized."
	notionAdminValidationMessage    = "The request is invalid."
	notionAdminMissingScopeMessage  = "The token's scopes don't cover this request."
	notionAdminForbiddenMessage     = "Forbidden."
	notionAdminNotFoundMessage      = "Not found."
	notionAdminRateLimitedMessage   = "Rate limited."
	notionAdminInternalErrorMessage = "An unexpected error occurred."
)

// notionAdminInvalidError is why a request is refused: what in it is wrong
// and what is allowed, as Notion's own validation errors say it
// (2026-10-16: every refusal said only "The request is invalid.").
type notionAdminInvalidError struct {
	sentinel error
	reason   string
}

func (invalid *notionAdminInvalidError) Error() string { return invalid.reason }
func (invalid *notionAdminInvalidError) Unwrap() error { return invalid.sentinel }

// notionAdminInvalid is the sentinel a check refuses with, carrying why.
func notionAdminInvalid(sentinel error, reason string) error {
	return &notionAdminInvalidError{sentinel: sentinel, reason: reason}
}

// notionAdminValidationReason is what a refused request is told: the reason
// its error carries, what a body that doesn't decode says, or else the
// plain refusal.
func notionAdminValidationReason(err error) string {
	var invalid *notionAdminInvalidError
	if errors.As(err, &invalid) {
		return invalid.reason
	}
	var typed *json.UnmarshalTypeError
	if errors.As(err, &typed) && typed.Field != "" {
		return "body." + typed.Field + " should be " + notionAdminJSONKind(typed.Type.Kind().String()) + "."
	}
	// PocketBase's BindBody decodes with encoding/json/v2.
	var semantic *jsonv2.SemanticError
	if errors.As(err, &semantic) {
		field := strings.Join(slices.Collect(semantic.JSONPointer.Tokens()), ".")
		switch {
		case errors.Is(semantic.Err, jsonv2.ErrUnknownName):
			return "body has a field this request doesn't take: " + strconv.Quote(field) + "."
		case semantic.GoType != nil && field != "":
			goType := semantic.GoType
			for goType.Kind() == reflect.Pointer {
				goType = goType.Elem()
			}
			return "body." + field + " should be " + notionAdminJSONKind(goType.Kind().String()) + "."
		}
	}
	var syntax *json.SyntaxError
	var syntactic *jsontext.SyntacticError
	if errors.As(err, &syntax) || errors.As(err, &syntactic) {
		return "The body isn't valid JSON."
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "The body is empty or cut short."
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return "The body is larger than this request takes."
	}
	if err != nil && err.Error() == "multiple JSON values" {
		return "The body holds more than one JSON value."
	}
	if err != nil && strings.Contains(err.Error(), "json: unknown field ") {
		return "body has a field this request doesn't take: " + strings.TrimPrefix(err.Error()[strings.Index(err.Error(), "json: unknown field "):], "json: unknown field ") + "."
	}
	return notionAdminValidationMessage
}

// notionAdminRefusalReason is the reason a refusal other than a validation
// error carries, or fallback when it carries none.
func notionAdminRefusalReason(err error, fallback string) string {
	invalid := &notionAdminInvalidError{}
	if errors.As(err, &invalid) {
		return invalid.reason
	}
	return fallback
}

// notionAdminScopeChangedReason is why a write is refused when the token
// lost its scope or workspace access between the first check and the write.
const notionAdminScopeChangedReason = "The token lost the scope or workspace access this request needs while it ran."

func notionAdminJSONKind(kind string) string {
	switch {
	case strings.HasPrefix(kind, "int"), strings.HasPrefix(kind, "uint"), strings.HasPrefix(kind, "float"):
		return "a number"
	case kind == "string":
		return "a string"
	case kind == "bool":
		return "a boolean"
	case kind == "slice", kind == "array":
		return "an array"
	case kind == "map", kind == "struct", kind == "ptr":
		return "an object"
	}
	return "a " + kind
}

type notionAdminAPIErrorBody struct {
	Type    string `json:"type"`
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type notionAdminPublicAPIErrorBody struct {
	Object  string `json:"object"`
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type notionAdminAuthorizationFailure struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

// notionAdminBusy answers a write that waited out its time for the database
// as the public API answers it (notionCompatibilityErrorWithData): 409, its
// conflict's words, so a client tries it again, not 500.
func notionAdminBusy(status int, code, message string, cause error) (int, string, string) {
	if isSQLiteBusyError(cause) {
		return http.StatusConflict, notionAdminErrorConflict, notionConflictWhileSaving
	}
	return status, code, message
}

func notionAdminAPIError(
	event *core.RequestEvent,
	status int,
	code, message string,
	cause error,
) error {
	status, code, message = notionAdminBusy(status, code, message, cause)
	requestID := notionRequestID(event)
	event.Response.Header().Set("Content-Type", notionJSONAnswerContentType)
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	logAPIDiagnostic(
		event,
		status,
		code,
		requestID,
		diagnosticOperation(event),
		"response",
		code,
		cause,
		diagnosticSourcePosition(1),
	)
	return event.JSON(status, notionAdminAPIErrorBody{
		Type: "error", Status: status, Code: code, Message: message,
	})
}

func notionAdminPublicAPIError(
	event *core.RequestEvent,
	status int,
	code, message string,
	cause error,
) error {
	status, code, message = notionAdminBusy(status, code, message, cause)
	requestID := notionRequestID(event)
	event.Response.Header().Set("Content-Type", notionJSONAnswerContentType)
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	logAPIDiagnostic(
		event,
		status,
		code,
		requestID,
		diagnosticOperation(event),
		"response",
		code,
		cause,
		diagnosticSourcePosition(1),
	)
	return event.JSON(status, notionAdminPublicAPIErrorBody{
		Object: "error", Status: status, Code: code, Message: message,
	})
}

func resolveNotionAdminAPIAuthorization(
	event *core.RequestEvent,
	secret string,
	capability string,
) (notionAdminIdentity, *notionAdminAuthorizationFailure) {
	identity, err := authenticateNotionAdminToken(
		event.Request.Context(), event.App, secret, time.Now().UTC(),
	)
	if err != nil {
		return notionAdminIdentity{}, &notionAdminAuthorizationFailure{
			Status: http.StatusUnauthorized, Code: notionAdminErrorUnauthorized,
			Message: notionAdminUnauthorizedMessage, Cause: err,
		}
	}
	if event.Request.Header.Get("Notion-Version") != notionAdminAPIVersion {
		return notionAdminIdentity{}, &notionAdminAuthorizationFailure{
			Status: http.StatusBadRequest, Code: notionAdminErrorValidation,
			Message: "The Notion-Version header must be " + notionAdminAPIVersion + ".",
		}
	}
	if !identity.HasCapability(capability) {
		return notionAdminIdentity{}, &notionAdminAuthorizationFailure{
			Status: http.StatusForbidden, Code: notionAdminErrorMissingScope,
			Message: notionAdminMissingScopeMessage + " It needs " + capability + ".",
		}
	}
	return identity, nil
}

func authorizeNotionAdminAPI(
	event *core.RequestEvent,
	secret string,
	capability string,
) (notionAdminIdentity, bool) {
	identity, failure := resolveNotionAdminAPIAuthorization(event, secret, capability)
	if failure != nil {
		_ = notionAdminAPIError(
			event, failure.Status, failure.Code, failure.Message, failure.Cause,
		)
		return notionAdminIdentity{}, false
	}
	return identity, true
}

func authorizeNotionAdminPublicAPI(
	event *core.RequestEvent,
	secret string,
	capability string,
) (notionAdminIdentity, bool) {
	identity, failure := resolveNotionAdminAPIAuthorization(event, secret, capability)
	if failure == nil {
		return identity, true
	}
	code := failure.Code
	message := failure.Message
	if failure.Status == http.StatusForbidden {
		code = notionErrorRestrictedResource
	}
	_ = notionAdminPublicAPIError(
		event, failure.Status, code, message, failure.Cause,
	)
	return notionAdminIdentity{}, false
}
