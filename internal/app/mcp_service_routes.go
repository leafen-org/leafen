package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A person's services: look at an address before connecting it, list them,
// connect one by its address (with the headers the person gives a service
// that opens no sign-in page), sign in again, read it again, disconnect.

const mcpServiceMaxBodyBytes = 16 << 10

type mcpServiceAnswer struct {
	Service      MCPService `json:"service"`
	AuthorizeURL string     `json:"authorizeUrl,omitempty"`
}

func registerMCPServiceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/mcp-services", listMCPServicesRoute)
	event.Router.GET(apiRoot+"/mcp-services/{serviceId}", readMCPServiceRoute)
	event.Router.POST(apiRoot+"/mcp-services", addMCPServiceRoute).Bind(apis.BodyLimit(mcpServiceMaxBodyBytes))
	event.Router.POST(apiRoot+"/mcp-services/probe", probeMCPServiceRoute).Bind(apis.BodyLimit(mcpServiceMaxBodyBytes))
	event.Router.POST(apiRoot+"/mcp-services/{serviceId}/sign-in", signInMCPServiceRoute)
	event.Router.POST(apiRoot+"/mcp-services/{serviceId}/check", checkMCPServiceRoute)
	event.Router.DELETE(apiRoot+"/mcp-services/{serviceId}", deleteMCPServiceRoute)
	event.Router.GET(apiRoot+mcpServiceCallbackPath, mcpServiceCallbackRoute)
	event.Router.GET(apiRoot+mcpServiceClientMetadataPath, mcpServiceClientMetadataRoute)
	event.Router.POST(apiRoot+"/mcp-services/{serviceId}/return", setMCPServiceReturnRoute)
	event.Router.GET(apiRoot+"/mcp-services/oauth/callback-address", mcpServiceCallbackAddressRoute)
	registerMCPServiceToolChoiceRoutes(event)
	registerMCPConnectionServiceRoutes(event)
}

// mcpServiceAddition is what the person's form gives: the address, the name
// they give the connection, how a service without a sign-in page signs in
// (Token alone is a bearer token) with their own headers, and for one with
// a sign-in page the scopes kept and their own OAuth client.
type mcpServiceAddition struct {
	URL     string               `json:"url"`
	Name    string               `json:"name"`
	Token   string               `json:"token"`
	Auth    mcpServiceHeaderAuth `json:"auth"`
	Headers []mcpServiceHeader   `json:"headers"`
	Scopes  []string             `json:"scopes"`
	Client  mcpServiceClient     `json:"-"`
}

// AddMCPService connects the person to the service at the address: read at
// once when it needs no sign-in or takes the headers given, or a client of
// its authorization server — the person's own client given, or this server —
// and answered with the address of its sign-in page. The same address again
// connects it anew.
func AddMCPService(ctx context.Context, app core.App, userID, origin string, given mcpServiceAddition) (mcpServiceAnswer, error) {
	answer := mcpServiceAnswer{}
	serviceURL, err := normalizeMCPServiceURL(given.URL)
	if err != nil {
		return answer, err
	}
	if err := refuseMCPServiceByPolicy(app, userID, serviceURL); err != nil {
		return answer, err
	}
	if given.Auth.Type == "" && given.Auth.Token == "" {
		given.Auth.Token = given.Token
	}
	headers, err := mcpServiceHeadersOf(given.Auth, given.Headers)
	if err != nil {
		return answer, err
	}
	name, err := normalizeMCPServiceName(given.Name)
	if err != nil {
		return answer, err
	}
	client := mcpServiceClient{ID: strings.TrimSpace(given.Client.ID), Secret: strings.TrimSpace(given.Client.Secret)}
	if !client.valid() || len(given.Scopes) > 100 {
		return answer, errMCPServiceInvalid
	}
	existingID := ""
	err = app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_mcp_services}} WHERE [[user_id]] = {:userId} AND [[url]] = {:url}`).
		Bind(dbx.Params{"userId": userID, "url": serviceURL}).Row(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return answer, err
	}
	reading, challenges, readErr := readMCPService(ctx, serviceURL, headers)
	fields := dbx.Params{"given_name": name, "auth": mcpServiceAuthNone, "status": mcpServiceStatusSigningIn, "failure": "", "issuer": "",
		"authorization_endpoint": "", "token_endpoint": "", "revocation_endpoint": "", "resource": "", "scope": "", "client_id": "",
		"client_secret": "", "client_auth": 0, "access": "", "headers": "", "refresh": "", "expires_at": 0, "state_hash": "",
		"verifier": "", "redirect_uri": "", "state_expires_at": 0, "connected_at": 0}
	switch {
	case readErr == nil:
		if len(headers) > 0 {
			if fields["headers"], err = sealMCPServiceHeaders(app, headers); err != nil {
				return answer, err
			}
			fields["auth"] = mcpServiceAuthToken
		}
	case errors.Is(readErr, errMCPServiceSignIn) && len(headers) == 0:
		redirect := origin + apiRoot + mcpServiceCallbackPath
		// The metadata document is read by the service's server, so only an
		// https address can be one, and it comes after registering
		// (mcp_service_oauth_register.go).
		document := ""
		if strings.HasPrefix(origin, "https://") {
			document = origin + apiRoot + mcpServiceClientMetadataPath
		}
		plan, err := discoverMCPServiceOAuth(ctx, serviceURL, challenges, redirect, client, document, given.Scopes)
		if err != nil {
			return answer, err
		}
		secret := ""
		if plan.ClientSecret != "" {
			if secret, err = encryptAdministratorSetting(app, cipherMCPService, plan.ClientSecret); err != nil {
				return answer, err
			}
		}
		fields["auth"], fields["issuer"], fields["authorization_endpoint"], fields["token_endpoint"] = mcpServiceAuthOAuth,
			plan.Issuer, plan.AuthorizationEndpoint, plan.TokenEndpoint
		fields["revocation_endpoint"], fields["resource"], fields["scope"], fields["client_id"], fields["client_secret"],
			fields["client_auth"], fields["redirect_uri"] = plan.RevocationEndpoint, plan.Resource, plan.Scope, plan.ClientID, secret,
			int(plan.ClientAuth), redirect
		fields["client_kind"], fields["pending_plan"] = plan.ClientKind, ""
	default:
		return answer, readErr
	}
	now := time.Now().UnixMilli()
	fields["updated_at"] = now
	serviceID := existingID
	if serviceID == "" {
		serviceID = core.GenerateDefaultRandomId()
		fields["id"], fields["user_id"], fields["url"], fields["created_at"] = serviceID, userID, serviceURL, now
		_, err = app.DB().Insert(mcpServicesTable, fields).Execute()
	} else {
		_, err = app.DB().Update(mcpServicesTable, fields, dbx.HashExp{"id": serviceID}).Execute()
	}
	if err != nil {
		return answer, err
	}
	if readErr == nil {
		if err := saveMCPServiceCheck(app, serviceID, reading, nil); err != nil {
			return answer, err
		}
	} else if answer.AuthorizeURL, err = startMCPServiceSignIn(app, serviceID); err != nil {
		return answer, err
	}
	stored, err := loadMCPService(app, userID, serviceID)
	answer.Service = stored.view()
	return answer, err
}

// CheckMCPService reads the service again with the person's sign-in.
func CheckMCPService(ctx context.Context, app core.App, userID, serviceID string) (MCPService, error) {
	stored, err := loadMCPService(app, userID, serviceID)
	if err != nil {
		return MCPService{}, err
	}
	reading, err := readMCPServiceAs(ctx, app, stored)
	if err != nil {
		app.Logger().Warn("leafen service check failed", "service", stored.ID, "error", err)
	}
	if err := saveMCPServiceCheck(app, stored.ID, reading, err); err != nil {
		return MCPService{}, err
	}
	readErr := err
	stored, err = loadMCPService(app, userID, serviceID)
	if err == nil && (errors.Is(readErr, errMCPServiceTooMany) || errors.Is(readErr, errMCPServicePolicyBlocked) ||
		errors.Is(readErr, errMCPServiceSignInUnavailable)) {
		// Too many tools to keep, a workspace's policy, or a sign-in server
		// that couldn't renew the sign-in for now, is said, not only shown
		// as unreachable.
		err = readErr
	}
	return stored.view(), err
}

func mcpServiceRequestUser(event *core.RequestEvent) (string, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return "", false
	}
	return user.Id, true
}

func mcpServiceFailure(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errMCPServiceNotFound):
		return apiError(event, http.StatusNotFound, errorCodeMCPServiceNotFound, "Service not found.")
	case errors.Is(err, errMCPServiceInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeMCPServiceInvalid, "Check the service's address: an https address of its MCP server.")
	case errors.Is(err, errOutboundUnsafeDestination):
		return apiError(event, http.StatusBadRequest, errorCodeMCPServiceUnsafeAddress, "The address reaches a place the server doesn't connect to.")
	case errors.Is(err, errMCPServiceTooMany):
		return apiError(event, http.StatusUnprocessableEntity, errorCodeMCPServiceTooManyTools, mcpServiceFailureText(err))
	case errors.Is(err, errMCPServicePolicyBlocked):
		return apiError(event, http.StatusForbidden, errorCodeMCPServicePolicyBlocked, mcpServicePolicyBlockedText)
	case errors.Is(err, errMCPServiceSignIn):
		return apiError(event, http.StatusBadRequest, errorCodeMCPServiceTokenRefused, "The service refused the token.")
	case errors.Is(err, errMCPServiceNoRegistration):
		return apiError(event, http.StatusUnprocessableEntity, errorCodeMCPServiceClientNeeded, mcpServiceClientNeedText(err, ""))
	case errors.Is(err, errMCPServiceAddressUnreachable):
		return apiError(event, http.StatusUnprocessableEntity, errorCodeMCPServiceAddressUnreachable,
			mcpServiceClientNeedText(err, mcpServiceRequestLanguage(event)))
	case errors.Is(err, errMCPServiceRegistrationRefused):
		event.App.Logger().Info("leafen service refused registration", "error", err)
		return apiError(event, http.StatusUnprocessableEntity, errorCodeMCPServiceRegistrationRefused, mcpServiceClientNeedText(err, ""))
	case errors.Is(err, errMCPServiceNoPKCE):
		return apiError(event, http.StatusUnprocessableEntity, errorCodeMCPServiceUnsafeSignIn, "The service's login isn't one Leafen can use safely (no PKCE).")
	case errors.Is(err, errMCPServiceSignInUnavailable):
		event.App.Logger().Warn("leafen service sign-in server unavailable", "error", err)
		return apiError(event, http.StatusBadGateway, errorCodeMCPServiceSignInUnavailable, mcpServiceSignInUnavailableText)
	case errors.Is(err, errMCPServiceFailed):
		event.App.Logger().Warn("leafen service connection failed", "error", err)
		return apiError(event, http.StatusBadGateway, errorCodeMCPServiceFailed, "The service didn't answer as an MCP server, or its login couldn't start.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeMCPServiceFailed, "Couldn't connect the service.",
		"mcp_service", "store", errorCodeMCPServiceFailed, err)
}

func listMCPServicesRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	services, next, err := ListMCPServices(event.App, userID, event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	// A client of the person's own after an address change (mcp_service_address_change.go).
	if origin, err := outwardServerOrigin(event.Request); err == nil {
		for index := range services {
			if stored, err := loadMCPService(event.App, userID, services[index].ID); err == nil {
				services[index] = stored.withReturn(event.App, services[index], origin)
			}
		}
	}
	return event.JSON(http.StatusOK, map[string]any{"services": services, "nextCursor": next})
}

func readMCPServiceRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	stored, err := loadMCPService(event.App, userID, event.Request.PathValue("serviceId"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, map[string]any{"service": stored.view()})
}

func addMCPServiceRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	input := struct {
		mcpServiceAddition
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}{}
	if event.BindBody(&input) != nil {
		return mcpServiceFailure(event, errMCPServiceInvalid)
	}
	if strings.TrimSpace(input.URL) == "" {
		return refuseMissingInput(event, errorCodeMCPServiceInvalid, "url", "the https address of the service's MCP server.")
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	input.Client = mcpServiceClient{ID: input.ClientID, Secret: input.ClientSecret}
	answer, err := AddMCPService(event.Request.Context(), event.App, userID, origin, input.mcpServiceAddition)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, answer)
}

// mcpServiceClientMetadataRoute answers this server's client ID metadata
// document to the service's authorization server, which reads it unsigned.
func mcpServiceClientMetadataRoute(event *core.RequestEvent) error {
	// Read at an earlier address, the document is that address's
	// (answeredAtOrigin).
	origin, err := answeredAtOrigin(event.Request)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	event.Response.Header().Set("Cache-Control", "public, max-age=3600")
	return event.JSON(http.StatusOK, mcpServiceClientMetadata(origin))
}

// mcpServiceCallbackAddressRoute tells the person the address a client they
// make on a service for Leafen sends them back to.
func mcpServiceCallbackAddressRoute(event *core.RequestEvent) error {
	if _, ok := mcpServiceRequestUser(event); !ok {
		return nil
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, map[string]string{"callbackUrl": origin + apiRoot + mcpServiceCallbackPath})
}

func signInMCPServiceRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	stored, err := loadMCPService(event.App, userID, event.Request.PathValue("serviceId"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	if stored.Auth != mcpServiceAuthOAuth {
		return apiError(event, http.StatusConflict, errorCodeMCPServiceTokenNeeded, "This service has no login page; connect it again with its token.")
	}
	// At an address the service doesn't have yet (mcp_service_address_change.go).
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	if err := prepareMCPServiceSignIn(event.Request.Context(), event.App, stored, origin); err != nil {
		return mcpServiceFailure(event, err)
	}
	address, err := startMCPServiceSignIn(event.App, stored.ID)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, mcpServiceAnswer{Service: stored.view(), AuthorizeURL: address})
}

func checkMCPServiceRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	service, err := CheckMCPService(event.Request.Context(), event.App, userID, event.Request.PathValue("serviceId"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, mcpServiceAnswer{Service: service})
}

func deleteMCPServiceRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	if err := DeleteMCPService(event.App, userID, event.Request.PathValue("serviceId")); err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.NoContent(http.StatusNoContent)
}

// mcpServiceCallbackCallWords are what a call to the address a service's
// sign-in comes back to hears.
const mcpServiceCallbackCallWords = "Check state: no login to an external service Leafen started is waiting with it. " +
	"This is where the service sends the person's browser back after they log in; POST /mcp-services/{serviceId}/sign-in " +
	"starts one and answers the authorizeUrl they open."

// mcpServiceCallbackRoute is where a service sends the person back: the
// sign-in completes and the page says so (signInCallbackPage).
func mcpServiceCallbackRoute(event *core.RequestEvent) error {
	query := event.Request.URL.Query()
	stored, err := CompleteMCPServiceSignIn(event.Request.Context(), event.App, query.Get("state"), query.Get("code"), query.Get("iss"))
	page := signInCallbackPage{
		Done: [2]string{"Connected.", "연결했습니다."},
		Failed: [2]string{"Couldn’t connect: the login expired, was refused or failed. Try again from Leafen.",
			"연결하지 못했습니다. 로그인이 만료됐거나 거부됐거나 실패했습니다. 리펜에서 다시 시도하세요."},
		Result: map[string]any{"type": "leafen:mcp-service", "service": stored.ID, "ok": err == nil},
		Back:   "/settings?section=integrations",
		Code:   errorCodeMCPServiceInvalid, Call: mcpServiceCallbackCallWords,
	}
	if err != nil {
		event.App.Logger().Warn("leafen service sign-in failed", "service", stored.ID, "error", err)
	}
	// The token endpoint down for now is said here and to the window waiting
	// for the sign-in (mcp_service_sign_in_server.go).
	if errors.Is(err, errMCPServiceSignInUnavailable) {
		page.Failed = [2]string{"Couldn’t connect: the service’s login server isn’t answering right now. Try again from Leafen in a while.",
			"연결하지 못했습니다. 서비스의 로그인 서버가 지금 답하지 않습니다. 잠시 뒤 리펜에서 다시 시도하세요."}
		page.Result["code"] = errorCodeMCPServiceSignInUnavailable
	}
	return page.write(event, err == nil)
}
