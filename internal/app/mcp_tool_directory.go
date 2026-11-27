package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpUserDirectoryDefaultPageSize = 100
	mcpUserDirectoryMaximumPageSize = 100
	mcpUserDirectoryCursorVersion   = 1
	mcpUserDirectoryCursorPrefix    = "hmuc1"
	mcpUserDirectoryCursorMaxLength = 1024
	mcpDirectoryQueryMaxRunes       = 200
)

var errMCPUserDirectoryCursorInvalid = errors.New("invalid MCP user directory cursor")

type notionMCPGetUsersInput struct {
	TeamspaceID string `json:"teamspace_id,omitempty" jsonschema:"Optional team from notion-get-teams, or a workspace from its workspaces, choosing the workspace to read"`
	UserID      string `json:"user_id,omitempty" jsonschema:"Specific Notion-compatible user ID, or self"`
	Query       string `json:"query,omitempty" jsonschema:"Optional case-insensitive name or email filter"`
	StartCursor string `json:"start_cursor,omitempty" jsonschema:"Opaque continuation from a prior response"`
	PageSize    int    `json:"page_size,omitzero" jsonschema:"Maximum users to return, from 1 to 100"`
}

type notionMCPGetUsersOutput struct {
	Object     string           `json:"object"`
	Results    []map[string]any `json:"results"`
	NextCursor *string          `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
	Type       string           `json:"type"`
	User       map[string]any   `json:"user"`
}

type mcpUserDirectoryCursor struct {
	Version          int    `json:"v"`
	ConnectionID     string `json:"c"`
	PrincipalID      string `json:"p"`
	WorkspaceID      string `json:"w"`
	PermissionCursor int    `json:"r"`
	QueryHash        string `json:"q"`
	AfterUserID      string `json:"a"`
	Limit            int    `json:"l"`
}

func registerMCPDirectoryTools(
	server *mcp.Server,
	app core.App,
	readOnly *mcp.ToolAnnotations,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-teams", Title: "Get Leafen teams",
		Description: "List the teamspaces of the fixed-scope workspaces, split by membership, with the workspaces themselves.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPGetTeamsInput) (*mcp.CallToolResult, notionMCPGetTeamsOutput, error) {
		return getMCPTeams(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-users", Title: "Get Leafen users",
		Description: "Read one fixed-scope workspace user directory with bounded continuation.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPGetUsersInput) (*mcp.CallToolResult, notionMCPGetUsersOutput, error) {
		return getMCPUsers(ctx, app, input)
	})
}

func getMCPUsers(
	ctx context.Context,
	app core.App,
	input notionMCPGetUsersInput,
) (*mcp.CallToolResult, notionMCPGetUsersOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPGetUsersOutput{}, err
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		TeamspaceID: strings.TrimSpace(input.TeamspaceID), AllowImplicit: true,
	}, permissionWorkspaceRead)
	if err != nil {
		return nil, notionMCPGetUsersOutput{}, err
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	if utf8.RuneCountInString(query) > mcpDirectoryQueryMaxRunes {
		return nil, notionMCPGetUsersOutput{}, mcpInvalid("query is at most %d characters", mcpDirectoryQueryMaxRunes)
	}
	limit := input.PageSize
	if limit == 0 {
		limit = mcpUserDirectoryDefaultPageSize
	}
	if limit < 1 || limit > mcpUserDirectoryMaximumPageSize {
		return nil, notionMCPGetUsersOutput{}, mcpInvalid("page_size is from 1 to %d", mcpUserDirectoryMaximumPageSize)
	}
	userID := strings.TrimSpace(input.UserID)
	if userID != "" {
		if input.StartCursor != "" {
			return nil, notionMCPGetUsersOutput{}, mcpInvalid("user_id asks for one user: leave start_cursor out")
		}
		internalID := userID
		if userID == "self" {
			internalID = authority.PrincipalID
		} else {
			internalID, err = resolveNotionCompatibleUserID(userID)
			if err != nil {
				return nil, notionMCPGetUsersOutput{}, mcpInvalid("user_id is an ID notion-get-users gives, or self")
			}
		}
		user, found, err := loadNotionUserDirectoryUser(
			app, authority.PrincipalID, workspace.WorkspaceID, internalID,
		)
		if err == nil && !found {
			// One a copy brought into the workspace reads by the name it kept (workspace_carried_people.go).
			user, found, err = carriedNotionDirectoryUser(app, authority.PrincipalID, workspace.WorkspaceID, internalID)
		}
		if err != nil {
			return nil, notionMCPGetUsersOutput{}, failedBecause(errMCPToolFailed, err)
		}
		results := []map[string]any{}
		if found && mcpDirectoryUserMatches(user, query) {
			projected, err := notionUserObjectFromFields(
				user.ID, user.Name, user.Avatar, user.Email, user.EmailVerified, requestOriginFromContext(ctx),
			)
			if err != nil {
				return nil, notionMCPGetUsersOutput{}, failedBecause(errMCPToolFailed, err)
			}
			results = append(results, projected)
		}
		return nil, notionMCPGetUsersOutput{
			Object: "list", Results: results, HasMore: false,
			Type: "user", User: map[string]any{},
		}, nil
	}

	queryHash := mcpDirectoryQueryHash(query)
	cursor, err := decodeMCPUserDirectoryCursor(
		app, input.StartCursor, authority, workspace.WorkspaceID, queryHash, limit,
	)
	if err != nil {
		return nil, notionMCPGetUsersOutput{}, mcpInvalid("start_cursor is the next_cursor this tool gave for the same query")
	}
	rows, err := loadNotionUserDirectoryRows(
		ctx, app, authority.PrincipalID, workspace.WorkspaceID,
		cursor.AfterUserID, limit+1, query,
	)
	if err != nil || len(rows) == 0 {
		return nil, notionMCPGetUsersOutput{}, failedBecause(errMCPToolFailed, err)
	}
	permissionCursor := rows[0].PermissionCursor
	if cursor.Version != 0 && cursor.PermissionCursor != permissionCursor {
		return nil, notionMCPGetUsersOutput{}, mcpInvalid("the workspace's people changed since start_cursor: ask again without it")
	}
	results := make([]map[string]any, 0, limit)
	internalIDs := make([]string, 0, limit+1)
	for _, row := range rows {
		if !row.ID.Valid || !row.Email.Valid || !row.EmailVerified.Valid {
			continue
		}
		internalIDs = append(internalIDs, row.ID.String)
		if len(results) == limit {
			continue
		}
		user, err := notionUserObjectFromFields(
			row.ID.String, row.Name.String, row.Avatar.String, row.Email.String, row.EmailVerified.Bool,
			requestOriginFromContext(ctx),
		)
		if err != nil {
			return nil, notionMCPGetUsersOutput{}, failedBecause(errMCPToolFailed, err)
		}
		results = append(results, user)
	}
	hasMore := len(internalIDs) > limit
	var nextCursor *string
	if hasMore {
		encoded, err := encodeMCPUserDirectoryCursor(app, mcpUserDirectoryCursor{
			Version: mcpUserDirectoryCursorVersion, ConnectionID: authority.ConnectionID,
			PrincipalID: authority.PrincipalID, WorkspaceID: workspace.WorkspaceID,
			PermissionCursor: permissionCursor, QueryHash: queryHash,
			AfterUserID: internalIDs[limit-1], Limit: limit,
		})
		if err != nil {
			return nil, notionMCPGetUsersOutput{}, failedBecause(errMCPToolFailed, err)
		}
		nextCursor = &encoded
	}
	return nil, notionMCPGetUsersOutput{
		Object: "list", Results: results, NextCursor: nextCursor, HasMore: hasMore,
		Type: "user", User: map[string]any{},
	}, nil
}

func mcpDirectoryUserMatches(user storedNotionDirectoryUser, query string) bool {
	return query == "" || strings.Contains(strings.ToLower(
		user.Name+"\n"+user.Email+"\n"+user.ID,
	), query)
}

func mcpDirectoryQueryHash(query string) string {
	digest := sha256.Sum256([]byte(query))
	return hex.EncodeToString(digest[:])
}

func encodeMCPUserDirectoryCursor(app core.App, cursor mcpUserDirectoryCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	secret, err := notionOAuthSecret(app)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := mcpUserDirectoryCursorSignature(secret, encoded)
	return mcpUserDirectoryCursorPrefix + "." + encoded + "." +
		base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeMCPUserDirectoryCursor(
	app core.App,
	raw string,
	authority mcpAuthority,
	workspaceID string,
	queryHash string,
	limit int,
) (mcpUserDirectoryCursor, error) {
	if raw == "" {
		return mcpUserDirectoryCursor{}, nil
	}
	if len(raw) > mcpUserDirectoryCursorMaxLength {
		return mcpUserDirectoryCursor{}, errMCPUserDirectoryCursorInvalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != mcpUserDirectoryCursorPrefix {
		return mcpUserDirectoryCursor{}, errMCPUserDirectoryCursorInvalid
	}
	secret, err := notionOAuthSecret(app)
	if err != nil {
		return mcpUserDirectoryCursor{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mcpUserDirectoryCursorSignature(secret, parts[1])) {
		return mcpUserDirectoryCursor{}, errMCPUserDirectoryCursorInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return mcpUserDirectoryCursor{}, errMCPUserDirectoryCursorInvalid
	}
	cursor := mcpUserDirectoryCursor{}
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return mcpUserDirectoryCursor{}, errMCPUserDirectoryCursorInvalid
	}
	canonical, err := json.Marshal(cursor)
	if err != nil || !hmac.Equal(canonical, payload) ||
		cursor.Version != mcpUserDirectoryCursorVersion ||
		cursor.ConnectionID != authority.ConnectionID || cursor.PrincipalID != authority.PrincipalID ||
		cursor.WorkspaceID != workspaceID || cursor.PermissionCursor < 0 ||
		cursor.QueryHash != queryHash || cursor.Limit != limit ||
		!validNotionCompatibleInternalID(cursor.AfterUserID) {
		return mcpUserDirectoryCursor{}, errMCPUserDirectoryCursorInvalid
	}
	return cursor, nil
}

func mcpUserDirectoryCursorSignature(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen:mcp-user-directory-cursor:v1\x00" + payload))
	return mac.Sum(nil)
}
