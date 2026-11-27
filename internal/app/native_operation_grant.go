package app

import (
	"maps"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What this connection can run, said beside each operation leafen_operations
// (GET /operations with a key) finds or describes, with the grant it holds
// once in the answer (2026-11-23, the user: an agent connected with their
// administrator's grant refused to make an account and told the person to
// sign up first — "에이전트가 내부적으로 내권한으로 할수있는지 한번 점검한후에
// 할수있으면 군말없이 해줘야지", "에이전트 받은 권한 말이야. 사람의 권한이
// 아니라"). It is judged as the key's check judges a call before its handler
// (user_key_auth.go): the scope the operation needs — by its action, or by
// the structural operations of its body — against the connection's, held to
// what its workspaces' organizations allow keys; whether it needs a key for
// all the person's workspaces; whether a key runs it at all. An /admin/
// operation is this server's administrator's (authorizeAdministratorRequest),
// which the connection's person is or isn't. What a handler then checks of
// one page or workspace — the person's role there — the call answers.

// nativeOperationGrant is the grant of the connection asking.
type nativeOperationGrant struct {
	key  userKeyAuthority
	kind connectionKind
	// allows is the scope its workspaces' organizations hold keys to.
	allows        userKeyScope
	reachesAll    bool
	administrator bool
	// language is its person's screen's, in which an operation only they do
	// names its place (user_key_person_only_screens.go).
	language string
}

// nativeOperationConnection is the grant as an answer gives it.
type nativeOperationConnection struct {
	// Kind is "agent connection" — its person's, everything they may in all
	// their workspaces —, "MCP token" — one workspace, the tools its page
	// leaves on, as Notion's — or "app": an installed app's, one registered
	// with Leafen or granted the native API (connection_kind_ways.go).
	Kind  string `json:"kind"`
	Scope string `json:"scope"`
	// OrganizationsAllow is the scope its workspaces' organizations hold keys
	// to, where below its own.
	OrganizationsAllow string `json:"organizations_allow,omitempty"`
	AllWorkspaces      bool   `json:"all_workspaces"`
	Workspaces         int    `json:"workspaces"`
	// Administrator is its person being this server's administrator, whose
	// every /admin/ operation is.
	Administrator bool `json:"administrator"`
	// Widen is how it gets more, where it has less than all.
	Widen string `json:"widen,omitempty"`
}

// What this_connection says of an operation.
const (
	nativeOperationCan           = "can"
	nativeOperationNotWithKey    = "not with a key: its person does it logged in on Leafen's screen"
	nativeOperationNotWithApp    = "not with this connection: an app's own"
	nativeOperationNotItsPerson  = "not its person: this server's administrator's"
	nativeOperationLacksAll      = "lacks all workspaces"
	nativeOperationLacksPrefix   = "lacks "
	nativeOperationPartlyPrefix  = "can, but lacks "
	nativeOperationOrganizations = ": its organization allows keys up to "
)

// nativeOperationWiden is how an app's connection granted over OAuth hears
// it comes to do more, as it always has; an agent connection and an MCP
// token hear their own kind's way (connectionWiden).
const nativeOperationWiden = "A connection does what its person may, less the tools its token's page turned off and what its " +
	"workspaces' organizations hold keys to. To turn a tool back on: leafen_call PATCH " + apiRoot + "/mcp/connections/{its id}/settings " +
	"with its disabledTools (Leafen asks your person in the conversation); what keys may do in an organization its administrators set. " +
	"An MCP token stays in its one workspace: an agent connection, by a connection code or `leafen connect`, reaches all its person's."

// nativeOperationGrantOf reads a key's grant.
func nativeOperationGrantOf(app core.App, key userKeyAuthority) (*nativeOperationGrant, error) {
	grant := &nativeOperationGrant{key: key, allows: userKeyScopeAdmin}
	var err error
	if key.AppInstallID == "" {
		if grant.allows, err = userKeyPolicyCap(app, key.WorkspaceIDs); err != nil {
			return nil, err
		}
	}
	// The connection's, whatever workspace this call was held to (user_key_reach_held_by_call.go).
	if grant.reachesAll, err = userKeyReachesAllWorkspaces(app, userKeyWholeOf(key)); err != nil {
		return nil, err
	}
	if grant.administrator, err = isAdministrator(app, key.OwnerID); err != nil {
		return nil, err
	}
	grant.language = mcpScreenLanguage(app, key.OwnerID)
	grant.kind = connectionKindOf(app, key)
	return grant, nil
}

// connection is the grant as an answer gives it.
func (grant *nativeOperationGrant) connection() *nativeOperationConnection {
	connection := &nativeOperationConnection{Kind: string(grant.kind), Scope: userKeyScopeNames[grant.key.Scope], AllWorkspaces: grant.reachesAll,
		Workspaces: len(grant.key.WorkspaceIDs), Administrator: grant.administrator}
	if grant.allows < grant.key.Scope {
		connection.OrganizationsAllow = userKeyScopeNames[grant.allows]
	}
	if grant.key.AppInstallID == "" && (grant.key.Scope < userKeyScopeAdmin || !grant.reachesAll) {
		connection.Widen = connectionWiden(grant.kind)
	}
	return connection
}

// of says whether the connection can run an operation.
func (grant *nativeOperationGrant) of(operation nativeOperation) string {
	rule := operation.rule
	_, path, _ := strings.Cut(operation.ID, " ")
	switch why := nativeOperationNotCall(operation.ID); {
	case why != "":
		// No call runs it: why, and the way that does it (native_operation_not_calls.go).
		return nativeOperationNotCallPrefix + why
	case rule.scope == userKeyScopeClosed:
		// Its person's own, with where they do it, so the agent leads them
		// there instead of saying it can't be done or asking for more.
		return nativeOperationNotWithKey + userKeyPersonOnlyPlace(grant.language, path)
	case nativeOperationAppOnly(operation.ID) != "" || rule.own && grant.key.AppInstallID != "":
		return nativeOperationNotWithApp
	case strings.HasPrefix(path, "/admin/") && !grant.administrator:
		return nativeOperationNotItsPerson
	case rule.across && !grant.reachesAll:
		return nativeOperationLacksAll
	}
	least, most, more := rule.scope, rule.scope, "structural operations"
	if rule.byOperations {
		least, most = userKeyScopeContent, userKeyScopeStructure
	}
	if rule.actionScopes != nil {
		needing := []string{}
		for _, action := range slices.Sorted(maps.Keys(rule.actionScopes)) {
			scope := rule.actionScopes[action]
			least, most = min(least, scope), max(most, scope)
			if scope > min(grant.key.Scope, grant.allows) {
				needing = append(needing, action)
			}
		}
		// The actions it names none of take the rule's own scope.
		if rule.scope > min(grant.key.Scope, grant.allows) {
			needing = append(needing, "any other")
		}
		more = "its actions " + strings.Join(needing, ", ")
	}
	switch {
	case grant.key.Scope < least:
		return nativeOperationLacksPrefix + userKeyScopeNames[least]
	case grant.allows < least:
		return nativeOperationLacksPrefix + userKeyScopeNames[least] + nativeOperationOrganizations + userKeyScopeNames[grant.allows]
	case min(grant.key.Scope, grant.allows) < most:
		return nativeOperationPartlyPrefix + userKeyScopeNames[most] + " for " + more
	}
	return nativeOperationCan
}

// userKeyScopeRefusal is what a key below a call's scope hears: what it
// reaches, what the call needs, and how its kind comes to do more.
func userKeyScopeRefusal(app core.App, key userKeyAuthority, needs userKeyScope) string {
	return "This key reaches " + userKeyScopeNames[key.Scope] + "; this needs " + userKeyScopeNames[needs] + ". " +
		connectionWiden(connectionKindOf(app, key))
}
