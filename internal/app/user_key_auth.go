package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	errorCodeUserKeyInvalid = "LEAFEN_USER_KEY_INVALID"
	errorCodeUserKeyClosed  = "LEAFEN_USER_KEY_CLOSED"
	errorCodeUserKeyScope   = "LEAFEN_USER_KEY_SCOPE"
	errorCodeUserKeyPolicy  = "LEAFEN_USER_KEY_POLICY"
	errorCodeUserKeyFailed  = "LEAFEN_USER_KEY_FAILED"
	userKeyAuthorityKey     = "leafenUserKey"
	userKeyPrefix           = "lmc_"
	// mcpOAuthAccessPrefix starts an OAuth access token, which an app holds.
	mcpOAuthAccessPrefix    = "lmo_"
	maxUserKeyInspectedBody = 64 << 20
)

type userKeyContextKey struct{}

// The key a call carries: one of its owner's own connections.
type userKeyAuthority struct {
	ConnectionID string
	Name         string
	OwnerID      string
	Scope        userKeyScope
	WorkspaceIDs []string
	// AllWorkspaces marks a key made for all of its person's workspaces,
	// reaching those they come to be in later too (user_key_reach_gained.go);
	// Agent, an agent connection's rather than an MCP token's (connection_person_rights.go).
	AllWorkspaces, Agent bool
	Created              int64
	ExpiresAt            int64
	// LastUsedAt is when the key's use was last recorded (mcp_connection_last_used.go).
	LastUsedAt int64
	// AppInstallID names the installed app whose screen holds the key, for a
	// person's app token (app_platform_delegation.go), and TenancyKey the
	// organization it was opened for.
	AppInstallID string
	TenancyKey   string
	// AppAccount marks the app's own account acting in its app workspace,
	// rather than a person through the app.
	AppAccount bool
	// An opened-file app's token reaches only the file block it opened on,
	// and copies kept beside it (app_platform_opened_file.go); its row is
	// named by DelegationHash.
	OpenedPageID   string
	OpenedBlockIDs []string
	DelegationHash string
	// What its page narrowed it to: an MCP token's tools and pages, nothing
	// for an agent connection (connection_person_rights.go).
	connectionNarrowing
	// MCP is the agent's call through Leafen's MCP, for the person's approval
	// it may wait for (mcp_action_approvals.go); nil for a request sent to
	// the API itself.
	MCP *userKeyMCPCall
}

// userKeyOf is the key a call was made with, if it was made with one.
func userKeyOf(event *core.RequestEvent) (userKeyAuthority, bool) {
	authority, ok := event.Get(userKeyAuthorityKey).(userKeyAuthority)
	return authority, ok
}

// appTokenCall says whether a request comes through an installed app's token
// rather than its person, a person's key or a bot.
func appTokenCall(event *core.RequestEvent) bool {
	authority, keyed := userKeyOf(event)
	return keyed && authority.AppInstallID != ""
}

// registerUserKeyAuthentication lets a connection's key act on the native
// API as its owner, after the session loader has found no session in the
// call.
// appBudgetGate admits an app's call within its budget and returns what
// ends its turn, or admitted false with the refusal it answered; a build
// without the app platform admits everything.
var appBudgetGate = func(*core.RequestEvent, userKeyAuthority) (func(), bool, error) { return func() {}, true, nil }

// appReadRecorder counts a person's data an app read; a build without the
// app platform has no apps to count.
var appReadRecorder = func(userKeyAuthority, userKeyPlace, string, time.Time) {}

func registerUserKeyAuthentication(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenUserKey", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 1, Func: authenticateUserKey,
	})
}

func authenticateUserKey(event *core.RequestEvent) error {
	path := event.Request.URL.Path
	token := bearerToken(event.Request)
	// An MCP operation call runs inside the server with its connection's
	// authority already checked; nothing outside the process can set it.
	internal, isInternal := event.Request.Context().Value(userKeyInternalContextKey{}).(userKeyAuthority)
	app := strings.HasPrefix(token, mcpOAuthAccessPrefix)
	delegated := strings.HasPrefix(token, appDelegationPrefix)
	if (path != apiRoot && !strings.HasPrefix(path, apiRoot+"/")) ||
		(!isInternal && !strings.HasPrefix(token, userKeyPrefix) && !app && !delegated) {
		return event.Next()
	}
	// A key is the only credential the call carries.
	event.Auth = nil
	authority, ok, err := internal, true, error(nil)
	if !isInternal && app {
		// An app's token, granted for the native API, is its person's key.
		resource, resourceErr := nativeOAuthResource(event.Request)
		if resourceErr != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", resourceErr)
		}
		authority, ok, err = findUserKeyBy(event.App, "token_hash", mcpTokenHash(token), resource)
	} else if !isInternal && delegated {
		authority, ok, err = appDelegationAuthority(event.App, token)
	} else if !isInternal {
		authority, ok, err = findUserKey(event.App, token)
	}
	if errors.Is(err, errAppStopped) {
		return apiError(event, http.StatusForbidden, errorCodeAppStopped, appStoppedMessage)
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", err)
	}
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUserKeyInvalid,
			"This key isn’t valid: it may be revoked or expired, or its workspaces are no longer yours.")
	}
	// The API is what leafen_call runs: a token whose page turned that off,
	// or narrowed it to chosen pages the API can't be held to, doesn't reach it.
	if !isInternal && (slices.Contains(authority.DisabledTools, "leafen_call") || authority.PagesNarrowed) {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed,
			"This token's page doesn't let it use the API: turn on leafen_call and \"모든 페이지\" there.")
	}
	noteKeyUse(event.App, event.Request, authority, isInternal)
	rule, found := userKeyRuleFor(event.Request.Method, strings.TrimPrefix(path, apiRoot))
	if !found && !userKeyRouteExists(event.Request.Method, strings.TrimPrefix(path, apiRoot)) {
		return apiError(event, http.StatusNotFound, errorCodeAPIRouteNotFound,
			"This server has no such API: leafen_operations (GET "+apiRoot+"/operations) lists what a key can run.")
	}
	if !found {
		rule = userKeyUnclassifiedRule
	}
	if rule.scope == userKeyScopeClosed {
		reason := rule.reason
		if reason == "" {
			reason = "A key can’t do this; it’s done logged in."
		}
		// Where the person does it, and the one step there (user_key_person_only_screens.go).
		reason = userKeyClosedWhere(event.App, authority.OwnerID, rule.path, reason)
		return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, reason)
	}
	if rule.own && authority.AppInstallID != "" {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "An app can’t do this; it’s its person’s own.")
	}
	if rule.across {
		reachesAll, err := userKeyReachesAllWorkspaces(event.App, authority)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", err)
		}
		if !reachesAll {
			return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, userKeyReachesSomeRefusal(event.App, authority))
		}
	}
	if authority.AppInstallID != "" && appTokenClosedPath(strings.TrimPrefix(path, apiRoot)) {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed,
			"An app reaches outside Leafen only through the addresses approved for it.")
	}
	if rule.place == userKeyPlacePublic {
		return event.Next()
	}
	var body []byte
	if rule.byOperations || rule.confirm || rule.place == userKeyPlaceBodyWorkspace || rule.place == userKeyPlaceBodyPage ||
		rule.place == userKeyPlaceForm || rule.actionScopes != nil || len(rule.bodyTargets) > 0 || authority.OpenedPageID != "" {
		if body, err = readUserKeyBody(event.Request); err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeUserKeyFailed, "Couldn’t read the request.")
		}
	}
	if authority.OpenedPageID != "" {
		if err := appOpenedFileAllows(event.App, event.Request, authority, strings.TrimPrefix(path, apiRoot), body); err != nil {
			return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "This app reaches only the file it was opened on.")
		}
	}
	required := rule.scopeFor(body)
	if rule.byOperations && userKeyBodyChangesStructure(body) {
		required = max(required, userKeyScopeStructure)
	}
	if authority.Scope < required {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyScope, userKeyScopeRefusal(event.App, authority, required))
	}
	// The fields a key's checks read, as its handler will; a body they need and can't read is refused.
	fields := readUserKeyBodyFields(event.Request.Header.Get("Content-Type"), body)
	// A form's file part is the body itself, its form named in a header.
	needsBody := len(rule.bodyTargets) > 0 || rule.place == userKeyPlaceBodyWorkspace || rule.place == userKeyPlaceBodyPage ||
		rule.place == userKeyPlaceForm && event.Request.Header.Get(formFileTokenHeader) == ""
	if needsBody && !fields.readable {
		return apiError(event, http.StatusBadRequest, errorCodeUserKeyFailed, "Send the request body as JSON or a form.")
	}
	workspaces, err := userKeyWorkspacesOf(event, rule, fields, authority)
	if errors.Is(err, errUserKeyFormNamesNothing) {
		return apiError(event, http.StatusBadRequest, errorCodeFormInvalid,
			"Check the form details: a form call names the page it configures (pageId) or the form it reads or answers (token).")
	}
	if errors.Is(err, errUserKeyOutsideWorkspaces) {
		return refuseUserKeyPlace(event, rule, fields)
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", err)
	}
	reached, within, err := userKeyTargetsWithin(event.App, rule, path, fields, authority)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", err)
	} else if !within {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound,
			"What this call names besides its own place — a destination, a source — isn’t in the workspaces this key reaches, "+
				"or doesn’t exist.")
	}
	if effective, err := userKeyEffectiveScope(event.App, authority, workspaces); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", err)
	} else if effective < required {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyPolicy,
			"This workspace’s organization allows keys up to the "+userKeyScopeNames[effective]+" scope; this needs "+userKeyScopeNames[required]+".")
	}
	if denial, err := userKeyPolicyDenial(event.App, authority, required, workspaces); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn’t check the key.", err)
	} else if denial != "" {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyPolicy, denial)
	}
	// An app asks a person before such a change (docs/app-platform.md 5); a
	// repeated request is no person's approval.
	asks := rule.asksConfirmation(event.App, authority, body)
	if asks && authority.AppInstallID != "" {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "An app can’t do this without the person’s approval.")
	}
	var pending *userKeyPendingAsk
	if asks && rule.asksWhereItActs {
		pending = &userKeyPendingAsk{authority: authority, rule: rule, body: body}
	} else if asks {
		if handled, err := requireUserKeyConfirmation(event, authority, rule, body); handled || err != nil {
			return err
		}
	}
	// An app's calls draw on its budget (docs/app-platform.md 15).
	if authority.AppInstallID != "" {
		release, admitted, refusal := appBudgetGate(event, authority)
		if !admitted {
			return refusal
		}
		defer release()
	}
	owner, err := event.App.FindRecordById("users", authority.OwnerID)
	if err != nil {
		return apiError(event, http.StatusUnauthorized, errorCodeUserKeyInvalid, "This key isn’t valid.")
	}
	event.Auth = owner
	event.Set(userKeyAuthorityKey, authority)
	// What the call changes names the key's connection beside its person.
	ctx := withPageHistoryCallMoment(context.WithValue(event.Request.Context(), userKeyContextKey{}, authority))
	if pending != nil {
		ctx = context.WithValue(ctx, userKeyPendingAskKey{}, pending)
	}
	event.Request = event.Request.WithContext(ctx)
	event.App = userKeyApp{App: event.App, ctx: event.Request.Context()}
	if err := nextKeepingUserKeySecrets(event, authority); err != nil {
		return err
	}
	noteUserKeyActedUnasked(event, pending)
	noteUserKeyWorkedIn(event, authority, workspaces, reached)
	// A change, not a read sent as a POST (a query, a search): its route needs more than reading (the audit log's "연결로 변경", 2026-11-06).
	if event.Request.Method != http.MethodGet && required > userKeyScopeRead && event.Status() < http.StatusBadRequest {
		recordUserKeyChange(event, authority, workspaces)
	}
	// What an app read of the person's data (docs/app-platform.md 15).
	if event.Request.Method == http.MethodGet && event.Status() < http.StatusBadRequest && authority.AppInstallID != "" && !authority.AppAccount {
		appReadRecorder(authority, rule.place, userKeyPathValue(path), time.Now())
	}
	return nil
}

// findUserKey reads a connection's own key: made by hand (not an OAuth
// token, which belongs to the MCP resource it was granted for), not revoked
// or expired, its owner able to act and its workspaces still theirs and
// allowed for hand-made connections by their organizations.
func findUserKey(app core.App, token string) (userKeyAuthority, bool, error) {
	return findUserKeyBy(app, "token_hash", mcpTokenHash(token), "")
}

// findUserKeyByID reads a connection by its id — made by hand, by a code,
// granted to an app or to an agent over OAuth — as something it made, such
// as a view link or a webhook, finds it again.
func findUserKeyByID(app core.App, connectionID string) (userKeyAuthority, bool, error) {
	return findUserKeyBy(app, "id", connectionID, "*")
}

// findUserKeyBy reads a key: one made by hand when resource is "", an
// app's granted for that native API resource, or any connection for "*".
func findUserKeyBy(app core.App, column, value, resource string) (userKeyAuthority, bool, error) {
	stored := struct {
		ID        string `db:"id"`
		OwnerID   string `db:"owner_id"`
		Name      string `db:"client_name"`
		Scope     string `db:"access_scope"`
		Created   int64  `db:"created"`
		ExpiresAt int64  `db:"token_expires_at"`
		ClientID  string `db:"oauth_client_id"`
		Resource  string `db:"oauth_resource"`
		LastUsed  int64  `db:"last_used_at"`
		CreatedBy string `db:"created_by_connection"`
		Renewable int64  `db:"refresh_expires_at"`
		Disabled  string `db:"disabled_tools"`
		Pages     string `db:"page_ids"`
		AllPages  int64  `db:"all_pages"`
		All       int64  `db:"all_workspaces"`
		Agent     bool   `db:"agent_connection"`
	}{AllPages: 1}
	// A key refreshed to be revoked later still works until then.
	previous := ""
	if column == "token_hash" {
		previous = ` OR ` + partialIndexKey(`[[previous_token_hash]]`, ` = {:value}`) + ` AND [[previous_token_until]] > {:now}`
	}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[access_scope]], [[created]], [[token_expires_at]],
			[[oauth_client_id]], [[oauth_resource]], [[last_used_at]], [[created_by_connection]], [[refresh_expires_at]],
			[[disabled_tools]], [[page_ids]], [[all_pages]], [[all_workspaces]], [[agent_connection]]
		FROM {{leafen_mcp_connections}}
		WHERE ([[` + column + `]] = {:value}` + previous + `) AND [[revoked_at]] = 0
	`).Bind(dbx.Params{"value": value, "now": time.Now().UnixMilli()}).One(&stored)
	if err == nil {
		manual := stored.ClientID == "" && stored.Resource == ""
		granted := stored.ClientID != "" && oauthResourceIsNative(stored.Resource)
		switch {
		case resource == "" && !manual,
			resource != "" && resource != "*" && (!granted || !sameServerResource(stored.Resource, resource)):
			err = sql.ErrNoRows
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return userKeyAuthority{}, false, nil
	}
	if err != nil {
		return userKeyAuthority{}, false, err
	}
	// What an OAuth connection made lasts while the connection can renew its
	// access, not only while one access token does.
	expiresAt := stored.ExpiresAt
	if resource == "*" && stored.ClientID != "" && stored.Renewable != 0 {
		expiresAt = stored.Renewable
	}
	scope, known := parseUserKeyScope(stored.Scope)
	if !known || (expiresAt != 0 && expiresAt <= time.Now().UnixMilli()) {
		return userKeyAuthority{}, false, nil
	}
	if active, err := accountMayAct(app, stored.OwnerID); err != nil || !active {
		return userKeyAuthority{}, false, err
	}
	// A key works as its person.
	acting := stored.OwnerID
	// A key another key made holds only while that one does.
	if holds, err := connectionLineageHolds(app, stored.CreatedBy); err != nil || !holds {
		return userKeyAuthority{}, false, err
	}
	if stored.All == 1 {
		keepUserKeyReachingAll(app, stored.ID, stored.OwnerID)
	}
	rows := []storedMCPWorkspace{}
	if err := app.DB().NewQuery(`
		SELECT [[scope]].[[workspace_id]], [[workspace]].[[organization]] AS [[organization_id]], [[workspace]].[[name]]
		FROM {{leafen_mcp_connection_workspaces}} [[scope]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
		WHERE [[scope]].[[connection_id]] = {:connectionId}
		ORDER BY [[scope]].[[workspace_id]]
	`).Bind(dbx.Params{"connectionId": stored.ID}).All(&rows); err != nil {
		return userKeyAuthority{}, false, err
	}
	scoped := make([]mcpGovernanceWorkspace, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		scoped = append(scoped, mcpGovernanceWorkspace{ID: row.WorkspaceID, OrganizationID: row.OrganizationID, Name: row.Name})
		ids = append(ids, row.WorkspaceID)
	}
	if len(ids) == 0 {
		return userKeyAuthority{}, false, nil
	}
	if err := authorizeConnectionWorkspaces(app, stored.OwnerID, ids); err != nil {
		if isPermissionDenied(err) {
			return userKeyAuthority{}, false, nil
		}
		return userKeyAuthority{}, false, err
	}
	governed := mcpGovernanceManualClientID
	if stored.ClientID != "" {
		governed = stored.ClientID
	}
	approved, err := filterMCPClientApprovedWorkspaces(app, scoped, governed, stored.OwnerID)
	if err != nil {
		return userKeyAuthority{}, false, err
	}
	approvedIDs := make([]string, 0, len(approved))
	for _, workspace := range approved {
		if acting != stored.OwnerID {
			if _, err := authorizeWorkspace(app, acting, workspace.ID, permissionWorkspaceRead); isPermissionDenied(err) {
				continue
			} else if err != nil {
				return userKeyAuthority{}, false, err
			}
		}
		approvedIDs = append(approvedIDs, workspace.ID)
	}
	return userKeyAuthority{
		ConnectionID: stored.ID, Name: stored.Name, OwnerID: acting, Scope: scope,
		WorkspaceIDs: approvedIDs, AllWorkspaces: stored.All == 1, Agent: stored.Agent, Created: stored.Created, ExpiresAt: stored.ExpiresAt,
		LastUsedAt: stored.LastUsed, connectionNarrowing: connectionNarrowingOf(stored.Agent, stored.Disabled, stored.Pages, stored.AllPages),
	}, true, nil
}

// readUserKeyBody reads the body a rule needs to see and leaves it for the
// handler to read again.
// userKeyReadBody is a request body put back after the key's checks read
// its start.
type userKeyReadBody struct {
	io.Reader
	io.Closer
}

func readUserKeyBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxUserKeyInspectedBody))
	if err != nil {
		return nil, err
	}
	// Read to its end, the body goes back as read: the server's request body
	// reads itself again once read to its end, so following it with what
	// was read sent it twice (2026-10-16: an app's file part arrived doubled
	// and every document app save was refused).
	if int64(len(body)) < maxUserKeyInspectedBody {
		request.Body = userKeyReadBody{Reader: bytes.NewReader(body), Closer: request.Body}
		return body, nil
	}
	// The handler reads the whole body, the inspected start and what follows
	// (2026-10-13: a body past the inspected 64 MiB reached it cut short).
	request.Body = userKeyReadBody{Reader: io.MultiReader(bytes.NewReader(body), request.Body), Closer: request.Body}
	return body, nil
}

func userKeyBodyChangesStructure(body []byte) bool {
	var input struct {
		Operations []struct {
			Kind string `json:"kind"`
		} `json:"operations"`
	}
	if json.Unmarshal(body, &input) != nil {
		return true
	}
	for _, operation := range input.Operations {
		if userKeyStructureOperations[operation.Kind] {
			return true
		}
	}
	return false
}

var errUserKeyOutsideWorkspaces = errors.New("outside the key's workspaces")

// userKeyWorkspacesOf finds the workspaces a call acts in and refuses one
// outside the key's, as the screen does a workspace that isn't the person's.
func userKeyWorkspacesOf(event *core.RequestEvent, rule userKeyRule, body userKeyBody, authority userKeyAuthority) ([]string, error) {
	value := userKeyPathValue(event.Request.URL.Path)
	lookup := func(query string) (string, error) {
		var workspaceID string
		err := event.App.DB().NewQuery(query).Bind(dbx.Params{"id": value}).Row(&workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", errUserKeyOutsideWorkspaces
		}
		return workspaceID, err
	}
	var workspaceID string
	var err error
	switch rule.place {
	case userKeyPlacePerson, userKeyPlacePublic:
		return nil, nil
	case userKeyPlaceWorkspaceParam:
		workspaceID = value
	case userKeyPlaceJobParam:
		workspaceID, err = lookup(`SELECT [[workspace_id]] FROM {{leafen_jobs}} WHERE [[id]] = {:id}`)
		// Work kept elsewhere than the job table is its owner's alone.
		if errors.Is(err, errUserKeyOutsideWorkspaces) || (err == nil && workspaceID == "") {
			return nil, nil
		}
	case userKeyPlaceOrganizationParam:
		return userKeyWorkspaceIn(event.App, value, authority)
	case userKeyPlaceForm:
		return userKeyFormWorkspaces(event, body, authority)
	case userKeyPlaceBodyWorkspace:
		// Two spellings with different values can't tell which the handler takes.
		var single bool
		if workspaceID, single = body.one(rule.param); !single {
			return nil, errUserKeyOutsideWorkspaces
		}
	case userKeyPlaceBodyPage:
		var single bool
		if value, single = body.one(rule.param); !single {
			return nil, errUserKeyOutsideWorkspaces
		}
		workspaceID, err = lookup(userKeyPlaceLookups[userKeyPlacePageParam])
	default:
		if query, found := userKeyPlaceLookups[rule.place]; found {
			workspaceID, err = lookup(query)
		}
	}
	if err != nil {
		return nil, err
	}
	if workspaceID == "" || !slices.Contains(authority.WorkspaceIDs, workspaceID) {
		return nil, errUserKeyOutsideWorkspaces
	}
	return []string{workspaceID}, nil
}

// userKeyWorkspaceIn is a workspace of the organization the key reaches,
// the call's place.
func userKeyWorkspaceIn(app core.App, organizationID string, authority userKeyAuthority) ([]string, error) {
	organizationWorkspaces := []string{}
	if organizationID != "" {
		if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[organization]] = {:id}`).
			Bind(dbx.Params{"id": organizationID}).Column(&organizationWorkspaces); err != nil {
			return nil, err
		}
	}
	for _, candidate := range organizationWorkspaces {
		if slices.Contains(authority.WorkspaceIDs, candidate) {
			return []string{candidate}, nil
		}
	}
	return nil, errUserKeyOutsideWorkspaces
}

// userKeyPathValue is the identifier every classified route carries second
// in its path: /pages/{id}/…, /workspaces/{id}/…, /database-rows/{id}/… and
// so on.
func userKeyPathValue(path string) string {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, apiRoot), "/"), "/")
	if len(segments) > 1 {
		return segments[1]
	}
	return ""
}
