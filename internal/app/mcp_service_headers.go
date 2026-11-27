package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's "사용자 지정 MCP 서버" form for a server that opens no sign-in page
// (seen 2026-10-13): the connection's name, how it signs in — "Bearer token"
// (the prefix "Bearer" or "Token", and the token), "Basic" (username and
// password) or "API Key" (a header's name and its value) — and, under "고급
// 설정", headers of the person's own "sent with each request to this
// server". The server keeps them as one list of headers sealed with the
// settings key, sends them to the service's own host only, and no agent
// sees them. The name the person gives stands for the service's own.

const (
	mcpServiceHeaderAuthBearer = "bearer"
	mcpServiceHeaderAuthBasic  = "basic"
	mcpServiceHeaderAuthAPIKey = "api_key"
	maxMCPServiceHeaders       = 20
	maxMCPServiceNameRunes     = 120
)

// Headers the MCP transport sends itself, or only the connection's own HTTP
// sets; a person's header doesn't stand in for them.
var mcpServiceReservedHeaders = map[string]bool{
	"host": true, "content-length": true, "content-type": true, "content-encoding": true, "transfer-encoding": true,
	"connection": true, "keep-alive": true, "upgrade": true, "te": true, "trailer": true, "proxy-authorization": true,
	"proxy-connection": true, "accept": true, "accept-encoding": true, "mcp-session-id": true, "mcp-protocol-version": true,
	"last-event-id": true,
}

var mcpServiceHeaderNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,100}$")

// mcpServiceHeaderAuth is how the form signs in: Type is "bearer" (the
// default), "basic" or "api_key".
type mcpServiceHeaderAuth struct {
	Type     string `json:"type"`
	Prefix   string `json:"prefix"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

type mcpServiceHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func mcpServiceHeaderText(value string, limit int) bool {
	return len(value) <= limit && !strings.ContainsAny(value, "\r\n\x00")
}

// mcpServiceHeadersOf is what the form sends the service: the sign-in's
// header, then the person's own, each name once; none when it gives none.
func mcpServiceHeadersOf(auth mcpServiceHeaderAuth, own []mcpServiceHeader) ([][2]string, error) {
	headers := [][2]string{}
	add := func(name, value string) error {
		if !mcpServiceHeaderNamePattern.MatchString(name) || mcpServiceReservedHeaders[strings.ToLower(name)] ||
			!mcpServiceHeaderText(value, 8192) {
			return errMCPServiceInvalid
		}
		for _, header := range headers {
			if strings.EqualFold(header[0], name) {
				return errMCPServiceInvalid
			}
		}
		headers = append(headers, [2]string{http.CanonicalHeaderKey(name), value})
		return nil
	}
	var err error
	switch auth.Type {
	case "", mcpServiceHeaderAuthBearer:
		prefix, token := firstNonempty(auth.Prefix, "Bearer"), strings.TrimSpace(auth.Token)
		if prefix != "Bearer" && prefix != "Token" {
			return nil, errMCPServiceInvalid
		}
		if token != "" {
			err = add("Authorization", prefix+" "+token)
		}
	case mcpServiceHeaderAuthBasic:
		// RFC 7617: the username holds no colon.
		if strings.Contains(auth.Username, ":") || !mcpServiceHeaderText(auth.Username, 255) || !mcpServiceHeaderText(auth.Password, 1024) {
			return nil, errMCPServiceInvalid
		}
		if auth.Username != "" || auth.Password != "" {
			err = add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth.Username+":"+auth.Password)))
		}
	case mcpServiceHeaderAuthAPIKey:
		if key, value := strings.TrimSpace(auth.Key), strings.TrimSpace(auth.Value); key != "" || value != "" {
			err = add(key, value)
		}
	default:
		return nil, errMCPServiceInvalid
	}
	if err != nil || len(own) > maxMCPServiceHeaders {
		return nil, errMCPServiceInvalid
	}
	for _, header := range own {
		name, value := strings.TrimSpace(header.Name), strings.TrimSpace(header.Value)
		if name == "" && value == "" {
			continue
		}
		if err := add(name, value); err != nil {
			return nil, err
		}
	}
	return headers, nil
}

// normalizeMCPServiceName is the name the person gives the connection, on
// one line; empty keeps the service's own.
func normalizeMCPServiceName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if len([]rune(name)) > maxMCPServiceNameRunes || strings.ContainsAny(name, "\r\n\x00") {
		return "", errMCPServiceInvalid
	}
	return name, nil
}

func sealMCPServiceHeaders(app core.App, headers [][2]string) (string, error) {
	if len(headers) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		return "", err
	}
	return encryptAdministratorSetting(app, cipherMCPService, string(encoded))
}

// requestHeaders is the person's headers for the service, opened; one that
// doesn't open asks them to connect it again.
func (stored storedMCPService) requestHeaders(app core.App) ([][2]string, error) {
	if stored.Headers == "" {
		return nil, nil
	}
	opened, err := decryptAdministratorSetting(app, stored.Headers)
	headers := [][2]string{}
	if err != nil || json.Unmarshal([]byte(opened), &headers) != nil {
		return nil, errMCPServiceSignIn
	}
	return headers, nil
}

func mcpServiceBearerHeaders(bearer string) [][2]string {
	if bearer == "" {
		return nil
	}
	return [][2]string{{"Authorization", "Bearer " + bearer}}
}
