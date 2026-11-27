package organization

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/elimity-com/scim/optional"
	"github.com/elimity-com/scim/schema"
	"github.com/pocketbase/pocketbase/core"
)

const scimRequestPathPrefix = leafen.APIRoot + "/scim"

// scimRefusal keeps a SCIM error's type and status and says which value or
// state refused it; the library's own detail names no field.
func scimRefusal(kind scimErrors.ScimError, detail string) scimErrors.ScimError {
	kind.Detail = detail
	return kind
}

func scimUnverifiedDomainDetail(email string) string {
	domain := email
	if at := strings.LastIndexByte(email, '@'); at >= 0 {
		domain = email[at+1:]
	}
	return "The domain " + domain + " isn't a verified domain of this organization; verify it under Settings, Identity, Domains first."
}

type scimOperationRequestState struct {
	errorCode  string
	errorStage string
	present    bool
	retryAfter string
	turn       scimOperationTurn
}

type scimOperationRequestStateKey struct{}

func serveEnterpriseSCIM(event *core.RequestEvent, operations *scimOperationRunner) error {
	token, err := authenticateSCIMToken(event.App, leafen.BearerToken(event.Request))
	if err != nil {
		return writeSCIMAuthenticationFailure(event, err)
	}
	reference := leafen.NewDiagnosticReference()
	server, err := newEnterpriseSCIMServer(event.App, token.OrganizationID, reference)
	if err != nil {
		return writeSCIMFailure(event, http.StatusServiceUnavailable, leafen.ErrorCodeSCIMUnavailable, err)
	}
	request := event.Request.Clone(event.Request.Context())
	state := &scimOperationRequestState{}
	request = request.WithContext(context.WithValue(
		request.Context(), scimOperationRequestStateKey{}, state,
	))
	request.URL.Path = strings.TrimPrefix(request.URL.Path, scimRequestPathPrefix)
	if !strings.HasPrefix(request.URL.Path, "/v2") {
		request.URL.Path = "/v2/" + strings.TrimPrefix(request.URL.Path, "/")
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	err = copySCIMResponse(event, recorder, reference, state)
	if state.present && state.turn.HasMore {
		operations.notify()
	}
	return err
}

func recordSCIMOperationTurn(request *http.Request, turn scimOperationTurn) {
	recordSCIMOperationResponse(request, turn, "", "", "")
}

func recordSCIMOperationConflict(request *http.Request, turn scimOperationTurn) {
	recordSCIMOperationResponse(
		request, turn, leafen.ErrorCodeSCIMOperationConflict, "operation-conflict", "1",
	)
}

func recordSCIMOperationResponse(
	request *http.Request,
	turn scimOperationTurn,
	errorCode string,
	errorStage string,
	retryAfter string,
) {
	if request == nil {
		return
	}
	state, _ := request.Context().Value(scimOperationRequestStateKey{}).(*scimOperationRequestState)
	if state == nil {
		return
	}
	state.present = true
	state.turn = turn
	state.errorCode = errorCode
	state.errorStage = errorStage
	state.retryAfter = retryAfter
}

func newEnterpriseSCIMServer(app core.App, organizationID string, reference string) (scim.Server, error) {
	configuration := &scim.ServiceProviderConfig{
		AuthenticationSchemes: []scim.AuthenticationScheme{{
			Type: scim.AuthenticationTypeOauthBearerToken, Name: "Bearer token",
			Description: "Leafen organization-scoped SCIM token", Primary: true,
		}},
		MaxResults: leafen.SCIMMaxPageSize, SupportFiltering: true, SupportPatch: true,
	}
	return scim.NewServer(&scim.ServerArgs{
		ServiceProviderConfig: configuration,
		ResourceTypes: []scim.ResourceType{
			{
				ID: optional.NewString("User"), Name: "User", Endpoint: "/Users",
				Description: optional.NewString("Leafen organization member"),
				Schema:      schema.CoreUserSchema(),
				Handler: &enterpriseSCIMUserHandler{
					app: app, organizationID: organizationID, diagnosticReference: reference,
				},
			},
			{
				ID: optional.NewString("Group"), Name: "Group", Endpoint: "/Groups",
				Description: optional.NewString("Leafen organization group"),
				Schema:      schema.CoreGroupSchema(),
				Handler: &enterpriseSCIMGroupHandler{
					app: app, organizationID: organizationID, diagnosticReference: reference,
				},
			},
		},
	})
}

func copySCIMResponse(
	event *core.RequestEvent,
	recorder *httptest.ResponseRecorder,
	reference string,
	operation *scimOperationRequestState,
) error {
	for key, values := range recorder.Header() {
		for _, value := range values {
			event.Response.Header().Add(key, value)
		}
	}
	status := recorder.Code
	if operation.present {
		event.Response.Header().Set(leafen.SCIMOperationIDHeader, operation.turn.OperationID)
		event.Response.Header().Set(leafen.SCIMOperationStatusHeader, operation.turn.Status)
		event.Response.Header().Set(leafen.SCIMOperationCursorHeader, strconv.Itoa(operation.turn.Cursor))
		event.Response.Header().Set(leafen.SCIMOperationTotalHeader, strconv.Itoa(operation.turn.Total))
		if operation.retryAfter != "" {
			event.Response.Header().Set("Retry-After", operation.retryAfter)
		} else if operation.turn.HasMore {
			event.Response.Header().Set("Retry-After", "0")
		}
	}
	if status >= http.StatusBadRequest {
		code := leafen.ErrorCodeSCIMRequestFailed
		stage := "protocol-response"
		if operation.errorCode != "" {
			code = operation.errorCode
		}
		if operation.errorStage != "" {
			stage = operation.errorStage
		}
		event.Response.Header().Set("X-Leafen-Diagnostic-Reference", reference)
		event.Response.Header().Set("X-Leafen-Error-Code", code)
		leafen.LogAPIDiagnostic(
			event, status, code, reference, leafen.DiagnosticOperation(event),
			stage, code, nil, leafen.DiagnosticSourcePosition(1),
		)
	}
	event.Response.WriteHeader(status)
	_, err := event.Response.Write(recorder.Body.Bytes())
	return err
}

func writeSCIMAuthenticationFailure(event *core.RequestEvent, cause error) error {
	return writeSCIMFailure(event, http.StatusUnauthorized, leafen.ErrorCodeSCIMTokenInvalid, cause)
}

func writeSCIMFailure(event *core.RequestEvent, status int, code string, cause error) error {
	reference := leafen.NewDiagnosticReference()
	payload, err := json.Marshal(scimErrors.ScimError{Status: status})
	if err != nil {
		return err
	}
	leafen.LogAPIDiagnostic(
		event, status, code, reference, leafen.DiagnosticOperation(event), "protocol-authentication",
		code, cause, leafen.DiagnosticSourcePosition(1),
	)
	event.Response.Header().Set("Content-Type", "application/scim+json")
	event.Response.Header().Set("X-Leafen-Diagnostic-Reference", reference)
	event.Response.Header().Set("X-Leafen-Error-Code", code)
	event.Response.WriteHeader(status)
	_, err = event.Response.Write(payload)
	return err
}
