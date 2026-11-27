package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An agent on another computer connects to Leafen without its person
// opening Leafen (user decision 2026-11-13: Leafen is set up and used in
// the conversation with an agent; a program's own browser sign-in, MCP's
// standard, stays for the programs that start it). As in OAuth's device
// authorization (RFC 8628), the agent asks with no key and its name, and
// gets a short code for its person and a device code to poll with. The
// person allows the code in their conversation with an agent already
// connected (leafen_connection_requests, which asks them as the boundary of
// person_ask.go does), or, with none connected yet, by typing it in Leafen's
// phone app (mcp_device_connection_screen.go); no link to a page is given. Allowed, the agent's next poll makes its MCP token, as the token
// page makes one, and gets its key once; its person sees the key again on
// the token's page. A code lasts ten minutes and is used once; asking, a
// wrong device code and a wrong code given by an agent are limited by
// address, connection and across the server; the request's row keeps who
// asked from where, who allowed it in which place, and the connection. Sent
// with a connection's own key, the same request asks its person for more for
// that connection instead (mcp_device_connection_widen.go).

const (
	mcpDeviceCodePrefix                 = "ldd_"
	mcpDeviceCodeLength                 = 40
	mcpDeviceUserCodeLength             = 8
	mcpDeviceConnectionLifetime         = 10 * time.Minute
	mcpDeviceConnectionInterval         = 5 * time.Second
	mcpDeviceConnectionsWaitingOnServer = 64
	mcpDeviceGrantType                  = "urn:ietf:params:oauth:grant-type:device_code"
	mcpDeviceConnectionPath             = "/mcp/device-connections"
	mcpDeviceConnectionTokenPath        = "/mcp/device-connections/token"
	mcpDeviceConnectionLimiterStoreKey  = "leafen.mcpDeviceConnectionLimiter"

	errorCodeMCPDeviceConnectionInvalid = "LEAFEN_MCP_DEVICE_CONNECTION_INVALID"
	errorCodeMCPDeviceConnectionLimited = "LEAFEN_MCP_DEVICE_CONNECTION_LIMITED"
	errorCodeMCPDeviceConnectionFailed  = "LEAFEN_MCP_DEVICE_CONNECTION_FAILED"
)

var errMCPDeviceConnectionMissing = errors.New(errorCodeMCPDeviceConnectionInvalid)

type mcpDeviceConnectionLimiter struct {
	asks         *requestWindowLimiter
	wrongDevice  *requestWindowLimiter
	wrongCodes   *requestWindowLimiter
	serverWrongs *requestWindowLimiter
}

func mcpDeviceConnectionLimiterOf(app core.App) *mcpDeviceConnectionLimiter {
	return app.Store().GetOrSet(mcpDeviceConnectionLimiterStoreKey, func() any {
		return &mcpDeviceConnectionLimiter{
			asks:         newRequestWindowLimiter(10, 15*time.Minute),
			wrongDevice:  newRequestWindowLimiter(30, 15*time.Minute),
			wrongCodes:   newRequestWindowLimiter(10, 15*time.Minute),
			serverWrongs: newRequestWindowLimiter(200, time.Hour),
		}
	}).(*mcpDeviceConnectionLimiter)
}

func registerMCPDeviceConnectionRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+mcpDeviceConnectionPath, askMCPDeviceConnection).Bind(apis.BodyLimit(8 << 10))
	event.Router.POST(apiRoot+mcpDeviceConnectionTokenPath, pollMCPDeviceConnection).Bind(apis.BodyLimit(8 << 10))
	registerMCPDeviceConnectionScreenRoutes(event)
}

// mcpDeviceConnection is one request as it waits.
type mcpDeviceConnection struct {
	ID          string `db:"id"`
	ClientName  string `db:"client_name"`
	RequestIP   string `db:"request_ip"`
	Status      string `db:"status"`
	CreatedAt   int64  `db:"created_at"`
	ExpiresAt   int64  `db:"expires_at"`
	PolledAt    int64  `db:"polled_at"`
	OwnerID     string `db:"owner_id"`
	WorkspaceID string `db:"workspace_ids"`
	Scope       string `db:"scope"`
	// The connection it gave, once used.
	ConnectionID string `db:"connection_id"`
}

const mcpDeviceConnectionColumns = `[[id]], [[client_name]], [[request_ip]], [[status]], [[created_at]], [[expires_at]], [[polled_at]],
	[[owner_id]], [[workspace_ids]], [[scope]], [[connection_id]]`

type mcpDeviceConnectionAskInput struct {
	Name       string `json:"name" form:"name"`
	ClientName string `json:"client_name" form:"client_name"`
}

type mcpDeviceConnectionStart struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
	TokenURL   string `json:"token_url"`
	Message    string `json:"message"`
}

// askMCPDeviceConnection takes an agent's request to connect, sent with no
// key; one a connection's key sends has nothing to ask for
// (mcp_device_connection_widen.go).
func askMCPDeviceConnection(event *core.RequestEvent) error {
	if key, keyed := userKeyOf(event); keyed {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", mcpDeviceAskedWithKey(connectionKindOf(event.App, key)))
	}
	input := mcpDeviceConnectionAskInput{}
	_ = event.BindBody(&input)
	name := strings.TrimSpace(firstNonempty(input.Name, input.ClientName))
	if name == "" || utf8.RuneCountInString(name) > 255 || strings.ContainsFunc(name, unicode.IsControl) {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request",
			"Give your program's name as name, on one line, as your person will see it (such as \"Claude Code on my laptop\").")
	}
	limiter := mcpDeviceConnectionLimiterOf(event.App)
	address := "address:" + requestLimitAddress(event.RealIP())
	if admitted, wait := limiter.asks.admit(address, time.Now()); !admitted {
		event.Response.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		return oauthProtocolError(event, http.StatusTooManyRequests, "slow_down", "Too many requests to connect came from here. Wait a while and ask again.")
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
			"Couldn't take the request to connect.", err)
	}
	// A public address takes agents only over HTTPS; say so before the person is asked.
	if _, err := newMCPConnectionURI(origin+mcpRoutePath, mcpConnectionCodeProbeKey); err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "Agents connect to a public server address only over HTTPS.")
	}
	deviceCode := mcpDeviceCodePrefix + security.RandomStringWithAlphabet(mcpDeviceCodeLength, mcpConnectionCodeAlphabet)
	userCode := ""
	now := time.Now().UnixMilli()
	expiresAt := now + mcpDeviceConnectionLifetime.Milliseconds()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		// Requests left waiting go a day after they ran out; decided and used
		// ones stay as the record of their connection until credentials retire.
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_mcp_device_connections}}
			WHERE ([[status]] IN ('pending', 'approved') AND [[expires_at]] <= {:stale})
				OR ([[status]] IN ('declined', 'used') AND [[decided_at]] <= {:retired})`).
			Bind(dbx.Params{"stale": now - (24 * time.Hour).Milliseconds(), "retired": now - int64(mcpCredentialRetentionMilliseconds)}).
			Execute(); err != nil {
			return err
		}
		waiting := 0
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_mcp_device_connections}}
			WHERE [[status]] = 'pending' AND [[expires_at]] > {:now}`).Bind(dbx.Params{"now": now}).Row(&waiting); err != nil {
			return err
		}
		if waiting >= mcpDeviceConnectionsWaitingOnServer {
			return errDeviceCodeLimited
		}
		// No two requests waiting share a code.
		for attempt := 0; userCode == "" && attempt < 8; attempt++ {
			candidate := security.RandomStringWithAlphabet(mcpDeviceUserCodeLength, deviceCodeAlphabet)
			if _, found, err := findWaitingMCPDeviceConnection(txApp, candidate); err != nil {
				return err
			} else if !found {
				userCode = candidate
			}
		}
		if userCode == "" {
			return errDeviceCodeLimited
		}
		_, err := txApp.DB().Insert(mcpDeviceConnectionsTable, dbx.Params{
			"id": "ldr_" + security.RandomString(20), "device_code_hash": mcpTokenHash(deviceCode), "user_code_hash": mcpTokenHash(userCode),
			"client_name": name, "request_ip": boundedAuthSessionMetadata(event.RealIP(), maxAuthSessionIPLength),
			"request_user_agent": boundedAuthSessionMetadata(event.Request.UserAgent(), maxAuthSessionUserAgentLength),
			"status":             "pending", "created_at": now, "expires_at": expiresAt,
		}).Execute()
		return err
	})
	if errors.Is(err, errDeviceCodeLimited) {
		limiter.asks.refund(address)
		event.Response.Header().Set("Retry-After", "60")
		return oauthProtocolError(event, http.StatusTooManyRequests, "slow_down", "Too many agents are waiting to connect to this server. Ask again in a few minutes.")
	}
	if err != nil {
		return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
			"Couldn't take the request to connect.", err)
	}
	shown := codeWithServerCode(event.App, codeInFours(userCode))
	tokenURL := origin + apiRoot + mcpDeviceConnectionTokenPath
	interval := int(mcpDeviceConnectionInterval.Seconds())
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusCreated, mcpDeviceConnectionStart{
		DeviceCode: deviceCode, UserCode: shown, ExpiresIn: int(mcpDeviceConnectionLifetime.Seconds()), Interval: interval, TokenURL: tokenURL,
		Message: mcpDeviceAskAnswer(shown, name, tokenURL, interval),
	})
}

type mcpDeviceConnectionPollInput struct {
	DeviceCode string `json:"device_code" form:"device_code"`
	GrantType  string `json:"grant_type" form:"grant_type"`
	// Check asks whether the person allowed it, taking nothing yet; Answer
	// "claude-code" takes the key as Claude Code's `claude mcp add-json`
	// takes Leafen's entry (agent_connect_claude_code.go).
	Check  bool   `json:"check" form:"check"`
	Answer string `json:"answer" form:"answer"`
}

type mcpDeviceConnectionToken struct {
	AccessToken  string   `json:"access_token"`
	TokenType    string   `json:"token_type"`
	MCPURL       string   `json:"mcp_url"`
	ConnectionID string   `json:"connection_id"`
	ClientName   string   `json:"client_name"`
	WorkspaceIDs []string `json:"workspace_ids"`
	Message      string   `json:"message"`
}

// pollMCPDeviceConnection answers an agent polling its request: still
// waiting, too soon, declined, ran out, or — once allowed — its key, made
// once. The key's making and the request's use commit together.
func pollMCPDeviceConnection(event *core.RequestEvent) error {
	input := mcpDeviceConnectionPollInput{}
	_ = event.BindBody(&input)
	if grant := strings.TrimSpace(input.GrantType); grant != "" && grant != mcpDeviceGrantType {
		return oauthProtocolError(event, http.StatusBadRequest, "unsupported_grant_type", "This takes the device_code grant only.")
	}
	if input.Answer != "" && input.Answer != agentAddToClaudeCode {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "answer is claude-code, or left out for the key itself.")
	}
	// One without its device code is told so (api_missing_input.go).
	if strings.TrimSpace(input.DeviceCode) == "" {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "device_code is missing: give the device_code the request to connect answered.")
	}
	limiter := mcpDeviceConnectionLimiterOf(event.App)
	address := "address:" + requestLimitAddress(event.RealIP())
	if admitted, _ := limiter.wrongDevice.admit(address, time.Now()); !admitted {
		event.Response.Header().Set("Retry-After", "900")
		return oauthProtocolError(event, http.StatusTooManyRequests, "slow_down", "Too many wrong device codes came from here. Wait a while.")
	}
	deviceCode := strings.TrimSpace(input.DeviceCode)
	stored := mcpDeviceConnection{}
	err := event.App.DB().NewQuery(`SELECT ` + mcpDeviceConnectionColumns + ` FROM {{leafen_mcp_device_connections}}
		WHERE [[device_code_hash]] = {:hash}`).Bind(dbx.Params{"hash": mcpTokenHash(deviceCode)}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) || deviceCode == "" {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_grant", "This device code isn't one this server gave.")
	}
	if err != nil {
		return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
			"Couldn't read the request to connect.", err)
	}
	limiter.wrongDevice.refund(address)
	now := time.Now().UnixMilli()
	switch {
	// A used request gives its key again within its ten minutes while that
	// key was never used (mcp_connection_retrade.go).
	case stored.Status == "used" && (stored.ExpiresAt <= now || !mcpConnectionNeverUsed(event.App, stored.OwnerID, stored.ConnectionID, "")):
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_grant", "This request already gave its key. Ask to connect again for another.")
	case stored.Status == "declined":
		return oauthProtocolError(event, http.StatusBadRequest, "access_denied", "The person declined this connection.")
	case stored.ExpiresAt <= now:
		return oauthProtocolError(event, http.StatusBadRequest, "expired_token", "The code ran out before it was allowed. Ask to connect again.")
	case stored.Status == "pending":
		// One poll per interval, as RFC 8628 has it; a sooner one hears to slow down.
		if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_mcp_device_connections}} SET [[polled_at]] = {:now} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": stored.ID, "now": now}).Execute(); err != nil {
			return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
				"Couldn't read the request to connect.", err)
		}
		if stored.PolledAt > 0 && now-stored.PolledAt < mcpDeviceConnectionInterval.Milliseconds()-500 {
			return oauthProtocolError(event, http.StatusBadRequest, "slow_down", "Poll every five seconds or more.")
		}
		return oauthProtocolError(event, http.StatusBadRequest, "authorization_pending", "Your person hasn't decided yet; poll again.")
	}
	if input.Check {
		return mcpDeviceConnectionAllowedAnswer(event, stored)
	}
	return connectAllowedMCPDevice(event, stored, input.Answer)
}

// connectAllowedMCPDevice makes the connection its person allowed, and
// answers with its key, or with Leafen's entry for Claude Code.
func connectAllowedMCPDevice(event *core.RequestEvent, stored mcpDeviceConnection, answer string) error {
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
			"Couldn't create the MCP connection.", err)
	}
	secret, err := mcpOAuthSecret(event.App)
	if err != nil {
		return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
			"Couldn't create the MCP connection.", err)
	}
	// An agent connection, for all its person's workspaces (connection_person_rights.go).
	connection := mcpConnectionInput{RequestID: "device:" + stored.ID, ClientName: stored.ClientName}
	var response mcpDeviceConnectionToken
	err = event.App.RunInTransaction(func(txApp core.App) error {
		now := time.Now().UnixMilli()
		if stored.Status == "used" {
			again, err := takeBackUnusedMCPConnection(txApp, stored.OwnerID, stored.ConnectionID, "", now)
			if err != nil {
				return err
			}
			if !again {
				return errMCPDeviceConnectionMissing
			}
			connection.RequestID = mcpRetradeRequestID(connection.RequestID, now)
		}
		requestHash, err := mcpManualCompletionRequestHash(connection)
		if err != nil {
			return err
		}
		result := completeManualMCPConnection(txApp, stored.OwnerID, connection, requestHash, origin+mcpRoutePath, secret)
		if result.err != nil {
			return result.err
		}
		claimed, err := txApp.DB().NewQuery(`UPDATE {{leafen_mcp_device_connections}}
			SET [[status]] = 'used', [[connection_id]] = {:connection}, [[used_at]] = {:now}, [[used_ip]] = {:ip}
			WHERE [[id]] = {:id} AND [[status]] = {:status} AND [[connection_id]] = {:previous} AND [[expires_at]] > {:now}`).Bind(dbx.Params{
			"id": stored.ID, "connection": result.response.ID, "now": now, "status": stored.Status, "previous": stored.ConnectionID,
			"ip": boundedAuthSessionMetadata(event.RealIP(), maxAuthSessionIPLength),
		}).Execute()
		if err != nil {
			return err
		}
		if rows, err := claimed.RowsAffected(); err != nil || rows != 1 {
			return mcpRefusedUnlessStorage(errMCPDeviceConnectionMissing, err)
		}
		address := origin + nativeMCPPath
		key := deterministicMCPToken(secret, "manual-access", result.response.ID, "lmc_")
		response = mcpDeviceConnectionToken{AccessToken: key, TokenType: "Bearer", MCPURL: address, ConnectionID: result.response.ID,
			ClientName: stored.ClientName, WorkspaceIDs: result.response.WorkspaceIDs,
			Message: strings.Join(append([]string{fmt.Sprintf("Your person allowed “%s”. Add Leafen's MCP to your program with access_token as its key; "+
				"it is shown once here (your person sees it again on the token's page in Leafen's settings), so don't repeat it in the conversation.",
				stored.ClientName)}, mcpAddLines(agentEntryNamesHere(txApp, stored.OwnerID), address, key)...), "\n")}
		return nil
	})
	if errors.Is(err, errMCPDeviceConnectionMissing) {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_grant", "This request already gave its key. Ask to connect again for another.")
	}
	if err != nil {
		return mcpDevicePollRefusal(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	if answer == agentAddToClaudeCode {
		entry, err := claudeCodeEntry(response.MCPURL, response.AccessToken)
		if err != nil {
			return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
				"Couldn't create the MCP connection.", err)
		}
		return event.Blob(http.StatusOK, "application/json", []byte(entry+"\n"))
	}
	return event.JSON(http.StatusOK, response)
}

// normalizeMCPDeviceUserCode reads a request's code as typed or said, with
// or without the server's code before it (server_code.go).
func normalizeMCPDeviceUserCode(typed string) (string, bool) {
	code, _ := splitServerCode(typed, mcpDeviceUserCodeLength)
	if len(code) != mcpDeviceUserCodeLength || strings.ContainsFunc(code, func(r rune) bool { return r < '0' || r > '9' }) {
		return "", false
	}
	return code, true
}

// findWaitingMCPDeviceConnection is the request waiting under code; none
// for a code another server's code begins.
func findWaitingMCPDeviceConnection(app core.App, typed string) (mcpDeviceConnection, bool, error) {
	code, ok := normalizeMCPDeviceUserCode(typed)
	if otherServerCode(app, typed, mcpDeviceUserCodeLength) != "" {
		ok = false
	}
	if !ok {
		return mcpDeviceConnection{}, false, nil
	}
	found := mcpDeviceConnection{}
	err := app.DB().NewQuery(`SELECT ` + mcpDeviceConnectionColumns + ` FROM {{leafen_mcp_device_connections}}
		WHERE [[user_code_hash]] = {:hash} AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).
		Bind(dbx.Params{"hash": mcpTokenHash(code), "now": time.Now().UnixMilli()}).One(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return found, false, nil
	}
	return found, err == nil, err
}
