package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Outside services a person connects to Leafen once, by their remote MCP
// server's address, so every agent connected to Leafen uses them through it.
// The server signs in for the person — OAuth with dynamic client
// registration and PKCE, headers the person gives (mcp_service_headers.go),
// or nothing — and keeps the sign-in sealed with the settings key; no agent
// sees it. It keeps the
// service's tools as last read, and nothing of the service's content.

const (
	mcpServicesTable          = "leafen_mcp_services"
	mcpServiceAuthNone        = "none"
	mcpServiceAuthToken       = "token"
	mcpServiceAuthOAuth       = "oauth"
	mcpServiceStatusSigningIn = "signing_in"
	mcpServiceStatusConnected = "connected"
	mcpServiceStatusSignIn    = "sign_in_again"
	mcpServiceStatusFailed    = "unreachable"
	// A person connects as many services as they like; lists read them a
	// window at a time, the latest connected first.
	mcpServicesWindow = 50
	// A service's tools are all kept, up to a safety bound far past any
	// service seen; one that lists more is refused, saying so, rather than
	// kept in part.
	mcpServiceToolsSafety      = 10_000
	mcpServiceToolsSafetyBytes = 16 << 20

	errorCodeMCPServiceNotFound      = "LEAFEN_MCP_SERVICE_NOT_FOUND"
	errorCodeMCPServiceInvalid       = "LEAFEN_MCP_SERVICE_INVALID"
	errorCodeMCPServiceUnsafeAddress = "LEAFEN_MCP_SERVICE_UNSAFE_ADDRESS"
	errorCodeMCPServiceTokenNeeded   = "LEAFEN_MCP_SERVICE_TOKEN_NEEDED"
	errorCodeMCPServiceClientNeeded  = "LEAFEN_MCP_SERVICE_CLIENT_NEEDED"
	errorCodeMCPServiceTokenRefused  = "LEAFEN_MCP_SERVICE_TOKEN_REFUSED"
	errorCodeMCPServiceUnsafeSignIn  = "LEAFEN_MCP_SERVICE_UNSAFE_SIGN_IN"
	errorCodeMCPServiceFailed        = "LEAFEN_MCP_SERVICE_FAILED"
	errorCodeMCPServiceTooManyTools  = "LEAFEN_MCP_SERVICE_TOO_MANY_TOOLS"
	// The service reads Leafen at an address it can't reach (mcp_service_oauth_reach.go).
	errorCodeMCPServiceAddressUnreachable = "LEAFEN_MCP_SERVICE_ADDRESS_UNREACHABLE"
	// The service refused Leafen's registration (mcp_service_oauth_register.go).
	errorCodeMCPServiceRegistrationRefused = "LEAFEN_MCP_SERVICE_REGISTRATION_REFUSED"
)

var (
	errMCPServiceNotFound = errors.New(errorCodeMCPServiceNotFound)
	errMCPServiceInvalid  = errors.New(errorCodeMCPServiceInvalid)
	errMCPServiceFailed   = errors.New(errorCodeMCPServiceFailed)
	errMCPServiceTooMany  = errors.New("the service lists more tools than Leafen keeps")
)

// Migration 490's sealed columns, frozen; the headers came with 511.
var mcpServiceCipherColumns490 = []administratorCipherColumn{
	{mcpServicesTable, "client_secret"},
	{mcpServicesTable, "access"},
	{mcpServicesTable, "refresh"},
	{mcpServicesTable, "verifier"},
}

var mcpServiceCipherColumns = append(append([]administratorCipherColumn{}, mcpServiceCipherColumns490...),
	administratorCipherColumn{mcpServicesTable, "headers"})

// storedMCPService is a service row as kept, its sealed values still sealed.
type storedMCPService struct {
	ID                    string `db:"id"`
	UserID                string `db:"user_id"`
	URL                   string `db:"url"`
	Name                  string `db:"name"`
	Version               string `db:"version"`
	Auth                  string `db:"auth"`
	Status                string `db:"status"`
	Failure               string `db:"failure"`
	Issuer                string `db:"issuer"`
	AuthorizationEndpoint string `db:"authorization_endpoint"`
	TokenEndpoint         string `db:"token_endpoint"`
	RevocationEndpoint    string `db:"revocation_endpoint"`
	Resource              string `db:"resource"`
	Scope                 string `db:"scope"`
	ClientID              string `db:"client_id"`
	ClientSecret          string `db:"client_secret"`
	ClientAuth            int    `db:"client_auth"`
	Access                string `db:"access"`
	Headers               string `db:"headers"`
	Refresh               string `db:"refresh"`
	ExpiresAt             int64  `db:"expires_at"`
	RedirectURI           string `db:"redirect_uri"`
	// How the server is a client of the service, and one made anew for a
	// sign-in under way (mcp_service_address_change.go).
	ClientKind  string `db:"client_kind"`
	PendingPlan string `db:"pending_plan"`
	AlwaysAllow bool   `db:"always_allow"`
	ToolCount   int    `db:"tool_count"`
	CheckedAt   int64  `db:"checked_at"`
	ConnectedAt int64  `db:"connected_at"`
	CreatedAt   int64  `db:"created_at"`
}

// A service's name is the one its person gave, or else the one it announced.
const mcpServiceNameSQL = `CASE WHEN [[given_name]] <> '' THEN [[given_name]] ELSE [[name]] END`

const storedMCPServiceColumns = `[[id]], [[user_id]], [[url]], ` + mcpServiceNameSQL + ` AS [[name]], [[version]], [[auth]], [[status]],
	[[failure]], [[issuer]], [[authorization_endpoint]], [[token_endpoint]], [[revocation_endpoint]], [[resource]], [[scope]], [[client_id]],
	[[client_secret]], [[client_auth]], [[access]], [[headers]], [[refresh]], [[expires_at]], [[redirect_uri]], [[client_kind]], [[pending_plan]], [[always_allow]],
	` + mcpServiceToolCountSQL + ` AS [[tool_count]], [[checked_at]], [[connected_at]], [[created_at]]`

// A service's tools for its agents: those its person hasn't turned off
// (mcp_service_tool_choices.go). The rows read are named [[service]].
const mcpServiceToolCountSQL = `json_array_length([[service]].[[tools]]) - (SELECT COUNT(*) FROM {{leafen_mcp_service_tool_choices}} [[choice]]
	WHERE [[choice]].[[service_id]] = [[service]].[[id]] AND [[choice]].[[enabled]] = 0 AND EXISTS (
		SELECT 1 FROM json_each([[service]].[[tools]]) AS [[listed]] WHERE json_extract([[listed]].[[value]], '$.name') = [[choice]].[[tool]]))`

// MCPServiceTool is one of a service's tools as the service described it.
type MCPServiceTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	ReadOnly     bool            `json:"readOnly"`
	Destructive  bool            `json:"destructive"`
}

// MCPService is a connected service as its person sees it.
type MCPService struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Auth        string `json:"auth"`
	Status      string `json:"status"`
	Failure     string `json:"failure,omitempty"`
	AlwaysAllow bool   `json:"alwaysAllow"`
	ToolCount   int    `json:"toolCount"`
	CheckedAt   int64  `json:"checkedAt"`
	ConnectedAt int64  `json:"connectedAt"`
	CreatedAt   int64  `json:"createdAt"`
	// After an address change, a client the person made on the service: the
	// return address it has, the one to add, and whether they said it is
	// (mcp_service_address_change.go).
	EarlierCallback string `json:"earlierCallback,omitempty"`
	Callback        string `json:"callback,omitempty"`
	CallbackAdded   bool   `json:"callbackAdded,omitzero"`
}

// view is the service as its person sees it: how many tools it has, which
// are read a window at a time (mcpServiceToolWindow) or one by name.
func (stored storedMCPService) view() MCPService {
	return MCPService{
		ID: stored.ID, URL: stored.URL, Name: stored.Name, Version: stored.Version, Auth: stored.Auth, Status: stored.Status,
		Failure: stored.Failure, AlwaysAllow: stored.AlwaysAllow, ToolCount: stored.ToolCount, CheckedAt: stored.CheckedAt, ConnectedAt: stored.ConnectedAt, CreatedAt: stored.CreatedAt,
	}
}

// mcpServiceToolWindow reads up to limit of a service's tools from the one
// at from on, in the service's order, with how its person set each (only
// those left on, for agents); next is where the next window starts, 0 at
// the end.
func mcpServiceToolWindow(app core.App, serviceID string, from, limit int, onOnly bool) ([]MCPServiceToolSetting, int, error) {
	rows := []struct {
		Position int    `db:"position"`
		Value    string `db:"value"`
		Enabled  bool   `db:"enabled"`
		Run      string `db:"run"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[tool]].[[key]] AS [[position]], [[tool]].[[value]] AS [[value]], COALESCE([[choice]].[[enabled]], 1) AS [[enabled]],
			COALESCE([[choice]].[[run]], '') AS [[run]]
		FROM {{leafen_mcp_services}} [[service]], json_each([[service]].[[tools]]) AS [[tool]]
		LEFT JOIN {{leafen_mcp_service_tool_choices}} [[choice]]
			ON [[choice]].[[service_id]] = [[service]].[[id]] AND [[choice]].[[tool]] = json_extract([[tool]].[[value]], '$.name')
		WHERE [[service]].[[id]] = {:id} AND [[tool]].[[key]] >= {:from} AND (NOT {:onOnly} OR COALESCE([[choice]].[[enabled]], 1) = 1)
		ORDER BY [[tool]].[[key]] LIMIT {:limit}
	`).Bind(dbx.Params{"id": serviceID, "from": from, "limit": limit + 1, "onOnly": onOnly}).All(&rows); err != nil {
		return nil, 0, err
	}
	next := 0
	if len(rows) > limit {
		rows, next = rows[:limit], rows[limit].Position
	}
	tools := make([]MCPServiceToolSetting, 0, len(rows))
	for _, row := range rows {
		tool := MCPServiceToolSetting{Enabled: row.Enabled}
		if err := json.Unmarshal([]byte(row.Value), &tool.MCPServiceTool); err != nil {
			return nil, 0, err
		}
		tool.Run = mcpServiceToolRun(tool.MCPServiceTool, row.Run)
		tools = append(tools, tool)
	}
	return tools, next, nil
}

// mcpServiceToolNamed finds one of a service's tools by its name.
func mcpServiceToolNamed(app core.App, serviceID, name string) (MCPServiceTool, bool, error) {
	var raw string
	err := app.DB().NewQuery(`
		SELECT [[tool]].[[value]] FROM {{leafen_mcp_services}} [[service]], json_each([[service]].[[tools]]) AS [[tool]]
		WHERE [[service]].[[id]] = {:id} AND json_extract([[tool]].[[value]], '$.name') = {:name}
		ORDER BY [[tool]].[[key]] LIMIT 1
	`).Bind(dbx.Params{"id": serviceID, "name": strings.TrimSpace(name)}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPServiceTool{}, false, nil
	}
	if err != nil {
		return MCPServiceTool{}, false, err
	}
	tool := MCPServiceTool{}
	return tool, true, json.Unmarshal([]byte(raw), &tool)
}

func loadMCPService(app core.App, userID, serviceID string) (storedMCPService, error) {
	stored := storedMCPService{}
	err := app.DB().NewQuery(`SELECT ` + storedMCPServiceColumns + ` FROM {{leafen_mcp_services}} [[service]]
		WHERE [[id]] = {:id} AND [[user_id]] = {:userId}`).Bind(dbx.Params{"id": serviceID, "userId": userID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, errMCPServiceNotFound
	}
	return stored, err
}

// ListMCPServices is a window of the person's services, the latest
// connected first, from the cursor on; next goes on from there, "" once all
// are listed.
func ListMCPServices(app core.App, userID, cursor string) ([]MCPService, string, error) {
	return listMCPServiceWindow(app, userID, "", cursor, mcpServicesWindow)
}

// mcpServiceConnectionFilterSQL keeps the services a connection may use:
// every one, or the chosen ones (none named: every one).
const mcpServiceConnectionFilterSQL = `({:connectionId} = '' OR NOT EXISTS (
		SELECT 1 FROM {{leafen_mcp_connections}} [[connection]]
		WHERE [[connection]].[[id]] = {:connectionId} AND [[connection]].[[service_choice]] = 'chosen'
	) OR EXISTS (
		SELECT 1 FROM {{leafen_mcp_connection_services}} [[chosen]]
		WHERE [[chosen]].[[connection_id]] = {:connectionId} AND [[chosen]].[[service_id]] = [[service]].[[id]]
	))`

// listMCPServiceWindow is a window of the person's services (those the
// connection may use, when one is named), the latest connected first.
func listMCPServiceWindow(app core.App, userID, connectionID, cursor string, limit int) ([]MCPService, string, error) {
	after, err := decodeTimeIDCursor(cursor)
	if err != nil {
		return nil, "", errMCPServiceInvalid
	}
	rows := []storedMCPService{}
	if err := app.DB().NewQuery(`SELECT ` + storedMCPServiceColumns + ` FROM {{leafen_mcp_services}} [[service]]
		WHERE [[service]].[[user_id]] = {:userId} AND ` + mcpServiceConnectionFilterSQL + `
			AND ({:afterId} = '' OR [[service]].[[created_at]] < {:afterAt}
				OR ([[service]].[[created_at]] = {:afterAt} AND [[service]].[[id]] > {:afterId}))
		ORDER BY [[service]].[[created_at]] DESC, [[service]].[[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"userId": userID, "connectionId": connectionID, "afterAt": after.At, "afterId": after.ID, "limit": limit + 1}).
		All(&rows); err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeTimeIDCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	services := make([]MCPService, 0, len(rows))
	for _, row := range rows {
		services = append(services, row.view())
	}
	return services, next, nil
}

// loadConnectionMCPService is the person's service by its id, when the
// connection may use it.
func loadConnectionMCPService(app core.App, authority mcpAuthority, serviceID string) (MCPService, bool, error) {
	stored := storedMCPService{}
	err := app.DB().NewQuery(`SELECT ` + storedMCPServiceColumns + ` FROM {{leafen_mcp_services}} [[service]]
		WHERE [[service]].[[id]] = {:id} AND [[service]].[[user_id]] = {:userId} AND ` + mcpServiceConnectionFilterSQL).
		Bind(dbx.Params{"id": serviceID, "userId": authority.owner(), "connectionId": authority.ConnectionID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPService{}, false, nil
	}
	return stored.view(), err == nil, err
}

// mcpServiceKeysFor is every service's key for agents (mcpServiceKeys),
// reading all the person's service names a batch at a time: a key depends
// on the older services of the same name.
func mcpServiceKeysFor(app core.App, userID string) (map[string]string, error) {
	named := []MCPService{}
	after := timeIDCursor{}
	for {
		batch := []struct {
			ID        string `db:"id"`
			Name      string `db:"name"`
			CreatedAt int64  `db:"created_at"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[id]], ` + mcpServiceNameSQL + ` AS [[name]], [[created_at]] FROM {{leafen_mcp_services}}
			WHERE [[user_id]] = {:userId} AND ({:afterId} = '' OR [[created_at]] < {:afterAt}
				OR ([[created_at]] = {:afterAt} AND [[id]] > {:afterId}))
			ORDER BY [[created_at]] DESC, [[id]] LIMIT 500`).
			Bind(dbx.Params{"userId": userID, "afterAt": after.At, "afterId": after.ID}).All(&batch); err != nil {
			return nil, err
		}
		for _, service := range batch {
			named = append(named, MCPService{ID: service.ID, Name: service.Name})
		}
		if len(batch) < 500 {
			return mcpServiceKeys(named), nil
		}
		last := batch[len(batch)-1]
		after = timeIDCursor{At: last.CreatedAt, ID: last.ID}
	}
}

// saveMCPServiceCheck keeps what reading the service found: its name, its
// tools and whether it answered.
func saveMCPServiceCheck(app core.App, serviceID string, found mcpServiceReading, failure error) error {
	now := time.Now().UnixMilli()
	if failure != nil {
		status := mcpServiceStatusFailed
		if errors.Is(failure, errMCPServiceSignIn) {
			status = mcpServiceStatusSignIn
		}
		_, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[status]] = {:status}, [[failure]] = {:failure},
			[[checked_at]] = {:now}, [[updated_at]] = {:now} WHERE [[id]] = {:id}`).Bind(dbx.Params{
			"id": serviceID, "status": status, "failure": mcpServiceFailureText(failure), "now": now,
		}).Execute()
		return err
	}
	tools, err := json.Marshal(found.Tools)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[status]] = 'connected', [[failure]] = '',
		[[name]] = {:name}, [[version]] = {:version}, [[tools]] = {:tools}, [[checked_at]] = {:now}, [[updated_at]] = {:now},
		[[connected_at]] = CASE WHEN [[connected_at]] = 0 THEN {:now} ELSE [[connected_at]] END WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"id": serviceID, "name": appOneLine(found.Name, 120), "version": appOneLine(found.Version, 40), "tools": string(tools), "now": now,
	}).Execute()
	return err
}

// mcpServiceFailureText is the short reason a person reads; the cause
// itself goes to the log.
func mcpServiceFailureText(err error) string {
	switch {
	case errors.Is(err, errMCPServiceSignIn):
		return "The service asks to log in again."
	case errors.Is(err, errOutboundUnsafeDestination):
		return "The address reaches a place the server doesn't connect to."
	case errors.Is(err, errMCPServiceTooMany):
		return fmt.Sprintf("The service lists more tools than Leafen keeps (%d tools or %d MiB of their descriptions).",
			mcpServiceToolsSafety, mcpServiceToolsSafetyBytes>>20)
	case errors.Is(err, errMCPServicePolicyBlocked):
		return mcpServicePolicyBlockedText
	case errors.Is(err, errMCPServiceSignInUnavailable):
		return "The service's login server isn't answering right now, so Leafen couldn't renew the login. Try again in a while."
	default:
		return "The service didn't answer as an MCP server."
	}
}

// DeleteMCPService disconnects a service: the server asks the service to
// revoke its tokens when it says how, then forgets the sign-in and the row.
func DeleteMCPService(app core.App, userID, serviceID string) error {
	stored, err := loadMCPService(app, userID, serviceID)
	if err != nil {
		return err
	}
	revokeMCPServiceTokens(app, stored)
	_, err = app.DB().Delete(mcpServicesTable, dbx.HashExp{"id": stored.ID, "user_id": userID}).Execute()
	return err
}

// normalizeMCPServiceURL is the service's address as kept: https (or the
// loopback address allowed for checks), no credentials, no fragment.
func normalizeMCPServiceURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if loopback := mcpServiceAllowedLoopback(); loopback != "" {
		if parsed, err := parseMCPServiceLoopbackURL(trimmed, loopback); err == nil {
			return parsed, nil
		}
	}
	normalized, err := normalizeOutboundPublicURL(trimmed, 2048, true)
	if err != nil {
		if errors.Is(err, errOutboundUnsafeDestination) {
			return "", err
		}
		return "", errMCPServiceInvalid
	}
	if !strings.HasPrefix(normalized, "https://") {
		return "", errMCPServiceInvalid
	}
	return normalized, nil
}
