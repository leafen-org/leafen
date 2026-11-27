package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's way to connect an integration: it sends the person to
// /v1/oauth/authorize, where they sign in, choose a workspace and the pages
// it may reach, and go back with a code to exchange at /v1/oauth/token
// (Notion's developer guide, "Authorization"). The request is checked here
// and answered in the app (NotionAuthorizationPage), whose approval is
// issueNotionOAuthAuthorizationCode; a request that can't go back is shown
// as an error, one that can goes back with Notion's OAuth error.
const (
	notionAPIOAuthAuthorizePath = notionAPIRoot + "/oauth/authorize"
	notionOAuthAuthorizeParam   = "notionAuthorization"
	maxNotionOAuthStateLength   = 1024
)

type notionOAuthAuthorizeRequest struct {
	ClientID    string `json:"clientId"`
	RedirectURI string `json:"redirectUri"`
	State       string `json:"state"`
}

type notionOAuthRequestInput struct {
	ClientID    string `json:"clientId"`
	RedirectURI string `json:"redirectUri"`
}

type notionOAuthRequestResponse struct {
	ClientName  string   `json:"clientName"`
	RedirectURI string   `json:"redirectUri"`
	Scopes      []string `json:"scopes"`
}

// notionOAuthAuthorizeRedirect is where the person goes back: the address
// asked for when the client registered it, or its only one when none is named.
func notionOAuthAuthorizeRedirect(client notionOAuthClient, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" && len(client.RedirectURIs) == 1 {
		requested = client.RedirectURIs[0]
	}
	if err := validateNotionOAuthRedirectURI(client, requested); err != nil {
		return "", err
	}
	return requested, nil
}

func notionOAuthErrorRedirect(redirectURI, code, state string) (string, error) {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("error", code)
	if state != "" {
		query.Set("state", state)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func beginNotionOAuthAuthorization(event *core.RequestEvent) error {
	query := event.Request.URL.Query()
	client, err := resolveNotionOAuthClient(strings.TrimSpace(query.Get("client_id")))
	if err != nil {
		return notionOAuthAuthorizeFailure(event, "invalid_client", "The integration isn't registered with this server.")
	}
	redirectURI, err := notionOAuthAuthorizeRedirect(client, query.Get("redirect_uri"))
	if err != nil {
		return notionOAuthAuthorizeFailure(event, "invalid_request", "The redirect URI isn't registered for this integration.")
	}
	state := query.Get("state")
	reason := ""
	switch {
	case query.Get("response_type") != "code":
		reason = "unsupported_response_type"
	case query.Get("owner") != "" && query.Get("owner") != "user", len(state) > maxNotionOAuthStateLength:
		reason = "invalid_request"
	}
	if reason != "" {
		target, err := notionOAuthErrorRedirect(redirectURI, reason, state)
		if err != nil {
			return notionOAuthAuthorizeFailure(event, "invalid_request", "The redirect URI is invalid.")
		}
		return event.Redirect(http.StatusSeeOther, target)
	}
	encoded, err := json.Marshal(notionOAuthAuthorizeRequest{ClientID: client.ID, RedirectURI: redirectURI, State: state})
	if err != nil {
		return notionOAuthAuthorizeFailure(event, "server_error", "Couldn't start the connection.")
	}
	destination := &url.URL{Path: "/"}
	destination.RawQuery = url.Values{notionOAuthAuthorizeParam: []string{base64.RawURLEncoding.EncodeToString(encoded)}}.Encode()
	return event.Redirect(http.StatusSeeOther, destination.String())
}

func notionOAuthAuthorizeFailure(event *core.RequestEvent, code, message string) error {
	return event.JSON(http.StatusBadRequest, map[string]string{"error": code, "error_description": message})
}

// readNotionOAuthRequest names, for the person answering, the integration
// asking and where it goes back.
func readNotionOAuthRequest(event *core.RequestEvent) error {
	if _, ok := authenticatedUser(event); !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input notionOAuthRequestInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Check the Notion connection request.")
	}
	if strings.TrimSpace(input.ClientID) == "" {
		return refuseMissingInput(event, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "clientId", "the client_id the Notion connection's authorize link names.")
	}
	client, err := resolveNotionOAuthClient(strings.TrimSpace(input.ClientID))
	if err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Registered Notion connection not found.")
	}
	redirectURI, err := notionOAuthAuthorizeRedirect(client, input.RedirectURI)
	if err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Check the Notion return URL.")
	}
	name := strings.TrimSpace(client.Name)
	if name == "" {
		name = client.ID
	}
	return event.JSON(http.StatusOK, notionOAuthRequestResponse{
		ClientName: name, RedirectURI: redirectURI, Scopes: append([]string{}, notionOAuthDefaultScopes...),
	})
}
