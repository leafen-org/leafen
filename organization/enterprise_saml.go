package organization

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/crewjam/saml"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerEnterpriseSAMLRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+leafen.SAMLManagementSnapshotPath, getSAMLManagementSnapshot)
	event.Router.PUT(leafen.APIRoot+"/organizations/{organizationId}/saml", updateSAMLConfiguration)
	event.Router.POST(leafen.APIRoot+"/organizations/{organizationId}/saml/return", setSAMLServiceReturn)
	event.Router.POST(leafen.APIRoot+"/organizations/{organizationId}/saml/domains", createSAMLDomain)
	event.Router.POST(leafen.APIRoot+"/organizations/{organizationId}/saml/domains/{domain}/verify", verifySAMLDomainRoute)
	event.Router.GET(leafen.APIRoot+"/auth/saml/{providerId}/metadata", serveSAMLMetadata)
	event.Router.GET(leafen.APIRoot+"/auth/saml/{providerId}/start", leafen.ReturnAuthProviderFailures(beginSAMLSignIn))
	event.Router.POST(leafen.APIRoot+"/auth/saml/acs", leafen.ReturnAuthProviderFailures(completeSAMLResponse)).
		Bind(apis.BodyLimit(leafen.SAMLMaxEncodedResponseBytes + 8192))
	event.Router.POST(leafen.APIRoot+"/auth/saml/exchange", exchangeSAMLCompletion)
	event.Router.POST(leafen.APIRoot+"/auth/saml/find", findSAMLOrganization)
}

func updateSAMLConfiguration(event *core.RequestEvent) error {
	organization, ok := authorizeSAMLManagement(event)
	if !ok {
		return nil
	}
	input := leafen.SAMLConfigurationInput{}
	if err := event.BindBody(&input); err != nil {
		return samlAPIError(event, leafen.ErrSAMLConfigurationInvalid, "configuration-bind")
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := saveSAMLConfiguration(txApp, organization, input)
		return err
	}); err != nil {
		return samlAPIError(event, err, "configuration-write")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	response, err := saveSAMLConfiguration(event.App, organization, input)
	if err != nil {
		return samlAPIError(event, err, "configuration-write")
	}
	fillSAMLServiceOrigins(event.App, event.Request, &response)
	return event.JSON(http.StatusOK, response)
}

func createSAMLDomain(event *core.RequestEvent) error {
	organization, ok := authorizeSAMLManagement(event)
	if !ok {
		return nil
	}
	input := leafen.SAMLDomainInput{}
	if err := event.BindBody(&input); err != nil {
		return samlAPIError(event, leafen.ErrSAMLConfigurationInvalid, "domain-bind")
	}
	response, err := createSAMLDomainChallenge(
		event.App, organization.OrganizationID, organization.PrincipalID, input.Domain,
	)
	if err != nil {
		return samlAPIError(event, err, "domain-challenge")
	}
	return event.JSON(http.StatusCreated, response)
}

func verifySAMLDomainRoute(event *core.RequestEvent) error {
	organization, ok := authorizeSAMLManagement(event)
	if !ok {
		return nil
	}
	response, err := verifySAMLDomain(
		event.Request.Context(), event.App, organization.OrganizationID,
		organization.PrincipalID,
		event.Request.PathValue("domain"),
	)
	if err != nil {
		return samlAPIError(event, err, "domain-verification")
	}
	return event.JSON(http.StatusOK, response)
}

func authorizeSAMLManagement(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	organization, err := leafen.AuthorizeOrganization(event.App, user.Id, organizationID, leafen.PermissionOrganizationIdentityAdmin)
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, false
	}
	if err != nil {
		_ = leafen.APIError(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.",
		)
		return leafen.OrganizationAuthorization{}, false
	}
	return organization, true
}

func serveSAMLMetadata(event *core.RequestEvent) error {
	provider, err := enabledSAMLProvider(event.App, event.Request.PathValue("providerId"))
	if err != nil {
		return samlAPIError(event, err, "metadata-provider")
	}
	config, err := loadSAMLProviderConfig(event.App, provider)
	if err != nil {
		return samlAPIError(event, err, "metadata-configuration")
	}
	serviceProvider, err := newSAMLServiceProvider(event.App, event.Request, config, false)
	if err != nil {
		return samlAPIError(event, err, "metadata-build")
	}
	payload, err := xml.MarshalIndent(serviceProvider.Metadata(), "", "  ")
	if err != nil {
		return samlAPIError(event, err, "metadata-render")
	}
	event.Response.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	event.Response.Header().Set("Cache-Control", "private, max-age=300")
	event.Response.WriteHeader(http.StatusOK)
	_, err = event.Response.Write(append([]byte(xml.Header), payload...))
	return err
}

func beginSAMLSignIn(event *core.RequestEvent) error {
	provider, err := enabledSAMLProvider(event.App, event.Request.PathValue("providerId"))
	if err != nil {
		return samlAPIError(event, err, "start-provider")
	}
	query := event.Request.URL.Query()
	relayState, err := validateSAMLRelayState(query.Get("redirect"))
	if err != nil {
		return samlAPIError(event, err, "start-relay-state")
	}
	appReturn, ok := leafen.ParseAuthAppReturn(query.Get(leafen.AuthAppReturnParam), query.Get(leafen.AuthAppChallengeParam))
	if !ok {
		return samlAPIError(event, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "invalid app return"), "start-app-return")
	}
	config, err := loadSAMLProviderConfig(event.App, provider)
	if err != nil {
		return samlAPIError(event, err, "start-configuration")
	}
	serviceProvider, err := newSAMLServiceProvider(event.App, event.Request, config, true)
	if err != nil {
		return samlAPIError(event, err, "start-provider-build")
	}
	request, err := serviceProvider.MakeAuthenticationRequest(
		config.LoginURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding,
	)
	if err != nil {
		return samlAPIError(event, err, "start-request-build")
	}
	destination, err := request.Redirect(url.QueryEscape(relayState), serviceProvider)
	if err != nil {
		return samlAPIError(event, err, "start-redirect-build")
	}
	if err := storeSAMLAuthenticationRequest(event.App, provider.ID, request.ID, relayState, appReturn); err != nil {
		return samlAPIError(event, err, "start-request-write")
	}
	return event.Redirect(http.StatusFound, destination.String())
}

func completeSAMLResponse(event *core.RequestEvent) error {
	if err := event.Request.ParseForm(); err != nil {
		return samlAPIError(event, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML form is invalid"), "acs-form")
	}
	decoded, err := preflightSAMLResponse(event.Request.Form.Get("SAMLResponse"))
	if err != nil {
		return samlAPIError(event, err, "acs-preflight")
	}
	envelope, err := readSAMLEnvelope(decoded)
	if err != nil {
		return samlAPIError(event, err, "acs-envelope")
	}
	responseContext, err := resolveSAMLResponseContext(event.App, envelope)
	if err != nil {
		return samlAPIError(event, err, "acs-correlation")
	}
	if responseContext.IDPInitiated {
		responseContext.RelayState, err = validateSAMLRelayState(event.Request.Form.Get("RelayState"))
		if err != nil {
			return samlAPIError(event, err, "acs-relay-state")
		}
	}
	config, err := loadSAMLProviderConfig(event.App, responseContext.Provider)
	if err != nil {
		return samlAPIError(event, err, "acs-configuration")
	}
	serviceProvider, err := newSAMLServiceProvider(event.App, event.Request, config, true)
	if err != nil {
		return samlAPIError(event, err, "acs-provider-build")
	}
	assertion, err := parseVerifiedSAMLResponse(serviceProvider, decoded, responseContext.RequestIDs)
	if err != nil {
		return samlAPIError(event, err, "acs-signature-validation")
	}
	if err := validateVerifiedSAMLAssertion(assertion, envelope.Assertion.ID); err != nil {
		return samlAPIError(event, err, "acs-assertion-validation")
	}
	code, relayState, err := acceptSAMLAssertion(
		event.App, responseContext, assertion, envelope.Assertion.ID,
	)
	if err != nil {
		return samlAPIError(event, err, "acs-durable-acceptance")
	}
	keepSAMLServiceOrigin(event.App, config, serviceProvider.AcsURL.String())
	// A sign-in an app started goes back to that app.
	destination, err := samlCompletionRedirect(relayState, code)
	if responseContext.AppReturn != "" {
		destination, err = leafen.AuthAppReturnDestination(responseContext.AppReturn, "samlCompletion", code)
	}
	if err != nil {
		return samlAPIError(event, err, "acs-completion-redirect")
	}
	return event.Redirect(http.StatusSeeOther, destination)
}

func exchangeSAMLCompletion(event *core.RequestEvent) error {
	input := leafen.SAMLExchangeInput{}
	if err := event.BindBody(&input); err != nil {
		return samlAPIError(event, errSAMLCompletionInvalid, "exchange-bind")
	}
	// One without its code is told so (api_missing_input.go).
	if strings.TrimSpace(input.Code) == "" {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeSAMLCompletionInvalid,
			"Check code: the completion code the SAML login page sent back.")
	}
	response, err := consumeSAMLCompletion(
		event.App, strings.TrimSpace(input.Code), strings.TrimSpace(input.Verifier),
		leafen.BrowserSessionMetadataFromEvent(event),
	)
	if err != nil {
		return samlAPIError(event, err, "exchange-consume")
	}
	return event.JSON(http.StatusOK, response)
}

func enabledSAMLProvider(app core.App, providerID string) (storedSAMLProvider, error) {
	identifier := strings.TrimSpace(providerID)
	provider, err := loadStoredSAMLProviderByID(app, identifier)
	if errors.Is(err, errSAMLProviderNotFound) {
		// The preserved browser entry point carries its organization identity.
		// Provider metadata and IdP callbacks continue to use the provider ID.
		provider, err = loadStoredSAMLProviderByOrganization(app, identifier)
	}
	if err != nil {
		return storedSAMLProvider{}, err
	}
	if provider.Enabled != 1 {
		return storedSAMLProvider{}, errSAMLProviderNotFound
	}
	return provider, nil
}

func validateVerifiedSAMLAssertion(assertion *saml.Assertion, assertionID string) error {
	if assertion == nil || assertion.ID == "" || assertion.ID != assertionID || assertion.Subject == nil ||
		len(assertion.Subject.SubjectConfirmations) == 0 || assertion.Conditions == nil {
		return newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "verified SAML assertion is incomplete")
	}
	return nil
}

func samlCompletionRedirect(relayState string, code string) (string, error) {
	destination, err := url.Parse(relayState)
	if err != nil {
		return "", err
	}
	query := destination.Query()
	query.Set("samlCompletion", code)
	destination.RawQuery = query.Encode()
	return destination.String(), nil
}

func samlAPIError(event *core.RequestEvent, err error, stage string) error {
	if storageError, ok := leafen.AuthStorageAPIError(event, err, "enterprise_saml", stage); ok {
		return storageError
	}
	status := http.StatusServiceUnavailable
	code := leafen.ErrorCodeSAMLUnavailable
	message := "Couldn't process the SAML login."
	var refusal *samlRefusal
	switch {
	case errors.As(err, &refusal):
		status, code, message = http.StatusBadRequest, refusal.code, refusal.message
	case errors.Is(err, leafen.ErrSAMLConfigurationInvalid):
		status, code, message = http.StatusBadRequest, leafen.ErrorCodeSAMLConfigurationInvalid, "Check the SAML settings."
	case errors.Is(err, errSAMLProviderNotFound):
		status, code, message = http.StatusNotFound, leafen.ErrorCodeSAMLProviderNotFound, "SAML login provider not found."
	case errors.Is(err, errSAMLAccountCreationOff):
		status, code, message = http.StatusForbidden, leafen.ErrorCodeSAMLAccountCreationOff, "This organization doesn't create accounts at SAML login."
	case errors.Is(err, errSAMLIdentityDenied):
		status, code, message = http.StatusForbidden, leafen.ErrorCodeSAMLIdentityDenied, "SAML login isn't available for this organization."
	case errors.Is(err, errSAMLCompletionInvalid):
		status, code, message = http.StatusBadRequest, leafen.ErrorCodeSAMLCompletionInvalid, "Check the SAML login completion."
	case errors.Is(err, errSAMLCompletionConsumed):
		status, code, message = http.StatusConflict, leafen.ErrorCodeSAMLCompletionConsumed, "The SAML login completion was already used."
	case errors.Is(err, leafen.ErrSAMLRequired):
		status, code, message = http.StatusForbidden, leafen.ErrorCodeSAMLRequired, "This organization requires SAML login."
	default:
		var protocolErr *samlProtocolError
		if protocolCode := samlProtocolFailureCode(err); errors.As(err, &protocolErr) {
			status, code, message = http.StatusBadRequest, protocolCode, "Couldn't verify the SAML login response."
		}
	}
	return leafen.APIErrorWithCause(
		event, status, code, message, "enterprise_saml", stage, code, err,
	)
}
