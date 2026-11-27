package app

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A user key — the key of one of a person's own connections — reaches the
// native API as that person, within the scope it was made with and the
// workspaces it names. Each route the product registers under apiRoot is
// classified here and in user_key_*_routes.go: the scope it needs, where its
// workspace comes from and whether a call needs confirming first. What a
// person does in one workspace a key for that workspace does; what is the
// person's own across their workspaces takes a key for all of them; what no
// key does is closed with its reason. A route no rule names follows the
// person's own permission (user_key_unclassified.go), and the server log
// names it.

type userKeyScope int

const (
	userKeyScopeClosed userKeyScope = iota
	userKeyScopeRead
	userKeyScopeContent
	userKeyScopeStructure
	userKeyScopeAdmin
)

var userKeyScopeNames = map[userKeyScope]string{
	userKeyScopeRead: "read", userKeyScopeContent: "content",
	userKeyScopeStructure: "structure", userKeyScopeAdmin: "admin",
}

func parseUserKeyScope(name string) (userKeyScope, bool) {
	for scope, candidate := range userKeyScopeNames {
		if candidate == name {
			return scope, true
		}
	}
	return userKeyScopeClosed, false
}

// Where the workspace a call acts in is found.
type userKeyPlace int

const (
	// The call is about the key's own person, not a workspace.
	userKeyPlacePerson userKeyPlace = iota
	userKeyPlaceWorkspaceParam
	userKeyPlacePageParam
	userKeyPlaceRowParam
	userKeyPlaceOrganizationParam
	userKeyPlaceJobParam
	userKeyPlaceContainerParam
	userKeyPlaceDataSourceParam
	userKeyPlaceBodyWorkspace
	userKeyPlaceBodyPage
	userKeyPlaceFileParam
	// A form: the page a body names, configuring it, or the form its token
	// names, answering it — in the body or a part's header (user_key_forms.go).
	userKeyPlaceForm
	// The call needs no sign-in at all (public pages, health).
	userKeyPlacePublic
	// A guest invite link, in the workspace it lets guests into (page_guest_links.go).
	userKeyPlaceGuestLinkParam
)

// userKeyPlaceLookups find the workspace of what a place's path value names.
var userKeyPlaceLookups = map[userKeyPlace]string{
	userKeyPlacePageParam:       `SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`,
	userKeyPlaceRowParam:        `SELECT [[workspace_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`,
	userKeyPlaceContainerParam:  `SELECT [[workspace_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id}`,
	userKeyPlaceDataSourceParam: `SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`,
	userKeyPlaceFileParam:       `SELECT [[workspace]] FROM {{leafen_files}} WHERE [[id]] = {:id}`,
	userKeyPlaceGuestLinkParam:  `SELECT [[workspace_id]] FROM {{leafen_page_guest_links}} WHERE [[id]] = {:id}`,
}

type userKeyRule struct {
	methods string
	pattern *regexp.Regexp
	scope   userKeyScope
	place   userKeyPlace
	// The path value that names the workspace, page, row, organization,
	// job or container.
	param string
	// Irreversible: the first call returns a confirmation to send back, or
	// waits for the person's approval when the token asks them first
	// (mcp_action_approvals.go).
	confirm bool
	// The person is asked every time, whatever the token's switch and with
	// no hour.
	everyTime bool
	// The person is asked, as everyTime asks, only for a request this says
	// loosens what the key does without asking.
	asksWhen func(app core.App, key userKeyAuthority, body []byte) bool
	// The scope follows the operations in the body (content-graph
	// mutations carry both content and structure operations).
	byOperations bool
	// The route's own path, and the body fields and path values naming
	// what else the call reaches (user_key_targets.go).
	path        string
	bodyTargets []string
	pathTargets []string
	// The person's own: an app's token never does it.
	own            bool
	confirmActions []string
	// It crosses the workspaces a key names, so only a key that reaches all
	// its person's workspaces does it.
	across bool
	// Why no key does a closed route, said to the key that calls it.
	reason string
	// The scope each action a body names needs, where it isn't the rule's.
	actionScopes map[string]userKeyScope
	// It asks where its handler acts, after the handler's own checks
	// (user_key_acting_ask.go).
	asksWhereItActs bool
}

var (
	userKeyRouteRest  = regexp.MustCompile(`\\\{[A-Za-z]+\\\.\\\.\\\.\\\}`)
	userKeyRouteValue = regexp.MustCompile(`\\\{[A-Za-z]+\\\}`)
)

func userKeyRoute(methods, pattern string, scope userKeyScope, place userKeyPlace, param string) userKeyRule {
	// {name} is one segment; {name...}, the rest of the path.
	expression := userKeyRouteRest.ReplaceAllString(regexp.QuoteMeta(pattern), `.*`)
	expression = "^" + userKeyRouteValue.ReplaceAllString(expression, `[^/]+`) + "$"
	return userKeyRule{methods: methods, pattern: regexp.MustCompile(expression), scope: scope, place: place, param: param, path: pattern}
}

// userKeyEach makes a rule for each of several routes, written "METHOD
// /path" (methods may be several, "PUT DELETE /path"), so each is named by
// its own path.
func userKeyEach(rule func(methods, path string) userKeyRule, routes ...string) []userKeyRule {
	rules := make([]userKeyRule, 0, len(routes))
	for _, route := range routes {
		methods, path, _ := strings.Cut(route, " /")
		rules = append(rules, rule(methods, "/"+path))
	}
	return rules
}

// closedBecause closes a route to every key, saying why.
func (rule userKeyRule) closedBecause(reason string) userKeyRule {
	rule.scope, rule.reason = userKeyScopeClosed, reason
	return rule
}

// byAction holds each action a body names to its own scope; another action
// needs the rule's.
func (rule userKeyRule) byAction(scopes map[string]userKeyScope) userKeyRule {
	rule.actionScopes = scopes
	return rule
}

// scopeFor is the scope this request needs, by the action its body names.
func (rule userKeyRule) scopeFor(body []byte) userKeyScope {
	if rule.actionScopes == nil {
		return rule.scope
	}
	request := struct {
		Action string `json:"action"`
	}{}
	if json.Unmarshal(body, &request) == nil {
		if scope, known := rule.actionScopes[request.Action]; known {
			return scope
		}
	}
	return rule.scope
}

// userKeyActionScopesText says which action needs which scope, by name.
func userKeyActionScopesText(scopes map[string]userKeyScope) string {
	actions := slices.Sorted(maps.Keys(scopes))
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		parts = append(parts, action+" "+userKeyScopeNames[scopes[action]])
	}
	return strings.Join(parts, ", ")
}

// askedEveryTime asks the person each time, as the conversation asks
// (mcp_action_approvals.go), however the token is set.
func (rule userKeyRule) askedEveryTime() userKeyRule {
	rule.confirm, rule.everyTime = true, true
	return rule
}

// askedWhen asks the person, as askedEveryTime does, only for the requests
// asks says loosen what the key does without asking (its own token's
// "모두 맡기기" turned on, mcp_token_hand_over.go) or change a safeguard
// among other settings (the server's sign-up and sign-in,
// administrator_settings_safeguards.go); the others go ahead.
func (rule userKeyRule) askedWhen(asks func(app core.App, key userKeyAuthority, body []byte) bool) userKeyRule {
	rule.confirm, rule.everyTime, rule.asksWhen = true, true, asks
	return rule
}

// confirmedFor asks only for the actions a body names that can't be taken back.
func (rule userKeyRule) confirmedFor(actions ...string) userKeyRule {
	rule.confirm, rule.confirmActions = true, actions
	return rule
}

// asksConfirmation says whether this request needs a second step.
func (rule userKeyRule) asksConfirmation(app core.App, key userKeyAuthority, body []byte) bool {
	if rule.confirm && rule.asksWhen != nil {
		return rule.asksWhen(app, key, body)
	}
	if !rule.confirm || rule.confirmActions == nil {
		return rule.confirm
	}
	request := struct {
		Action string `json:"action"`
	}{}
	return json.Unmarshal(body, &request) != nil || slices.Contains(rule.confirmActions, request.Action)
}

func (rule userKeyRule) operations() userKeyRule { rule.byOperations = true; return rule }
func (rule userKeyRule) persons() userKeyRule    { rule.own = true; return rule }
func (rule userKeyRule) crossing() userKeyRule   { rule.own, rule.across = true, true; return rule }
func (rule userKeyRule) allows(method string) bool {
	return strings.Contains(" "+rule.methods+" ", " "+method+" ")
}

const (
	userKeyReads  = "GET"
	userKeyWrites = "POST PUT PATCH DELETE"
	userKeyAll    = "GET POST PUT PATCH DELETE"
)

// First match wins: what a key never reaches, then what needs no sign-in or
// takes only its own code, then the person's own and the server's
// administration, each by its own path (user_key_account_routes.go,
// user_key_admin_routes.go), then the person's own more broadly, then each
// write route by its own path (user_key_write_routes.go), then reads, which
// may be broad.
var userKeyRules = userKeyServerWide(slices.Concat(userKeyClosedRules, userKeyKeylessRules, userKeyPublicRules, userKeyCodeRules,
	userKeyOwnRules, userKeyAdminRules, userKeyPersonRules, userKeyWriteRules, userKeyReadRules))

// userKeyServerWide makes the server's administration and an organization's
// settings one rule, whichever list names them: they reach past one
// workspace, so only a key for all of its person's workspaces does them,
// never an app's token (user_key_admin_routes.go).
func userKeyServerWide(rules []userKeyRule) []userKeyRule {
	for index, rule := range rules {
		if rule.scope == userKeyScopeAdmin && (strings.HasPrefix(rule.path, "/admin/") || strings.HasPrefix(rule.path, "/organizations/")) {
			rules[index] = rule.crossing()
		}
	}
	return rules
}

var userKeyClosedRules = []userKeyRule{
	// SCIM is an identity provider's protocol, answered by its own tokens; no
	// person does it on a screen.
	userKeyRoute(userKeyAll, "/scim/v2/{rest}", userKeyScopeClosed, userKeyPlacePerson, ""),
}

// What only signing in used to do — sign-in, credentials, sessions, keys and
// tokens, first setup, joining and making workspaces — a person's key does
// too, through the same handlers and the same confirmations they ask for
// (agent full-permission decision of 2026-10-16, 1–2). An app's token never
// does it, and what crosses workspaces needs a key for all of them.
var userKeyPersonRules = []userKeyRule{
	userKeyRoute(userKeyAll, "/auth/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/auth/{a}/{b}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/auth/{a}/{b}/{c}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/setup/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	// The web agents waiting in the desktop app's window for its owner's
	// answer, as their connection requests are the person's own.
	userKeyRoute(userKeyReads, "/local-owner/agent-requests", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/notion/oauth/authorizations", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/notion/oauth/requests", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/notification-email/unsubscribe", userKeyScopeAdmin, userKeyPlacePerson, "").persons(),
	userKeyRoute(userKeyAll, "/account/password", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/emails", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/emails/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/mfa", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/mfa/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/mfa/{a}/{b}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/passkeys", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/passkeys/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/sessions", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/sessions/{sessionId}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyReads, "/account/deletion", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/auth-audit", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/developer-ids", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/account/web-push/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").persons(),
	// Keys and connections: what a key makes ends with it (mcp_connection_children.go).
	userKeyRoute(userKeyAll, "/mcp/{rest}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/mcp/{a}/{b}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/mcp/{a}/{b}/{c}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/mcp/oauth/requests/{request}/{action}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/workspaces/{workspaceId}/personal-access-tokens", userKeyScopeAdmin, userKeyPlaceWorkspaceParam, "workspaceId").persons(),
	userKeyRoute(userKeyAll, "/workspaces/{workspaceId}/personal-access-tokens/{tokenId}/revoke", userKeyScopeAdmin, userKeyPlaceWorkspaceParam, "workspaceId").persons().askedWhereItActs(),
	userKeyRoute(userKeyAll, "/workspaces/{workspaceId}/connections", userKeyScopeAdmin, userKeyPlaceWorkspaceParam, "workspaceId").persons(),
	userKeyRoute(userKeyAll, "/workspaces/{workspaceId}/connections/{grantId}/disconnect", userKeyScopeAdmin, userKeyPlaceWorkspaceParam, "workspaceId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/notion-admin-tokens", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/notion-admin-tokens/{tokenId}/revoke", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons().askedWhereItActs(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/scim/tokens", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/scim/tokens/{tokenId}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/mcp-governance/{rest}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}/clients", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}/clients/{clientId}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/organizations/{organizationId}/user-key-policy", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").persons(),
	userKeyRoute(userKeyAll, "/admin/accounts/{accountId}/password", userKeyScopeAdmin, userKeyPlacePerson, "").persons(),
	userKeyRoute(userKeyReads, "/collaboration/{room}", userKeyScopeRead, userKeyPlacePageParam, "room").persons(),
	userKeyRoute(userKeyAll, "/collaboration-state/{room}", userKeyScopeRead, userKeyPlacePageParam, "room").persons(),
	userKeyRoute(userKeyAll, "/collaboration-status/{room}", userKeyScopeRead, userKeyPlacePageParam, "room").persons(),
	userKeyRoute(userKeyReads, "/organizations", userKeyScopeRead, userKeyPlacePerson, "").crossing(),
	userKeyRoute("POST", "/workspaces", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyReads, "/apps", userKeyScopeRead, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/apps/{appId}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/workspaces/{workspaceId}/join", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyAll, "/share-links/join", userKeyScopeAdmin, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/deleted-databases", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId").persons(),
	userKeyRoute("POST", "/workspaces/{workspaceId}/deleted-databases/{blockId}/restore", userKeyScopeStructure, userKeyPlaceWorkspaceParam, "workspaceId").persons(),
	// Deleting a database from the trash for good can't be undone, as a page's purge can't.
	userKeyRoute("POST", "/workspaces/{workspaceId}/deleted-databases/{blockId}/delete", userKeyScopeStructure, userKeyPlaceWorkspaceParam, "workspaceId").persons().askedWhereItActs(),
	userKeyRoute(userKeyReads, "/bootstrap", userKeyScopeRead, userKeyPlacePerson, "").crossing(),
	userKeyRoute(userKeyWrites, "/workspaces/{workspaceId}/apps/{appId}", userKeyScopeAdmin, userKeyPlaceWorkspaceParam, "workspaceId").persons(),
}

// Open to anyone: a key adds nothing.
var userKeyPublicRules = []userKeyRule{
	userKeyRoute(userKeyAll, "/health", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/runtime-config", userKeyScopeRead, userKeyPlacePublic, ""),
	// Each read a batch names is checked as its own request (read_batch.go).
	userKeyRoute(userKeyReads, "/read-batch", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/server-identity", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/public-content", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/public-content/{source}/{key}/files/{fileId}", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/public-content/{source}/{key}/files/{fileId}/html-frame", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/public-content/{source}/{key}/cover", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/public-content/{source}/{key}/emoji/{token}", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/public-sites/{slug}/assets/{asset}", userKeyScopeRead, userKeyPlacePublic, ""),
	userKeyRoute(userKeyAll, "/file-capabilities/{token}", userKeyScopeRead, userKeyPlacePublic, ""),
	// A large answer's own capability in the path decides (native_operation_answer_downloads.go).
	userKeyRoute(userKeyAll, "/operation-answers/{token}", userKeyScopeRead, userKeyPlacePublic, ""),
	// The upload's own capability in the path decides.
	userKeyRoute(userKeyAll, "/mcp-file-uploads/{token}", userKeyScopeRead, userKeyPlacePublic, ""),
}

var userKeyReadRules = []userKeyRule{
	// The key's own person.
	userKeyRoute(userKeyReads, "/account/{setting}", userKeyScopeRead, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/operations", userKeyScopeRead, userKeyPlacePerson, ""),
	// An app's own outside sign-ins: the route takes only that app's token.
	userKeyRoute(userKeyReads, "/apps/{installId}/oauth", userKeyScopeRead, userKeyPlacePerson, ""),
	// Where an app's own data went: the route takes only an app's token or account.
	userKeyRoute(userKeyReads, "/app-data", userKeyScopeRead, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/app-people", userKeyScopeRead, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/app-groups", userKeyScopeRead, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/app-workspaces", userKeyScopeRead, userKeyPlacePerson, ""),
	// Server and organization administration.
	userKeyRoute(userKeyReads, "/admin/{rest}", userKeyScopeAdmin, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/admin/{a}/{b}", userKeyScopeAdmin, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/admin/{a}/{b}/{c}", userKeyScopeAdmin, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/admin/{a}/{b}/{c}/{d}", userKeyScopeAdmin, userKeyPlacePerson, ""),
	userKeyRoute(userKeyReads, "/organizations/{organizationId}/{a}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId"),
	userKeyRoute(userKeyReads, "/organizations/{organizationId}/{a}/{b}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId"),
	userKeyRoute(userKeyReads, "/organizations/{organizationId}/{a}/{b}/{c}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId"),
	userKeyRoute(userKeyReads, "/organizations/{organizationId}/{a}/{b}/{c}/{d}", userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId"),
	// Workspaces, pages, rows and files, inside the key's workspaces.
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/activities", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId"),
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/{a}", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId"),
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/{a}/{b}", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId"),
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/{a}/{b}/{c}", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId"),
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/{a}/{b}/{c}/{d}", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId"),
	userKeyRoute(userKeyReads, "/workspaces/{workspaceId}/{a}/{b}/{c}/{d}/{e}", userKeyScopeRead, userKeyPlaceWorkspaceParam, "workspaceId"),
	userKeyRoute(userKeyReads, "/pages/{id}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	userKeyRoute(userKeyReads, "/pages/{id}/{a}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	userKeyRoute(userKeyReads, "/pages/{id}/{a}/{b}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	userKeyRoute(userKeyReads, "/pages/{id}/{a}/{b}/{c}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	userKeyRoute(userKeyReads, "/pages/{id}/{a}/{b}/{c}/{d}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	userKeyRoute(userKeyReads, "/pages/{id}/{a}/{b}/{c}/{d}/{e}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	userKeyRoute(userKeyReads, "/files/{id}", userKeyScopeRead, userKeyPlaceFileParam, "id"),
	userKeyRoute(userKeyReads, "/database-rows/{rowId}/{a}", userKeyScopeRead, userKeyPlaceRowParam, "rowId"),
	userKeyRoute(userKeyReads, "/database-rows/{rowId}/{a}/{b}", userKeyScopeRead, userKeyPlaceRowParam, "rowId"),
	userKeyRoute(userKeyReads, "/database-containers/{containerId}/preview", userKeyScopeRead, userKeyPlaceContainerParam, "containerId"),
	userKeyRoute(userKeyReads, dataSourceMentionPreviewPath, userKeyScopeRead, userKeyPlaceDataSourceParam, "dataSourceId"),
	userKeyRoute(userKeyReads, "/database-containers/{containerId}/starter-views", userKeyScopeRead, userKeyPlaceContainerParam, "containerId"),
	userKeyRoute(userKeyReads, "/data-sources/{dataSourceId}/properties/{propertyId}/access", userKeyScopeRead, userKeyPlaceDataSourceParam, "dataSourceId"),
	userKeyRoute(userKeyReads, "/data-sources/{dataSourceId}/unique-id", userKeyScopeRead, userKeyPlaceDataSourceParam, "dataSourceId"),
	userKeyRoute(userKeyReads, "/page-links/{id}", userKeyScopeRead, userKeyPlacePageParam, "id"),
	// A workspace's own emoji, read where that workspace is the key's.
	userKeyRoute(userKeyReads, "/custom-emojis/{emojiId}", userKeyScopeRead, userKeyPlacePerson, "").reachingPath("emojiId"),
	// Work a person started, found by its job.
	userKeyRoute(userKeyReads, "/jobs/{id}", userKeyScopeRead, userKeyPlaceJobParam, "id"),
	userKeyRoute(userKeyReads, "/block-moves/{jobId}", userKeyScopeRead, userKeyPlaceJobParam, "jobId"),
	userKeyRoute(userKeyReads, "/page-duplications/{jobId}", userKeyScopeRead, userKeyPlaceJobParam, "jobId"),
	userKeyRoute(userKeyReads, "/page-workspace-moves/{jobId}", userKeyScopeRead, userKeyPlaceJobParam, "jobId"),
	userKeyRoute(userKeyReads, "/wiki-mutations/{jobId}", userKeyScopeRead, userKeyPlaceJobParam, "jobId"),
	// The opens of a view link the person made, read as its revoke is made
	// (user_key_write_routes.go): an agent that made a link can see who opened
	// it (2026-11-07: its key could make and revoke a link but not read this).
	userKeyRoute(userKeyReads, "/view-links/{viewLinkId}/opens", userKeyScopeRead, userKeyPlacePerson, ""),
}

// The content-graph operations that change a database's structure rather
// than its content.
var userKeyStructureOperations = map[string]bool{
	"upsertDataSource": true, "deleteDataSource": true, "upsertView": true, "updateView": true,
	"deleteView": true, "upsertProperty": true, "deleteProperty": true, "configureDependency": true,
	"upsertDependencyEdge": true, "deleteDependencyEdge": true,
}

func userKeyRuleFor(method, path string) (userKeyRule, bool) {
	for _, rule := range userKeyRules {
		if rule.allows(method) && rule.pattern.MatchString(path) {
			return userKeyHeldToReading(rule, method), true
		}
	}
	return userKeyRule{}, false
}
