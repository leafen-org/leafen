package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	mcpOAuthRequestLifetime = time.Duration(mcpOAuthRequestLifetimeMilliseconds) * time.Millisecond
)

var mcpPKCEChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var errMCPOAuthConsentSnapshotChanged = errors.New("mcp oauth consent snapshot changed")

type mcpOAuthAuthorizationRequest struct {
	ID            string `json:"id"`
	ClientID      string `json:"clientId"`
	RedirectURI   string `json:"redirectUri"`
	Resource      string `json:"resource"`
	Scope         string `json:"scope"`
	CodeChallenge string `json:"codeChallenge"`
	State         string `json:"state"`
	ExpiresAt     int64  `json:"expiresAt"`
}

type mcpOAuthConsentCursor struct {
	RequestID          string `json:"requestId"`
	PrincipalID        string `json:"principalId"`
	MembershipSnapshot string `json:"membershipSnapshot"`
	WorkspaceCreated   string `json:"workspaceCreated"`
	WorkspaceID        string `json:"workspaceId"`
}

type storedMCPOAuthAuthorization struct {
	ID            string `db:"id"`
	ClientID      string `db:"client_id"`
	OwnerID       string `db:"owner_id"`
	RedirectURI   string `db:"redirect_uri"`
	Resource      string `db:"resource"`
	Scope         string `db:"scope"`
	CodeHash      string `db:"code_hash"`
	CodeChallenge string `db:"code_challenge"`
	WorkspaceHash string `db:"workspace_hash"`
	Created       int64  `db:"created"`
	ExpiresAt     int64  `db:"expires_at"`
	// The connection whose key approved it, when one did.
	CreatedBy string `db:"created_by_connection"`
}

func beginMCPOAuthAuthorization(event *core.RequestEvent) error {
	query := event.Request.URL.Query()
	clientID := strings.TrimSpace(query.Get("client_id"))
	if mcpOAuthClientIDIsMetadataAddress(clientID) {
		// A client named by its metadata document's address (Claude's
		// published identity): read and kept here first.
		if _, err := resolveMCPOAuthMetadataClient(event.Request.Context(), event.App, clientID); errors.Is(err, errMCPOAuthClientCapacity) {
			return oauthProtocolErrorWithProductCode(event, http.StatusServiceUnavailable, "temporarily_unavailable",
				errorCodeMCPOAuthClientCapacity, "This Leafen server has no room for another agent app right now. Try again later, or ask the server's administrator.")
		} else if err != nil {
			return oauthProtocolError(event, http.StatusBadRequest, "invalid_client", err.Error())
		}
	}
	client, err := findMCPOAuthClient(event.App, clientID)
	if err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "unknown client")
	}
	// The MCP endpoint is granted the "mcp" scope; the native API a key's scope.
	resource := query.Get("resource")
	scope, scopeGranted := grantedOAuthScope(resource, query.Get("scope"))
	redirectURI := strings.TrimSpace(query.Get("redirect_uri"))
	state := strings.TrimSpace(query.Get("state"))
	challenge := strings.TrimSpace(query.Get("code_challenge"))
	if query.Get("response_type") != "code" || !clientAllowsRedirect(client, redirectURI) ||
		!mcpOAuthResourceKnown(event.Request, resource) || !scopeGranted ||
		query.Get("code_challenge_method") != "S256" || !mcpPKCEChallengePattern.MatchString(challenge) ||
		state == "" || len(state) > 1024 {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "invalid authorization request")
	}
	if err := touchMCPOAuthClient(event.App, client.ID, time.Now().UnixMilli()); err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "unknown client")
	}
	request := mcpOAuthAuthorizationRequest{
		ID: security.RandomString(24), ClientID: client.ID, RedirectURI: redirectURI,
		Resource: resource, Scope: scope, CodeChallenge: challenge, State: state,
		ExpiresAt: time.Now().Add(mcpOAuthRequestLifetime).UnixMilli(),
	}
	sealed, err := sealMCPOAuthRequest(event.App, request)
	if err != nil {
		return oauthServerError(event, "authorization failed", err)
	}
	// A packaged app's own server has no sign-in outside: the request waits
	// for its owner's answer in the app's window (local_owner_tunnel.go). So
	// does one from an agent on this computer, which a browser here makes
	// without the window's sign-in (user decision 2026-11-09: an agent going
	// through the first run connects itself, its person allowing it in Leafen).
	tunneled := requestTunnelOrigin(event.Request) != ""
	if owner := localOwnerOf(event.App); owner != nil && (tunneled || mcpLoopbackHost(event.Request.Host)) {
		ticket, err := owner.await(request, sealed, client.ClientName, !tunneled)
		if err != nil {
			return oauthServerError(event, "authorization failed", err)
		}
		return event.Redirect(http.StatusSeeOther, localOwnerWaitingPath+"?"+url.Values{"ticket": []string{ticket}}.Encode())
	}
	destination := &url.URL{Path: "/"}
	destination.RawQuery = url.Values{"mcpAuthorization": []string{sealed}}.Encode()
	return event.Redirect(http.StatusSeeOther, destination.String())
}

func readMCPOAuthConsent(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	request, client, err := validatedMCPOAuthRequest(event)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPOAuthRequestInvalid, "The MCP connection request has expired or is invalid.")
	}
	cursor, err := decodeMCPOAuthConsentCursor(event.Request.URL.Query().Get("cursor"), request.ID, user.Id)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPOAuthRequestInvalid, "The MCP connection request location is invalid.")
	}
	ready, err := ensureBootstrapVisibilityProjectionReadiness(
		event, user.Id, errorCodeMCPOAuthFailed, "Couldn't load the MCP connection request.", "mcp-oauth.consent",
	)
	if !ready {
		return err
	}
	response := mcpOAuthConsentResponse{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		membershipSnapshot, err := coordinateMCPConsentMembershipSnapshot(
			txApp, user.Id, cursor.MembershipSnapshot,
		)
		if err != nil {
			return err
		}
		cursor.MembershipSnapshot = membershipSnapshot
		rows, done, err := loadBootstrapWorkspaces(txApp, user.Id, bootstrapCursor{
			WorkspaceCreated: cursor.WorkspaceCreated, WorkspaceID: cursor.WorkspaceID,
		}, maxMCPOAuthConsentPage)
		if err != nil {
			return err
		}
		governanceWorkspaces := make([]mcpGovernanceWorkspace, 0, len(rows))
		for _, row := range rows {
			governanceWorkspaces = append(governanceWorkspaces, mcpGovernanceWorkspace{
				ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name,
			})
		}
		approvedWorkspaces, err := filterMCPClientApprovedWorkspaces(
			txApp, governanceWorkspaces, client.ID, user.Id,
		)
		if err != nil {
			return err
		}
		approvedIDs := make(map[string]struct{}, len(approvedWorkspaces))
		for _, workspace := range approvedWorkspaces {
			approvedIDs[workspace.ID] = struct{}{}
		}
		// An app that acts as itself is installed by a workspace's owner.
		registration, registered, err := findAppRegistration(txApp, client.ID)
		if err != nil {
			return err
		}
		integration := registered && registration.ActsAs == appActsAsIntegration
		response = mcpOAuthConsentResponse{
			Integration: integration,
			ID:          request.ID, Client: mcpOAuthConsentClient{ID: client.ID, Name: client.ClientName},
			Resource: request.Resource, Scopes: []string{request.Scope}, Workspaces: []workspaceResponse{},
			ExpiresAt: request.ExpiresAt, MembershipSnapshot: membershipSnapshot,
		}
		// The person's own connections, so the screen says when they near
		// the limit; an app acting as itself counts under its own account.
		if !integration {
			if response.ActiveConnections, err = countActiveMCPConnections(txApp, user.Id, time.Now().UnixMilli()); err != nil {
				return err
			}
			response.ConnectionLimit = maxActiveMCPConnectionsPerOwner
		}
		// An app's workspace and one an import is staging in say so, for
		// the screen to set them apart from the person's own (2026-10-16:
		// all looked the same, none chosen).
		staging, err := mcpConsentStagingWorkspaces(txApp, rows)
		if err != nil {
			return err
		}
		for _, row := range rows {
			cursor.WorkspaceCreated = row.Created
			cursor.WorkspaceID = row.ID
			if _, approved := approvedIDs[row.ID]; !approved || (integration && row.OwnerID != user.Id) {
				continue
			}
			role := ""
			if row.OwnerID != user.Id {
				role = row.Role
			}
			_, staged := staging[row.ID]
			response.Workspaces = append(response.Workspaces, workspaceResponse{
				ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name, Role: role,
				AppInstallID: row.AppInstallID, AppSlug: row.AppSlug, Staging: staged,
			})
		}
		if !done {
			next, err := encodeMCPOAuthConsentCursor(cursor)
			if err != nil {
				return err
			}
			response.NextCursor = &next
			response.HasMore = true
		}
		return nil
	})
	if errors.Is(err, errMCPOAuthConsentSnapshotChanged) {
		return apiError(event, http.StatusConflict, errorCodeMCPOAuthRequestConflict, "The workspace list changed. Load it again from the start.")
	}
	if errors.Is(err, errMCPGovernanceInvalid) || errors.Is(err, errMCPGovernanceUnavailable) {
		return mcpGovernanceManagementError(event, err, "oauth-consent-read")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPOAuthFailed, "Couldn't load the MCP connection request.", err)
	}
	return event.JSON(http.StatusOK, response)
}

func approveMCPOAuthConsent(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	request, client, err := validatedMCPOAuthRequest(event)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPOAuthRequestInvalid, "The MCP connection request has expired or is invalid.")
	}
	var input mcpOAuthApprovalInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPOAuthRequestInvalid, "Check the MCP workspace selection.")
	}
	workspaceIDs := uniqueNonemptyStrings(input.WorkspaceIDs)
	if len(workspaceIDs) == 0 || len(workspaceIDs) != len(input.WorkspaceIDs) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPOAuthRequestInvalid, "Choose at least one workspace.")
	}
	code, err := deterministicMCPOAuthCode(event.App, request.ID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPOAuthFailed, "Couldn't approve the MCP connection.", err)
	}
	workspaceHash := mcpOAuthWorkspaceHash(workspaceIDs)
	denied := false
	governanceDenied := false
	conflict := false
	connectionsFull := false
	waitingFull := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if err := authorizeWorkspaceSet(txApp, user.Id, workspaceIDs, permissionWorkspaceRead); isPermissionDenied(err) {
			denied = true
			return nil
		} else if err != nil {
			return err
		}
		// An app that acts as itself is installed in one workspace by its
		// owner, as a member or a guest; its token is that account's.
		ownerID := user.Id
		registration, registered, err := findAppRegistration(txApp, client.ID)
		if err != nil {
			return err
		}
		if registered && registration.ActsAs == appActsAsIntegration {
			role := input.IntegrationRole
			if role == "" {
				role = integrationRoleGuest
			}
			if len(workspaceIDs) != 1 || (role != workspaceRoleMember && role != integrationRoleGuest) {
				denied = true
				return nil
			}
			if _, err := authorizeWorkspace(txApp, user.Id, workspaceIDs[0], permissionWorkspaceOwner); err != nil {
				if isPermissionDenied(err) {
					denied = true
					return nil
				}
				return err
			}
			if ownerID, err = ensureIntegrationAccount(txApp, registration, workspaceIDs[0], role, user.Id); err != nil {
				return err
			}
		}
		workspaces, err := loadMCPGovernanceWorkspaces(txApp, workspaceIDs)
		if err != nil {
			return err
		}
		err = assertMCPClientApprovedForWorkspaces(txApp, workspaces, mcpGovernanceContext{
			ActorID: user.Id, ClientID: client.ID, ClientName: client.ClientName,
			ConnectionID: request.ID, Stage: "oauth-consent-approval",
		})
		if errors.Is(err, errMCPClientNotApproved) {
			governanceDenied = true
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_mcp_oauth_authorization_workspaces}}
			WHERE [[authorization_id]] IN (
				SELECT [[id]] FROM {{leafen_mcp_oauth_authorizations}}
				WHERE [[owner_id]] = {:ownerId} AND [[expires_at]] <= {:now}
			);
			DELETE FROM {{leafen_mcp_oauth_authorizations}}
			WHERE [[owner_id]] = {:ownerId} AND [[expires_at]] <= {:now}
		`).Bind(dbx.Params{"ownerId": ownerID, "now": time.Now().UnixMilli()}).Execute(); err != nil {
			return err
		}
		existing, err := findMCPOAuthAuthorizationByID(txApp, request.ID)
		if err == nil {
			if existing.OwnerID != ownerID || existing.ClientID != request.ClientID || existing.RedirectURI != request.RedirectURI ||
				existing.Resource != request.Resource || existing.CodeChallenge != request.CodeChallenge || existing.WorkspaceHash != workspaceHash {
				conflict = true
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UnixMilli()
		var count int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_mcp_oauth_authorizations}}
			WHERE [[owner_id]] = {:ownerId} AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{"ownerId": ownerID, "now": now}).Row(&count); err != nil {
			return err
		}
		if count >= maxActiveMCPOAuthRequests {
			waitingFull = true
			return nil
		}
		// A connection the agent would fail to take is refused here, where
		// the person sees why: approved ones still waiting take a place too.
		active, err := countActiveMCPConnections(txApp, ownerID, now)
		if err != nil {
			return err
		}
		if active+count >= maxActiveMCPConnectionsPerOwner {
			connectionsFull = true
			return nil
		}
		created := now
		if _, err := txApp.DB().Insert(mcpOAuthAuthorizationsTable, dbx.Params{
			"id": request.ID, "client_id": request.ClientID, "owner_id": ownerID,
			"redirect_uri": request.RedirectURI, "resource": request.Resource, "scope": request.Scope,
			"code_hash": mcpTokenHash(code), "code_challenge": request.CodeChallenge,
			"workspace_hash": workspaceHash, "created": created, "expires_at": request.ExpiresAt,
			"created_by_connection": approvingConnection(event),
		}).Execute(); err != nil {
			return err
		}
		for _, workspaceID := range workspaceIDs {
			if _, err := txApp.DB().Insert(mcpOAuthWorkspacesTable, dbx.Params{
				"authorization_id": request.ID, "workspace_id": workspaceID,
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	if denied {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if governanceDenied {
		return mcpGovernanceManagementError(event, errMCPClientNotApproved, "oauth-consent-approval")
	}
	if conflict {
		return apiError(event, http.StatusConflict, errorCodeMCPOAuthRequestConflict, "This MCP connection request was already processed with a different scope.")
	}
	if waitingFull {
		return apiError(event, http.StatusConflict, errorCodeMCPOAuthRequestCapacity, "Too many approved connections are still waiting for their agents. Try again in a few minutes.")
	}
	if connectionsFull {
		return apiError(event, http.StatusConflict, errorCodeMCPConnectionCapacity, "This account reached its MCP connection limit. Disconnect one you no longer use, then try again.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPOAuthFailed, "Couldn't approve the MCP connection.", err)
	}
	redirect, err := mcpOAuthAuthorizationRedirect(request, map[string]string{"code": code})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPOAuthFailed, "Couldn't approve the MCP connection.", err)
	}
	// A web agent waiting at the tunnel's address goes on from its own page;
	// the window stays.
	if owner := localOwnerOf(event.App); owner != nil && owner.answer(request.ID, localOwnerAgentAllowed, redirect) {
		return event.JSON(http.StatusOK, mcpOAuthRedirectResponse{Remote: true})
	}
	return event.JSON(http.StatusOK, mcpOAuthRedirectResponse{RedirectURL: redirect})
}

func denyMCPOAuthConsent(event *core.RequestEvent) error {
	if _, ok := authenticatedUser(event); !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	request, _, err := validatedMCPOAuthRequest(event)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPOAuthRequestInvalid, "The MCP connection request has expired or is invalid.")
	}
	redirect, err := mcpOAuthAuthorizationRedirect(request, map[string]string{"error": "access_denied"})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPOAuthFailed, "Couldn't process the MCP connection denial.", err)
	}
	if owner := localOwnerOf(event.App); owner != nil && owner.answer(request.ID, localOwnerAgentDenied, redirect) {
		return event.JSON(http.StatusOK, mcpOAuthRedirectResponse{Remote: true})
	}
	return event.JSON(http.StatusOK, mcpOAuthRedirectResponse{RedirectURL: redirect})
}

func validatedMCPOAuthRequest(event *core.RequestEvent) (mcpOAuthAuthorizationRequest, storedMCPOAuthClient, error) {
	request, err := openMCPOAuthRequest(event.App, event.Request.PathValue("request"))
	if err != nil || request.ID == "" || request.ClientID == "" || request.ExpiresAt <= time.Now().UnixMilli() ||
		!oauthScopeFitsResource(request.Resource, request.Scope) || !mcpPKCEChallengePattern.MatchString(request.CodeChallenge) {
		return mcpOAuthAuthorizationRequest{}, storedMCPOAuthClient{}, errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	if !mcpOAuthResourceKnown(event.Request, request.Resource) {
		return mcpOAuthAuthorizationRequest{}, storedMCPOAuthClient{}, errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	client, err := findMCPOAuthClient(event.App, request.ClientID)
	if err != nil || !clientAllowsRedirect(client, request.RedirectURI) {
		return mcpOAuthAuthorizationRequest{}, storedMCPOAuthClient{}, errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	return request, client, nil
}

func findMCPOAuthAuthorizationByID(app core.App, id string) (storedMCPOAuthAuthorization, error) {
	var authorization storedMCPOAuthAuthorization
	err := app.DB().NewQuery(`
		SELECT [[id]], [[client_id]], [[owner_id]], [[redirect_uri]], [[resource]], [[scope]],
			[[code_hash]], [[code_challenge]], [[workspace_hash]], [[created]], [[expires_at]], [[created_by_connection]]
		FROM {{leafen_mcp_oauth_authorizations}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": id}).One(&authorization)
	return authorization, err
}

func mcpOAuthWorkspaceHash(workspaceIDs []string) string {
	digest := sha256.Sum256([]byte(strings.Join(workspaceIDs, "\x00")))
	return hex.EncodeToString(digest[:])
}

func mcpOAuthAuthorizationRedirect(request mcpOAuthAuthorizationRequest, values map[string]string) (string, error) {
	redirect, err := url.Parse(request.RedirectURI)
	if err != nil {
		return "", err
	}
	query := redirect.Query()
	for key, value := range values {
		query.Set(key, value)
	}
	query.Set("state", request.State)
	issuer, err := url.Parse(request.Resource)
	if err != nil {
		return "", err
	}
	issuer.Path = ""
	issuer.RawPath = ""
	issuer.RawQuery = ""
	issuer.Fragment = ""
	query.Set("iss", issuer.String())
	redirect.RawQuery = query.Encode()
	return redirect.String(), nil
}

func decodeMCPOAuthConsentCursor(raw string, requestID string, principalID string) (mcpOAuthConsentCursor, error) {
	cursor := mcpOAuthConsentCursor{RequestID: requestID, PrincipalID: principalID}
	if strings.TrimSpace(raw) == "" {
		return cursor, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 2048 || json.Unmarshal(data, &cursor) != nil ||
		cursor.RequestID != requestID || cursor.PrincipalID != principalID ||
		cursor.MembershipSnapshot == "" || cursor.WorkspaceCreated == "" || cursor.WorkspaceID == "" {
		return mcpOAuthConsentCursor{}, errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	return cursor, nil
}

func encodeMCPOAuthConsentCursor(cursor mcpOAuthConsentCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// approvingConnection is the connection whose key approved a sign-in, so the
// agent's connection ends with it; none for a person on the screen.
func approvingConnection(event *core.RequestEvent) string {
	if key, keyed := userKeyOf(event); keyed {
		return key.ConnectionID
	}
	return ""
}
