package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/pocketbase/core"
)

// The Notion-compatible API and MCP stay at the Notion API versions they
// support now — notionCurrentAPIVersion for the public API (and
// notionPreviousAPIVersion, turned into it at the door) and
// notionAdminAPIVersion for the admin API — and don't follow newer ones.
// Agents and new programs use Leafen's own API and MCP; imports read Notion
// at the versions the provider client names (notion_provider*.go).
const (
	notionCurrentAPIVersion       = "2026-03-11"
	notionErrorMissingVersion     = "missing_version"
	notionErrorValidation         = "validation_error"
	notionErrorRestrictedResource = "restricted_resource"
)

type notionOAuthBearerIdentity struct {
	PrincipalID     string
	ClientID        string
	GrantID         string
	GrantGeneration int
	WorkspaceID     string
	Scopes          []string
	ResourceIDs     map[string]string
	// PageScoped is a connection that reaches only the pages its person
	// chose (ResourceIDs, under them) and the pages it makes, as Notion's
	// integrations do; a personal access token reaches what its person
	// can.
	PageScoped bool
	IssuedAt   int64
	// Origin is the address the request came by, for the avatars it reads.
	Origin string
}

func parseNotionOAuthAccessClaims(event *core.RequestEvent, token string) (notionOAuthAccessClaims, error) {
	secret, err := notionOAuthSecret(event.App)
	if err != nil {
		return notionOAuthAccessClaims{}, err
	}
	// Issued at an earlier address that forwards, it holds at the new one,
	// and the other way round (mcp_earlier_address.go).
	origin, err := answeredAtOrigin(event.Request)
	if err != nil {
		return notionOAuthAccessClaims{}, err
	}
	claims := notionOAuthAccessClaims{}
	parsed, err := jwt.ParseWithClaims(
		token,
		&claims,
		func(candidate *jwt.Token) (any, error) {
			if candidate.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("invalid Notion OAuth signing method")
			}
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid || !notionOAuthIssuedHere(claims.RegisteredClaims, origin) || claims.Type != notionOAuthAccessTokenType ||
		claims.Subject == "" || claims.GrantID == "" || claims.ClientID == "" ||
		claims.WorkspaceID == "" || claims.GrantGeneration < 1 {
		return notionOAuthAccessClaims{}, errors.New("invalid Notion OAuth access token")
	}
	return claims, nil
}

func resolveNotionOAuthBearer(event *core.RequestEvent, rawToken string) (notionOAuthBearerIdentity, bool, error) {
	claims, err := parseNotionOAuthAccessClaims(event, rawToken)
	if err != nil {
		return notionOAuthBearerIdentity{}, false, nil
	}
	grant, found, err := findNotionOAuthGrantByID(event.App, claims.GrantID)
	if err != nil {
		return notionOAuthBearerIdentity{}, false, err
	}
	if !found || grant.Status != "active" || grant.OwnerID != claims.Subject ||
		grant.ClientID != claims.ClientID || grant.WorkspaceID != claims.WorkspaceID ||
		grant.Scope != claims.Scope || grant.CredentialGeneration != claims.GrantGeneration {
		return notionOAuthBearerIdentity{}, false, nil
	}
	if err := authorizeWorkspaceSet(event.App, grant.OwnerID, []string{grant.WorkspaceID}, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return notionOAuthBearerIdentity{}, false, nil
		}
		return notionOAuthBearerIdentity{}, false, err
	}
	resources, err := loadNotionOAuthGrantResources(event.App, grant.ID)
	if err != nil {
		return notionOAuthBearerIdentity{}, false, err
	}
	issuedAt := int64(0)
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Unix()
	}
	return notionOAuthBearerIdentity{
		PrincipalID: grant.OwnerID, ClientID: grant.ClientID, GrantID: grant.ID,
		GrantGeneration: grant.CredentialGeneration,
		WorkspaceID:     grant.WorkspaceID, Scopes: strings.Fields(grant.Scope),
		ResourceIDs: resources, PageScoped: true, IssuedAt: issuedAt,
	}, true, nil
}

func loadNotionOAuthGrantResources(app core.App, grantID string) (map[string]string, error) {
	type row struct {
		ResourceID   string `db:"resource_id"`
		ResourceKind string `db:"resource_kind"`
	}
	rows := []row{}
	err := app.DB().NewQuery(`
		SELECT [[resource_id]], [[resource_kind]]
		FROM {{leafen_notion_oauth_grant_resources}}
		WHERE [[grant_id]]={:grantId} ORDER BY [[resource_id]]
	`).Bind(map[string]any{"grantId": grantID}).All(&rows)
	if err != nil {
		return nil, err
	}
	resources := make(map[string]string, len(rows))
	for _, row := range rows {
		resources[row.ResourceID] = row.ResourceKind
	}
	return resources, nil
}

func notionOAuthBearerFromRequest(event *core.RequestEvent) (notionOAuthBearerIdentity, bool, error) {
	parts := strings.Fields(strings.TrimSpace(event.Request.Header.Get("Authorization")))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return notionOAuthBearerIdentity{}, false, nil
	}
	return resolveNotionOAuthBearer(event, parts[1])
}

func notionOAuthHasScope(identity notionOAuthBearerIdentity, required string) bool {
	for _, scope := range identity.Scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func serveNotionOAuthUsersMe(event *core.RequestEvent) error {
	identity, authorized, err := authorizeNotionUsersMe(event)
	if err != nil || !authorized {
		return err
	}
	user, err := event.App.FindRecordById("users", identity.PrincipalID)
	if err != nil {
		return notionUnauthorized(event)
	}
	// The owner's email shows to a token that may read people's emails
	// (notionIdentityReadsPeople), as Notion's user information capability
	// decides it.
	response, err := notionUserObjectFromFields(user.Id, user.GetString("name"), user.GetString("profile_avatar"),
		notionIdentityEmail(event.App, identity, user.Email()), user.Verified(), requestOrigin(event.Request))
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	// A connection answers as its own bot, which that person connected; a
	// personal access token, which acts as the person who made it, as the
	// token's own bot, which that person owns.
	if identity.ClientID == "personal-access-token" {
		response, err = notionPersonalAccessTokenBotUser(event.App, identity.GrantID, response)
	} else {
		response, err = notionOAuthBotUser(event.App, identity.GrantID, response)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func revokeNotionOAuthToken(event *core.RequestEvent) error {
	body, err := bindNotionOAuthJSON(event)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	client, err := authenticateNotionOAuthClient(event.Request, body)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	token := notionOAuthString(body, "token")
	if token == "" {
		return writeNotionOAuthError(event, notionOAuthFailure(http.StatusBadRequest, "invalid_request", "token is required."))
	}
	grantID := ""
	if refresh, found, findErr := findNotionOAuthRefreshByHash(event.App, notionOAuthTokenHash(token)); findErr != nil {
		return writeNotionOAuthError(event, findErr)
	} else if found && refresh.ClientID == client.ID {
		grantID = refresh.GrantID
	} else if claims, parseErr := parseNotionOAuthAccessClaims(event, token); parseErr == nil && claims.ClientID == client.ID {
		grantID = claims.GrantID
	}
	if grantID != "" {
		if err := event.App.RunInTransaction(func(txApp core.App) error {
			grant, found, err := findNotionOAuthGrantByID(txApp, grantID)
			if err != nil || !found || grant.ClientID != client.ID {
				return err
			}
			return revokeNotionOAuthGrant(txApp, grantID, time.Now().UnixMilli())
		}); err != nil {
			return writeNotionOAuthError(event, err)
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusOK, map[string]any{})
}

func introspectNotionOAuthToken(event *core.RequestEvent) error {
	body, err := bindNotionOAuthJSON(event)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	client, err := authenticateNotionOAuthClient(event.Request, body)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	token := notionOAuthString(body, "token")
	if token == "" {
		return writeNotionOAuthError(event, notionOAuthFailure(http.StatusBadRequest, "invalid_request", "token is required."))
	}
	identity, active, err := resolveNotionOAuthBearer(event, token)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	if active && identity.ClientID != client.ID {
		active = false
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	// In the order Notion's reference gives: active, scope, iat.
	response := notionOrderedObject{{key: "active", value: active}}
	if active {
		response = append(response, notionOrderedMember{key: "scope", value: strings.Join(identity.Scopes, " ")},
			notionOrderedMember{key: "iat", value: identity.IssuedAt})
	}
	return event.JSON(http.StatusOK, response)
}
