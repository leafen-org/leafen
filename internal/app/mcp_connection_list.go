package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const mcpConnectionListCursorVersion = 1

var (
	errMCPConnectionListCursorInvalid   = errors.New(errorCodeMCPConnectionListCursorInvalid)
	errMCPConnectionListRevisionChanged = errors.New(errorCodeMCPConnectionListRevisionChanged)
	errMCPConnectionListScopeInvalid    = errors.New(errorCodeMCPConnectionListScopeInvalid)
)

type mcpConnectionListCursor struct {
	Version           int    `json:"v"`
	PrincipalID       string `json:"p"`
	AuthorityRevision int64  `json:"r"`
	AccessSnapshot    string `json:"a"`
	AfterCreated      int64  `json:"c"`
	AfterID           string `json:"i"`
	Limit             int    `json:"l"`
}

type storedMCPConnectionListRow struct {
	ID             string `db:"id"`
	ClientName     string `db:"client_name"`
	Description    string `db:"description"`
	Icon           string `db:"icon"`
	TokenExpiresAt int64  `db:"token_expires_at"`
	// RefreshExpiresAt is when an OAuth connection ends: its access token
	// lasts an hour and is renewed until then (mcp_oauth_tokens.go).
	RefreshExpiresAt int64  `db:"refresh_expires_at"`
	OAuthClientID    string `db:"oauth_client_id"`
	OAuthResource    string `db:"oauth_resource"`
	OAuthScope       string `db:"oauth_scope"`
	AccessScope      string `db:"access_scope"`
	Created          int64  `db:"created"`
	RevokedAt        int64  `db:"revoked_at"`
	LastUsedAt       int64  `db:"last_used_at"`
	ReachedVia       string `db:"reached_via"`
	// Its page's "모든 페이지", off holding it to PageIDs (mcp_token_tools.go).
	AllPages int64  `db:"all_pages"`
	PageIDs  string `db:"page_ids"`
}

type storedMCPConnectionListScope struct {
	ConnectionID   string `db:"connection_id"`
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization_id"`
	Name           string `db:"name"`
	Valid          int    `db:"valid"`
}

func readMCPConnectionList(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	limit, err := parseMCPConnectionListQuery(event.Request)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionListInvalid, "Check the MCP connection query range.")
	}
	response, err := loadMCPConnectionListPage(
		event.App, user.Id, event.Request.URL.Query().Get("cursor"), limit,
	)
	switch {
	case errors.Is(err, errMCPConnectionListCursorInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionListCursorInvalid, "Check the MCP connection query position.")
	case errors.Is(err, errMCPConnectionListRevisionChanged):
		return apiError(event, http.StatusConflict, errorCodeMCPConnectionListRevisionChanged, "The MCP connections changed. Load them again from the start.")
	case errors.Is(err, errMCPConnectionListScopeInvalid):
		return apiError(event, http.StatusConflict, errorCodeMCPConnectionListScopeInvalid, "The MCP connection's workspace scope is invalid. Authenticate again.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeMCPConnectionListFailed,
			"Couldn't load the MCP connection.", "mcp_connection_list", "read",
			errorCodeMCPConnectionListFailed, err,
		)
	default:
		event.Response.Header().Set("Cache-Control", "no-store")
		event.Response.Header().Set("Pragma", "no-cache")
		return event.JSON(http.StatusOK, response)
	}
}

func parseMCPConnectionListQuery(request *http.Request) (int, error) {
	query := request.URL.Query()
	for key, values := range query {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			return 0, errors.New(errorCodeMCPConnectionListInvalid)
		}
	}
	limit := mcpConnectionListDefaultPageSize
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > mcpConnectionListMaximumPageSize {
			return 0, errors.New(errorCodeMCPConnectionListInvalid)
		}
		limit = parsed
	}
	return limit, nil
}

func loadMCPConnectionListPage(
	app core.App,
	principalID string,
	rawCursor string,
	limit int,
) (mcpConnectionListResponse, error) {
	response := mcpConnectionListResponse{Connections: []mcpConnectionListItem{}}
	// A key for all of the person's workspaces shows those they came to be
	// in since it was last used too (user_key_reach_gained.go).
	keepUserKeysReachingAll(app, principalID)
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		cursor, err := decodeMCPConnectionListCursor(rawCursor, principalID, limit)
		if err != nil {
			return err
		}
		if err := txApp.DB().NewQuery(`
			SELECT COALESCE((
				SELECT [[revision]] FROM {{leafen_mcp_connection_owner_revisions}}
				WHERE [[owner_id]] = {:ownerId}
			), 0)
		`).Bind(dbx.Params{"ownerId": principalID}).Row(&response.AuthorityRevision); err != nil {
			return err
		}
		if cursor.Version != 0 && cursor.AuthorityRevision != response.AuthorityRevision {
			return errMCPConnectionListRevisionChanged
		}
		accessSnapshot, err := readMCPConsentMembershipSnapshot(txApp, principalID)
		if err != nil {
			return err
		}
		if cursor.Version != 0 && cursor.AccessSnapshot != accessSnapshot {
			return errMCPConnectionListRevisionChanged
		}
		rows, err := loadMCPConnectionListRows(txApp, principalID, cursor, limit)
		if err != nil {
			return err
		}
		if response.ActiveConnections, err = countActiveMCPConnections(txApp, principalID, time.Now().UnixMilli()); err != nil {
			return err
		}
		response.ConnectionLimit = maxActiveMCPConnectionsPerOwner
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		scopes, organizations, err := loadMCPConnectionListScopes(txApp, rows)
		if err != nil {
			return err
		}
		workspaceIDs := mcpConnectionListWorkspaceIDs(scopes)
		if len(workspaceIDs) > 0 {
			if err := authorizeWorkspaceSet(
				txApp, principalID, workspaceIDs, permissionWorkspaceRead,
			); isPermissionDenied(err) {
				return errMCPConnectionListScopeInvalid
			} else if err != nil {
				return err
			}
		}
		now := time.Now().UnixMilli()
		// A connection that came through a tunnel address the server no
		// longer has connects again (server_tunnel_first_run.go).
		tunnelAddress := currentTunnelAddress(txApp)
		for _, row := range rows {
			workspaces := scopes[row.ID]
			if len(workspaces) == 0 {
				return errMCPConnectionListScopeInvalid
			}
			item, err := mcpConnectionListWireItem(row, workspaces, now)
			if err != nil {
				return err
			}
			// Held to chosen pages, it says how many its page shows, so it reads
			// apart from one reaching every page (2026-11-13: both read "권한 전부").
			if row.AllPages == 0 {
				pages, err := mcpConnectionChosenPages(txApp, row.PageIDs)
				if err != nil {
					return err
				}
				item.Pages = &pages
			}
			// One that came through an earlier address goes on there while it
			// forwards, and may move to the new one; it is to connect again
			// once that address no longer reaches the server (mcp_earlier_address.go).
			if item.Status == "active" && mcpConnectionNeedsReconnect(row.ReachedVia, tunnelAddress) {
				item.Moves = forwardedOrigin(row.ReachedVia) != ""
				item.Reconnect = !item.Moves
			}
			if item.Status == "active" {
				if err := markMCPConnectionListBlocked(txApp, &item, row.OAuthClientID, principalID, organizations); err != nil {
					return err
				}
			}
			response.Connections = append(response.Connections, item)
		}
		if hasMore && len(rows) > 0 {
			last := rows[len(rows)-1]
			next, err := encodeMCPConnectionListCursor(mcpConnectionListCursor{
				Version: mcpConnectionListCursorVersion, PrincipalID: principalID,
				AuthorityRevision: response.AuthorityRevision, AccessSnapshot: accessSnapshot,
				AfterCreated: last.Created, AfterID: last.ID, Limit: limit,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}

// mcpConnectionChosenPages counts the pages a connection is held to that are
// out of the trash with every page above them (pageOutOfTrashPathSQL), the
// ones its token page lists (mcp_token_settings.go).
func mcpConnectionChosenPages(app core.App, pageIDs string) (int, error) {
	ids := parseMCPNameList(pageIDs)
	if len(ids) == 0 {
		return 0, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	count := 0
	err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]")).
		Bind(dbx.Params{"ids": string(encoded)}).Row(&count)
	return count, err
}

func loadMCPConnectionListRows(
	app core.App,
	ownerID string,
	cursor mcpConnectionListCursor,
	limit int,
) ([]storedMCPConnectionListRow, error) {
	rows := []storedMCPConnectionListRow{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[client_name]], [[description]], [[icon]], [[token_expires_at]], [[refresh_expires_at]], [[oauth_client_id]],
			[[oauth_resource]], [[oauth_scope]], [[access_scope]], [[created]], [[revoked_at]], [[last_used_at]],
			[[reached_via]], [[all_pages]], [[page_ids]]
		FROM {{leafen_mcp_connections}} INDEXED BY [[idx_leafen_mcp_connections_owner_created]]
		WHERE [[owner_id]] = {:ownerId} AND [[app_install_id]] = '' AND [[request_id]] NOT LIKE 'agent-session:%' AND (
			{:afterId} = '' OR [[created]] < {:afterCreated}
			OR ([[created]] = {:afterCreated} AND [[id]] < {:afterId})
		)
		ORDER BY [[created]] DESC, [[id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"ownerId": ownerID, "afterCreated": cursor.AfterCreated,
		"afterId": cursor.AfterID, "limit": limit + 1,
	}).All(&rows)
	return rows, err
}

func loadMCPConnectionListScopes(
	app core.App,
	connections []storedMCPConnectionListRow,
) (map[string][]mcpConnectionListWorkspace, map[string]string, error) {
	result := make(map[string][]mcpConnectionListWorkspace, len(connections))
	organizations := map[string]string{}
	if len(connections) == 0 {
		return result, organizations, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(connections))
	for index, connection := range connections {
		key := "connection" + strconv.Itoa(index)
		params[key] = connection.ID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	// Every workspace of the window's connections, however many.
	rows := []storedMCPConnectionListScope{}
	if err := app.DB().NewQuery(`
		SELECT [[scope]].[[connection_id]], [[scope]].[[workspace_id]], COALESCE([[workspace]].[[name]], '') AS [[name]],
			COALESCE([[workspace]].[[organization]], '') AS [[organization_id]],
			CASE WHEN [[workspace]].[[id]] IS NULL THEN 0 ELSE 1 END AS [[valid]]
		FROM {{leafen_mcp_connection_workspaces}} [[scope]]
		LEFT JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
		WHERE [[scope]].[[connection_id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[scope]].[[connection_id]], [[scope]].[[workspace_id]]
	`).Bind(params).All(&rows); err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		if row.Valid != 1 || row.ConnectionID == "" || row.WorkspaceID == "" {
			return nil, nil, errMCPConnectionListScopeInvalid
		}
		result[row.ConnectionID] = append(result[row.ConnectionID], mcpConnectionListWorkspace{ID: row.WorkspaceID, Name: row.Name})
		organizations[row.WorkspaceID] = row.OrganizationID
	}
	return result, organizations, nil
}

// markMCPConnectionListBlocked marks the workspaces whose administrators
// haven't approved this kind of connection, by the same check the agent's
// calls meet (mcp_connections.go), so the person sees why it can't work there.
func markMCPConnectionListBlocked(
	app core.App,
	item *mcpConnectionListItem,
	oauthClientID string,
	principalID string,
	organizations map[string]string,
) error {
	clientID := oauthClientID
	if clientID == "" {
		clientID = mcpGovernanceManualClientID
	}
	scoped := make([]mcpGovernanceWorkspace, 0, len(item.Workspaces))
	for _, workspace := range item.Workspaces {
		scoped = append(scoped, mcpGovernanceWorkspace{
			ID: workspace.ID, OrganizationID: organizations[workspace.ID], Name: workspace.Name,
		})
	}
	approved, err := filterMCPClientApprovedWorkspaces(app, scoped, clientID, principalID)
	if err != nil {
		return err
	}
	allowed := make(map[string]bool, len(approved))
	for _, workspace := range approved {
		allowed[workspace.ID] = true
	}
	for index := range item.Workspaces {
		item.Workspaces[index].Blocked = !allowed[item.Workspaces[index].ID]
	}
	return nil
}

func mcpConnectionListWireItem(
	row storedMCPConnectionListRow,
	workspaces []mcpConnectionListWorkspace,
	now int64,
) (mcpConnectionListItem, error) {
	workspaceIDs := make([]string, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspaceIDs = append(workspaceIDs, workspace.ID)
	}
	if row.ID == "" || row.ClientName == "" || row.Created <= 0 || row.RevokedAt < 0 || row.TokenExpiresAt < 0 {
		return mcpConnectionListItem{}, errMCPConnectionListScopeInvalid
	}
	clientID := row.OAuthClientID
	resource := row.OAuthResource
	scopes := []string{mcpOAuthScope}
	if row.OAuthScope != "" {
		scopes = []string{row.OAuthScope}
	}
	if clientID == "" {
		clientID = row.ID
		// A key made by hand may be made to expire.
		if resource != "" || row.OAuthScope != "" {
			return mcpConnectionListItem{}, errMCPConnectionListScopeInvalid
		}
	} else if resource == "" || !oauthScopeFitsResource(resource, row.OAuthScope) || row.TokenExpiresAt == 0 {
		return mcpConnectionListItem{}, errMCPConnectionListScopeInvalid
	}
	status := "active"
	var expiresAt, revokedAt *int64
	// When the person has to connect again: an OAuth connection's refresh
	// end, not its hour-long access token (2026-10-16: a connection made that
	// day read as ending that day); a key's own end.
	ends := row.TokenExpiresAt
	if row.OAuthClientID != "" && row.RefreshExpiresAt > 0 {
		ends = row.RefreshExpiresAt
	}
	if ends > 0 {
		value := ends
		expiresAt = &value
	}
	var lastUsedAt *int64
	if row.LastUsedAt > 0 {
		value := row.LastUsedAt
		lastUsedAt = &value
	}
	if row.RevokedAt > 0 {
		value := row.RevokedAt
		revokedAt = &value
		status = "revoked"
	} else if ends > 0 && ends <= now {
		status = "revoked"
	}
	return mcpConnectionListItem{
		ID: row.ID, ClientID: clientID, ClientName: row.ClientName, Description: row.Description, Icon: row.Icon,
		Resource: resource, Scopes: scopes, AccessScope: mcpConnectionListAppScope(row), WorkspaceIDs: workspaceIDs, Workspaces: workspaces,
		Status: status, CreatedAt: row.Created, ExpiresAt: expiresAt, RevokedAt: revokedAt, LastUsedAt: lastUsedAt,
	}, nil
}

func mcpConnectionListWorkspaceIDs(scopes map[string][]mcpConnectionListWorkspace) []string {
	unique := map[string]struct{}{}
	for _, workspaces := range scopes {
		for _, workspace := range workspaces {
			unique[workspace.ID] = struct{}{}
		}
	}
	workspaceIDs := make([]string, 0, len(unique))
	for workspaceID := range unique {
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)
	return workspaceIDs
}

func encodeMCPConnectionListCursor(cursor mcpConnectionListCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	result := base64.RawURLEncoding.EncodeToString(encoded)
	if len(result) > mcpConnectionListCursorBytes {
		return "", errMCPConnectionListCursorInvalid
	}
	return result, nil
}

func decodeMCPConnectionListCursor(
	raw string,
	principalID string,
	limit int,
) (mcpConnectionListCursor, error) {
	if raw == "" {
		return mcpConnectionListCursor{}, nil
	}
	if len(raw) > mcpConnectionListCursorBytes {
		return mcpConnectionListCursor{}, errMCPConnectionListCursorInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return mcpConnectionListCursor{}, errMCPConnectionListCursorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	cursor := mcpConnectionListCursor{}
	if err := decoder.Decode(&cursor); err != nil {
		return mcpConnectionListCursor{}, errMCPConnectionListCursorInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return mcpConnectionListCursor{}, errMCPConnectionListCursorInvalid
	}
	canonical, err := encodeMCPConnectionListCursor(cursor)
	if err != nil || canonical != raw || cursor.Version != mcpConnectionListCursorVersion ||
		cursor.PrincipalID != principalID || cursor.AuthorityRevision < 1 ||
		cursor.AccessSnapshot == "" || cursor.AfterCreated <= 0 ||
		cursor.AfterID == "" || cursor.Limit != limit {
		return mcpConnectionListCursor{}, errMCPConnectionListCursorInvalid
	}
	return cursor, nil
}

// mcpConnectionListAppScope is what an outside app granted the native API
// may do, as its grant says; "" for a person's own connection, which does
// what its person may (connection_person_rights.go).
func mcpConnectionListAppScope(row storedMCPConnectionListRow) string {
	if row.OAuthClientID != "" && oauthResourceIsNative(row.OAuthResource) {
		return row.AccessScope
	}
	return ""
}
