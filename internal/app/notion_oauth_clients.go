package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	notionOAuthClientsEnvironment      = "LEAFEN_NOTION_COMPAT_OAUTH_CLIENTS"
	notionOAuthClientIDEnvironment     = "LEAFEN_NOTION_COMPAT_OAUTH_CLIENT_ID"
	notionOAuthClientNameEnvironment   = "LEAFEN_NOTION_COMPAT_OAUTH_CLIENT_NAME"
	notionOAuthClientSecretEnvironment = "LEAFEN_NOTION_COMPAT_OAUTH_CLIENT_SECRET"
	notionOAuthClientHashEnvironment   = "LEAFEN_NOTION_COMPAT_OAUTH_CLIENT_SECRET_HASH"
	notionOAuthRedirectsEnvironment    = "LEAFEN_NOTION_COMPAT_OAUTH_REDIRECT_URIS"
	notionOAuthWebhookEnvironment      = "LEAFEN_NOTION_COMPAT_OAUTH_WEBHOOK_URL"
	notionOAuthClientAuthMethod        = "client_secret_basic"
)

type notionOAuthProtocolError struct {
	status  int
	code    string
	message string
}

func (failure *notionOAuthProtocolError) Error() string {
	return failure.code
}

type notionOAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	SecretHashes []string
	// WebhookURL receives, in Notion's format, the events of the pages its
	// connections are shared, of the kinds WebhookEvents names (all when
	// empty), as a Notion integration's webhook subscription does.
	WebhookURL    string
	WebhookEvents []string
}

type notionOAuthClientInput struct {
	ClientID         string   `json:"clientId"`
	ClientIDSnake    string   `json:"client_id"`
	ClientName       string   `json:"clientName"`
	ClientNameSnake  string   `json:"client_name"`
	ClientSecret     string   `json:"clientSecret"`
	ClientSecretAlt  string   `json:"client_secret"`
	ClientSecretHash string   `json:"clientSecretHash"`
	SecretHashAlt    string   `json:"client_secret_hash"`
	RedirectURIs     []string `json:"redirectUris"`
	RedirectURIsAlt  []string `json:"redirect_uris"`
	WebhookURL       string   `json:"webhookUrl"`
	WebhookURLAlt    string   `json:"webhook_url"`
	WebhookEvents    []string `json:"webhookEvents"`
	WebhookEventsAlt []string `json:"webhook_events"`
}

func notionOAuthFailure(status int, code string, message string) error {
	return &notionOAuthProtocolError{status: status, code: code, message: message}
}

func notionOAuthSecretHash(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

func decodeNotionOAuthBasic(request *http.Request) (string, string, bool) {
	header := strings.TrimSpace(request.Header.Get("Authorization"))
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}
	separator := strings.IndexByte(string(decoded), ':')
	if separator < 1 {
		return "", "", false
	}
	return string(decoded[:separator]), string(decoded[separator+1:]), true
}

func normalizeNotionOAuthClient(input notionOAuthClientInput, fallbackID string) (notionOAuthClient, bool) {
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		clientID = strings.TrimSpace(input.ClientIDSnake)
	}
	if clientID == "" {
		clientID = strings.TrimSpace(fallbackID)
	}
	name := strings.TrimSpace(input.ClientName)
	if name == "" {
		name = strings.TrimSpace(input.ClientNameSnake)
	}
	if name == "" {
		name = "Leafen integration"
	}
	secret := input.ClientSecret
	if secret == "" {
		secret = input.ClientSecretAlt
	}
	configuredHash := strings.ToLower(strings.TrimSpace(input.ClientSecretHash))
	if configuredHash == "" {
		configuredHash = strings.ToLower(strings.TrimSpace(input.SecretHashAlt))
	}
	hashes := []string{}
	if secret != "" {
		hashes = append(hashes, notionOAuthSecretHash(secret))
	}
	if decoded, err := hex.DecodeString(configuredHash); err == nil && len(decoded) == sha256.Size {
		hashes = append(hashes, configuredHash)
	}
	redirects := append([]string{}, input.RedirectURIs...)
	redirects = append(redirects, input.RedirectURIsAlt...)
	redirects = uniqueNonemptyStrings(redirects)
	if clientID == "" || len(hashes) == 0 || len(redirects) == 0 {
		return notionOAuthClient{}, false
	}
	webhook := strings.TrimSpace(input.WebhookURL)
	if webhook == "" {
		webhook = strings.TrimSpace(input.WebhookURLAlt)
	}
	events := uniqueNonemptyStrings(append(append([]string{}, input.WebhookEvents...), input.WebhookEventsAlt...))
	return notionOAuthClient{
		ID: clientID, Name: name, RedirectURIs: redirects, SecretHashes: uniqueNonemptyStrings(hashes),
		WebhookURL: webhook, WebhookEvents: events,
	}, true
}

func configuredNotionOAuthClients() ([]notionOAuthClient, error) {
	clients := []notionOAuthClient{}
	raw := strings.TrimSpace(os.Getenv(notionOAuthClientsEnvironment))
	if raw != "" {
		var list []notionOAuthClientInput
		if err := json.Unmarshal([]byte(raw), &list); err == nil {
			for _, input := range list {
				if client, ok := normalizeNotionOAuthClient(input, ""); ok {
					clients = append(clients, client)
				}
			}
		} else {
			var object map[string]json.RawMessage
			if decodeErr := json.Unmarshal([]byte(raw), &object); decodeErr != nil {
				return nil, errors.New("invalid Notion OAuth client configuration")
			}
			for clientID, encoded := range object {
				var plain string
				if json.Unmarshal(encoded, &plain) == nil {
					if client, ok := normalizeNotionOAuthClient(notionOAuthClientInput{
						ClientSecret: plain,
					}, clientID); ok {
						clients = append(clients, client)
					}
					continue
				}
				var input notionOAuthClientInput
				if decodeErr := json.Unmarshal(encoded, &input); decodeErr != nil {
					return nil, errors.New("invalid Notion OAuth client configuration")
				}
				if client, ok := normalizeNotionOAuthClient(input, clientID); ok {
					clients = append(clients, client)
				}
			}
		}
	}

	singleID := strings.TrimSpace(os.Getenv(notionOAuthClientIDEnvironment))
	if singleID != "" {
		redirects := strings.Split(os.Getenv(notionOAuthRedirectsEnvironment), ",")
		if client, ok := normalizeNotionOAuthClient(notionOAuthClientInput{
			ClientName:       os.Getenv(notionOAuthClientNameEnvironment),
			ClientSecret:     os.Getenv(notionOAuthClientSecretEnvironment),
			ClientSecretHash: os.Getenv(notionOAuthClientHashEnvironment),
			RedirectURIs:     redirects,
			WebhookURL:       os.Getenv(notionOAuthWebhookEnvironment),
		}, singleID); ok {
			clients = append(clients, client)
		}
	}
	seen := map[string]struct{}{}
	for _, client := range clients {
		if _, exists := seen[client.ID]; exists {
			return nil, errors.New("duplicate Notion OAuth client configuration")
		}
		seen[client.ID] = struct{}{}
	}
	return clients, nil
}

func resolveNotionOAuthClient(clientID string) (notionOAuthClient, error) {
	clients, err := configuredNotionOAuthClients()
	if err != nil {
		return notionOAuthClient{}, err
	}
	for _, client := range clients {
		if client.ID == clientID {
			return client, nil
		}
	}
	return notionOAuthClient{}, notionOAuthFailure(http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
}

func authenticateNotionOAuthClient(request *http.Request, body map[string]any) (notionOAuthClient, error) {
	basicID, basicSecret, ok := decodeNotionOAuthBasic(request)
	bodyID, _ := body["client_id"].(string)
	bodySecret, _ := body["client_secret"].(string)
	if !ok || basicID == "" || basicSecret == "" ||
		(bodyID != "" && bodyID != basicID) || (bodySecret != "" && bodySecret != basicSecret) {
		return notionOAuthClient{}, notionOAuthFailure(http.StatusUnauthorized, "invalid_client", "HTTP Basic client authentication is required.")
	}
	client, err := resolveNotionOAuthClient(basicID)
	if err != nil {
		return notionOAuthClient{}, err
	}
	presented := []byte(notionOAuthSecretHash(basicSecret))
	for _, expected := range client.SecretHashes {
		if subtle.ConstantTimeCompare(presented, []byte(expected)) == 1 {
			return client, nil
		}
	}
	return notionOAuthClient{}, notionOAuthFailure(http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
}

func validateNotionOAuthRedirectURI(client notionOAuthClient, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return notionOAuthFailure(http.StatusBadRequest, "invalid_request", "redirect_uri is invalid.")
	}
	loopback := parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return notionOAuthFailure(http.StatusBadRequest, "invalid_request", "redirect_uri must use HTTPS.")
	}
	for _, allowed := range client.RedirectURIs {
		if allowed == raw {
			return nil
		}
	}
	return notionOAuthFailure(http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client.")
}
