package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a key's call is answered with leaves out what a person's screen shows
// them and an agent has no use for, which in the wrong hands reaches further
// or finds a person (agent key decision of 2026-11-16): a session's ID, by
// which it is ended — a handle stands for it, made for that key alone, and
// ending a session takes the handle —, an IP address and its country, a
// browser's own string, the server's public IP, and the change tokens the
// web app follows a workspace by. The answers that carry such values are
// all listed here, by the routes a key reaches them at; the person, signed
// in, reads them whole.

const userKeySessionHandlePrefix = "lsh_"

// userKeyAnswerSecrets are the values each route's answer leaves out for a
// key: in each object found at a path ("" the answer itself, "[]" each item
// of a list), the fields named, and "id" made a session's handle.
type userKeyAnswerSecrets struct {
	at       string
	drop     []string
	sessions bool
}

var userKeyAnsweredSecrets = map[string]userKeyAnswerSecrets{
	"GET /account/sessions":                       {at: "[]", drop: []string{"ip", "country", "userAgent"}, sessions: true},
	"GET /admin/accounts/{accountId}/devices":     {at: "devices.[]", drop: []string{"ip", "country", "userAgent"}, sessions: true},
	"GET /account/auth-audit":                     {at: "events.[]", drop: []string{"ip", "userAgent"}},
	"GET /admin/audit":                            {at: "items.[]", drop: []string{"ip", "userAgent"}},
	"GET /view-links/{viewLinkId}/opens":          {at: "opens.[]", drop: []string{"ip", "userAgent"}},
	"GET /admin/tunnel/reach":                     {drop: []string{"publicIPv4"}},
	"GET /workspaces/{workspaceId}/activities":    {drop: []string{"refreshToken"}},
	"GET /bootstrap":                              {drop: []string{"workspaceRefreshTokens"}},
	"POST /workspaces/{workspaceId}/change-probe": {drop: []string{"workspaceRefreshToken"}},
}

// The routes that end a session by its ID take a key's handle instead, the
// account they end it for named by the path value given, "" for the key's
// own person.
var userKeySessionHandleRoutes = map[string]string{
	"DELETE /account/sessions/{sessionId}":                         "",
	"POST /admin/accounts/{accountId}/sessions/{sessionId}/revoke": "accountId",
}

// userKeySecretPatterns are the listed routes' own patterns, so a call a
// broad rule admits is still found by its route.
var userKeySecretPatterns = func() map[string]*regexp.Regexp {
	patterns := map[string]*regexp.Regexp{}
	for _, route := range slices.Concat(slices.Collect(maps.Keys(userKeyAnsweredSecrets)), slices.Collect(maps.Keys(userKeySessionHandleRoutes))) {
		method, path, _ := strings.Cut(route, " ")
		patterns[route] = userKeyRoute(method, path, userKeyScopeRead, userKeyPlacePerson, "").pattern
	}
	return patterns
}()

// userKeySecretRoute is the listed route a call's method and path (below
// apiRoot) are, or "".
func userKeySecretRoute(method, path string) string {
	for route, pattern := range userKeySecretPatterns {
		if strings.HasPrefix(route, method+" ") && pattern.MatchString(path) {
			return route
		}
	}
	return ""
}

// nextKeepingUserKeySecrets runs a key's call: a session handle in its path
// is made the session's ID again, and its answer leaves out what
// userKeyAnsweredSecrets lists.
func nextKeepingUserKeySecrets(event *core.RequestEvent, key userKeyAuthority) error {
	route := userKeySecretRoute(event.Request.Method, strings.TrimPrefix(event.Request.URL.Path, apiRoot))
	if account, ends := userKeySessionHandleRoutes[route]; ends {
		owner := key.OwnerID
		if account != "" {
			owner = event.Request.PathValue(account)
		}
		// A refusal names the handle sent, never the ID it stands for
		// (api_error_target.go).
		apiKeepSentPathValue(event, "sessionId")
		event.Request.SetPathValue("sessionId", userKeySessionOfHandle(event.App, key, owner, event.Request.PathValue("sessionId")))
	}
	secrets, kept := userKeyAnsweredSecrets[route]
	if !kept {
		return event.Next()
	}
	writer := &userKeySecretsWriter{ResponseWriter: event.Response}
	event.Response = writer
	err := event.Next()
	event.Response = writer.ResponseWriter
	writer.finish(event.App, key, secrets)
	return err
}

// userKeySessionHandle is what a key reads in place of a session's ID: the
// same session reads the same to that key, and to no other.
func userKeySessionHandle(app core.App, key userKeyAuthority, sessionID string) string {
	secret, err := mcpOAuthSecret(app)
	if err != nil || sessionID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("leafen-key-session-handle\x00" + key.ConnectionID + "\x00" + sessionID))
	return userKeySessionHandlePrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}

// userKeySessionOfHandle is the account's session a key's handle stands
// for, or the handle itself — which names no session — when none does: a
// key ends a session only by the handle it was given, never by an ID.
func userKeySessionOfHandle(app core.App, key userKeyAuthority, ownerID, handle string) string {
	if !strings.HasPrefix(handle, userKeySessionHandlePrefix) || ownerID == "" {
		return userKeySessionHandlePrefix
	}
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_auth_sessions}} WHERE [[owner_id]] = {:owner} AND [[revoked_at]] = 0`).
		Bind(dbx.Params{"owner": ownerID}).Column(&ids); err != nil {
		return handle
	}
	for _, id := range ids {
		if hmac.Equal([]byte(userKeySessionHandle(app, key, id)), []byte(handle)) {
			return id
		}
	}
	return handle
}

// userKeySecretsWriter holds a listed answer until its values are left out.
type userKeySecretsWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (writer *userKeySecretsWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *userKeySecretsWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.body.Write(data)
}

func (writer *userKeySecretsWriter) Status() int   { return writer.status }
func (writer *userKeySecretsWriter) Written() bool { return writer.status != 0 }

func (writer *userKeySecretsWriter) finish(app core.App, key userKeyAuthority, secrets userKeyAnswerSecrets) {
	if writer.status == 0 {
		return
	}
	body := writer.body.Bytes()
	if writer.status < http.StatusBadRequest && strings.HasPrefix(writer.Header().Get("Content-Type"), "application/json") {
		var answer any
		if json.Unmarshal(body, &answer) == nil {
			for _, item := range userKeyAnswerObjects(answer, secrets.at) {
				for _, field := range secrets.drop {
					delete(item, field)
				}
				if id, ok := item["id"].(string); ok && secrets.sessions {
					item["id"] = userKeySessionHandle(app, key, id)
				}
			}
			if encoded, err := json.Marshal(answer); err == nil {
				body = encoded
			}
		}
	}
	writer.Header().Del("Content-Length")
	writer.ResponseWriter.WriteHeader(writer.status)
	_, _ = writer.ResponseWriter.Write(body)
}

// userKeyAnswerObjects are the objects at a path in an answer: "" the
// answer, "name" a field's value, "[]" each item of a list.
func userKeyAnswerObjects(value any, path string) []map[string]any {
	if path == "" {
		if object, ok := value.(map[string]any); ok {
			return []map[string]any{object}
		}
		return nil
	}
	step, rest, _ := strings.Cut(path, ".")
	if step == "[]" {
		list, _ := value.([]any)
		found := []map[string]any{}
		for _, item := range list {
			found = append(found, userKeyAnswerObjects(item, rest)...)
		}
		return found
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return userKeyAnswerObjects(object[step], rest)
}
