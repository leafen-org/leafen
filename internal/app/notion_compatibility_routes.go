package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAPIRoot                    = "/v1"
	notionAPIUsersPath               = notionAPIRoot + "/users"
	notionAPIUsersMePath             = notionAPIRoot + "/users/me"
	notionAPIOAuthTokenPath          = notionAPIRoot + "/oauth/token"
	notionAPIOAuthRevokePath         = notionAPIRoot + "/oauth/revoke"
	notionAPIOAuthIntrospectPath     = notionAPIRoot + "/oauth/introspect"
	notionErrorInvalidRequestURL     = "invalid_request_url"
	notionErrorUnauthorized          = "unauthorized"
	notionRequestIDHeader            = "X-Notion-Request-Id"
	notionInvalidRequestURLMessage   = "Invalid request URL."
	notionInvalidAuthorizationFormat = "The Authorization header takes the form \"Bearer <token>\"."
	notionInvalidToken               = "API token is invalid."
)

var notionCompatibilityMethods = [...]string{
	http.MethodConnect,
	http.MethodDelete,
	http.MethodGet,
	http.MethodHead,
	http.MethodOptions,
	http.MethodPatch,
	http.MethodPost,
	http.MethodPut,
	http.MethodTrace,
}

type notionAPIErrorBody struct {
	Object    string `json:"object"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	// AdditionalData is what some errors add, as a rate limit its reason
	// and its wait.
	AdditionalData map[string]any `json:"additional_data,omitempty"`
}

func registerNotionCompatibilityRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.POST(
		apiRoot+"/notion/oauth/authorizations",
		issueNotionOAuthAuthorizationCode,
	).Bind(apis.BodyLimit(128 << 10))
	event.Router.POST(
		apiRoot+"/notion/oauth/requests",
		readNotionOAuthRequest,
	).Bind(apis.BodyLimit(16 << 10))
	// Its body is read as every public API body is (refuseNotionBodyByType).
	event.Router.POST(notionMeetingNotesQueryPath, func(request *core.RequestEvent) error {
		return serveNotionAnswer(request, func() error { return serveUnsupportedNotionMeetingNotesQuery(request) })
	})
	for _, method := range notionCompatibilityMethods {
		serve := func(request *core.RequestEvent) error {
			// Every answer is in Notion's envelope (serveNotionAnswer); a
			// body's type and size are refused before a previous version's
			// body is renamed.
			return serveNotionAnswer(request, func() error {
				if refused, err := refuseNotionBodyByType(request); refused {
					return err
				}
				return serveNotionAtPreviousVersion(request, func() error { return serveNotionCompatibility(request, ygo) })
			})
		}
		event.Router.Route(method, notionAPIRoot, serve)
		event.Router.Route(method, notionAPIRoot+"/{path...}", serve)
	}
}

func serveNotionCompatibility(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	if refused, err := refuseNotionUnreadableJSON(event); refused {
		return err
	}
	if event.Request.Method == http.MethodGet && event.Request.URL.Path == notionAPIOAuthAuthorizePath {
		return beginNotionOAuthAuthorization(event)
	}
	if event.Request.Method == http.MethodPost {
		switch event.Request.URL.Path {
		case notionAPIOAuthTokenPath:
			return exchangeNotionOAuthToken(event)
		case notionAPIOAuthRevokePath:
			return revokeNotionOAuthToken(event)
		case notionAPIOAuthIntrospectPath:
			return introspectNotionOAuthToken(event)
		}
	}
	if limited, err := admitNotionConnectionRequest(event); limited {
		return err
	}
	if refused, err := refuseNotionRequest(event); refused {
		return err
	}
	if event.Request.Method == http.MethodGet && event.Request.URL.Path == notionAPIUsersMePath {
		return serveNotionOAuthUsersMe(event)
	}
	if event.Request.Method == http.MethodGet && event.Request.URL.Path == notionAPIUsersPath {
		return serveNotionUserList(event)
	}
	if event.Request.Method == http.MethodGet && strings.HasPrefix(event.Request.URL.Path, notionAPIUsersPath+"/") {
		userID := strings.TrimPrefix(event.Request.URL.Path, notionAPIUsersPath+"/")
		if userID != "" && !strings.Contains(userID, "/") {
			return serveNotionUserRetrieve(event, userID)
		}
	}
	if event.Request.Method == http.MethodPost && event.Request.URL.Path == notionAPISearchPath {
		return serveNotionSearch(event)
	}
	if event.Request.Method == http.MethodGet && strings.HasPrefix(event.Request.URL.Path, notionAPIAsyncTasksPath+"/") {
		taskID := strings.TrimPrefix(event.Request.URL.Path, notionAPIAsyncTasksPath+"/")
		if taskID != "" && !strings.Contains(taskID, "/") {
			return serveNotionAsyncTaskRetrieve(event, taskID)
		}
	}
	if handled, err := serveNotionCustomEmojiCompatibility(event); handled {
		return err
	}
	if handled, err := serveNotionPageCompatibility(event, ygo); handled {
		return err
	}
	if handled, err := serveNotionDataSourceCompatibility(event, ygo); handled {
		return err
	}
	if handled, err := serveNotionDatabaseCompatibility(event, ygo); handled {
		return err
	}
	if handled, err := serveNotionBlockCompatibility(event, ygo); handled {
		return err
	}
	if handled, err := serveNotionViewCompatibility(event, ygo); handled {
		return err
	}
	if handled, err := serveNotionCommentCompatibility(event); handled {
		return err
	}
	if handled, err := serveNotionFileUploadCompatibility(event); handled {
		return err
	}
	if handled, err := serveUnsupportedNotionPublicCapability(event); handled {
		return err
	}
	return notionCompatibilityError(
		event,
		http.StatusBadRequest,
		notionErrorInvalidRequestURL,
		notionInvalidRequestURLMessage,
	)
}

// notionUnauthorized answers a request without a usable token as Notion
// does (seen 2026-10-17): a header that isn't "Bearer <token>" gets the format
// message, a token that is wrong, revoked or no longer reaches gets "API token
// is invalid." with the reason token_invalid.
func notionUnauthorized(event *core.RequestEvent) error {
	if bearerToken(event.Request) == "" {
		return notionCompatibilityError(event, http.StatusUnauthorized, notionErrorUnauthorized, notionInvalidAuthorizationFormat)
	}
	return notionCompatibilityErrorWithData(event, http.StatusUnauthorized, notionErrorUnauthorized, notionInvalidToken,
		map[string]any{"reason": "token_invalid"}, nil)
}

func notionCompatibilityError(
	event *core.RequestEvent,
	status int,
	code string,
	message string,
) error {
	return notionCompatibilityErrorWithData(event, status, code, message, nil, nil)
}

// notionConflictWhileSaving is Notion's answer to a write that met another
// (seen 2026-11-14: of twelve appends to one page at once, ten were answered
// 409 conflict_error with these words and no Retry-After).
const notionConflictWhileSaving = "Another change was being saved at the same moment; try again."

// notionRequestNotCompleted answers a request the server couldn't finish
// for cause, which is logged with it.
func notionRequestNotCompleted(event *core.RequestEvent, cause error) error {
	return notionCompatibilityErrorWithData(
		event, http.StatusInternalServerError, "internal_server_error", "The request failed on the server.", nil, cause,
	)
}

// notionCompatibilityErrorWithData answers with a Notion error that carries
// additional data. A write that waited out its time for the database
// (sqlite_write_gate.go), whatever the route would have said, is answered as
// Notion answers a write that met another: trying it again goes through once
// the other is done (2026-11-14: POST /v1/pages and PATCH
// /v1/pages/{id}/markdown answered 500 after ten seconds).
func notionCompatibilityErrorWithData(
	event *core.RequestEvent,
	status int,
	code string,
	message string,
	data map[string]any,
	cause error,
) error {
	// A refusal for its reason that a route answers as its failure says
	// itself, as Leafen's own error writer says one (refusalAnswer).
	if words, refused := refusalWords(cause); refused && status >= http.StatusInternalServerError {
		status, code, message, data = http.StatusBadRequest, "validation_error", words, nil
	}
	// So is a page an operation holds for a while (notion_held_page.go).
	if isSQLiteBusyError(cause) || notionHeldAnswer(event, status, code, cause) {
		status, code, message, data = http.StatusConflict, "conflict_error", notionConflictWhileSaving, nil
	}
	status, code, message, data = notionNotFoundSaid(event, status, code, message, data)
	requestID := notionRequestID(event)
	// JSON in UTF-8 wherever it is answered, a limit before the route too.
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
	return event.JSON(status, notionAPIErrorBody{
		Object:         "error",
		Status:         status,
		Code:           code,
		Message:        message,
		RequestID:      requestID,
		AdditionalData: data,
	})
}
