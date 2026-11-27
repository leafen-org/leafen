package app

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// A key or agent connection does everything its person may (agent
// full-permission decision of 2026-10-16, 4): a route no rule names is not
// closed to it but left to the handler's own checks of the person, with the
// whole scope. The route can't say which workspace it acts in, so a key the
// person held to some of their workspaces stays out of it; an app's token
// acts within its grants and stays out too.
var userKeyUnclassifiedRule = userKeyRule{methods: userKeyAll, scope: userKeyScopeAdmin, place: userKeyPlacePerson, own: true, across: true}

// userKeyRuleOfRoute is the rule a registered route (below apiRoot, its
// path values as the router names them) is listed with, and whether a rule
// names it: a write a wider rule covers isn't named but waits for its own,
// so a new one is never opened to fewer workspaces by a wider rule. One no
// rule names is listed with userKeyUnclassifiedRule, which the key's check
// meets on it too.
func userKeyRuleOfRoute(method, path string) (userKeyRule, bool) {
	rule, found := userKeyRuleFor(method, path)
	if found && method != http.MethodGet && rule.scope != userKeyScopeClosed && !userKeyRuleNamesItsRoute(rule, path) {
		found = false
	}
	if !found {
		return userKeyUnclassifiedRule, false
	}
	return rule, true
}

// checkUnclassifiedKeyRoutes names, as the server starts, each route it
// registered that no rule names: a key held to some workspaces can't use
// one, so it shows the moment the server is first run, not only once
// something lists the operations (2026-11-13: eight such routes were named
// only then). A development build stops there, so a route added without its
// rule is classified before it is merged, not found in a log later
// (2026-11-23: the guest invite link's two routes were named only in the
// log, and every connection short of a key for all workspaces read as
// unable to make one); a release build starts as usual.
func checkUnclassifiedKeyRoutes(app core.App, group any) error {
	unclassified := []string{}
	for _, route := range nativeRoutes(group) {
		if _, named := userKeyRuleOfRoute(route.Method, route.Path); !named {
			unclassified = append(unclassified, route.Method+" "+route.Path)
		}
	}
	if len(unclassified) == 0 {
		return nil
	}
	if unclassifiedKeyRoutesStopStart {
		return fmt.Errorf("%d native routes aren't classified for keys: give each its rule in user_key_*_routes.go: %s",
			len(unclassified), strings.Join(unclassified, ", "))
	}
	log.Printf("leafen: %d native routes aren't classified for keys; they follow the person's own permission with the whole scope and a key for all their workspaces: %s",
		len(unclassified), strings.Join(unclassified, ", "))
	app.Logger().Warn("leafen native routes not classified for keys", "count", len(unclassified), "routes", unclassified)
	return nil
}

// userKeyRoutes matches a request against the API's own routes, built once
// for the router the server registered.
var userKeyRoutes = struct {
	sync.Mutex
	router *router.Router[*core.RequestEvent]
	mux    *http.ServeMux
}{}

// userKeyRouteExists says whether the API has a route for this method and
// path, so a key naming none hears that, not that it reaches too few
// workspaces (2026-11-07: leafen_call on a mistyped path answered "This key
// reaches only some of your workspaces"). Before the server registers its
// routes it says yes, keeping the refusal it gave.
func userKeyRouteExists(method, path string) bool {
	nativeOperationsState.Lock()
	group := nativeOperationsState.router
	nativeOperationsState.Unlock()
	if group == nil {
		return true
	}
	userKeyRoutes.Lock()
	defer userKeyRoutes.Unlock()
	if userKeyRoutes.router != group {
		mux := http.NewServeMux()
		for _, route := range nativeRoutes(group) {
			pattern := route.Path
			if route.Method != "" {
				pattern = route.Method + " " + pattern
			}
			func() {
				// A pattern the mux holds already, or can't read, adds nothing.
				defer func() { _ = recover() }()
				mux.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
			}()
		}
		userKeyRoutes.router, userKeyRoutes.mux = group, mux
	}
	_, pattern := userKeyRoutes.mux.Handler(&http.Request{Method: method, URL: &url.URL{Path: path}, Host: "leafen"})
	return pattern != ""
}

// userKeyPersonWorkspacesSQL selects, for {:person}, the workspaces a key
// for all of its person's names: the ones the person chooses from for a
// key. That is the person's list of workspaces (bootstrap's: one they own
// or are a member of, in an organization they are active in, not being
// deleted), apps' own workspaces and the ones an import is staging in set
// apart, as the connection screen sets them apart and leaves them
// unchosen. `leafen connect` and a connection asked for by code name these
// (userKeyChoosableWorkspaces), a key is made for all of them when it names
// every one (userKeyChoosesAll), and such a key reaches these as they come
// and go (keepUserKeyReachingAll): one set, so whoever has an import's
// staging workspace, a guest's place or an organization's other workspaces
// still gets a key for all of them (2026-11-13: the check counted the
// staging workspace no screen chose, and such a person's agent couldn't
// make or join a workspace). The person's own and member workspaces are
// read by their indexes, not by a scan of every workspace.
const userKeyPersonWorkspacesSQL = `
	SELECT [[workspace]].[[id]] AS [[id]] FROM {{leafen_workspaces}} [[workspace]]
	WHERE [[workspace]].[[id]] IN (
		SELECT [[own]].[[id]] FROM {{leafen_workspaces}} [[own]] WHERE [[own]].[[workspace_owner]] = {:person}
		UNION SELECT [[membership]].[[workspace_id]] FROM {{leafen_workspace_memberships}} [[membership]]
			WHERE [[membership]].[[user_id]] = {:person} AND [[membership]].[[role]] = 'member')
	AND EXISTS (SELECT 1 FROM {{leafen_organization_memberships}} [[organization_membership]]
		WHERE [[organization_membership]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[user]] = {:person} AND [[organization_membership]].[[status]] = {:active})
	AND [[workspace]].[[deletion_job_id]] = '' AND [[workspace]].[[app_install_id]] = ''
	AND NOT EXISTS (SELECT 1 FROM {{leafen_app_workspaces}} [[app_own]] WHERE [[app_own]].[[workspace_id]] = [[workspace]].[[id]])
	AND [[workspace]].[[id]] NOT IN (` + userKeyStagingWorkspacesSQL + `)`

// userKeyPersonWorkspaces are the workspaces a key for all of its person's
// names (userKeyPersonWorkspacesSQL).
func userKeyPersonWorkspaces(app core.App, personID string) ([]string, error) {
	workspaceIDs := []string{}
	err := app.DB().NewQuery(userKeyPersonWorkspacesSQL).
		Bind(dbx.Params{"person": personID, "active": organizationMemberStatusActive}).Column(&workspaceIDs)
	return workspaceIDs, err
}

// userKeyChoosesAll says whether a key naming these workspaces is made for
// all of its person's: it names every one they choose from, and they have
// one. Such a key is for all of them now and later (all_workspaces,
// schema_baseline.json).
func userKeyChoosesAll(app core.App, personID string, workspaceIDs []string) (bool, error) {
	choosable, err := userKeyPersonWorkspaces(app, personID)
	if err != nil || len(choosable) == 0 {
		return false, err
	}
	for _, workspaceID := range choosable {
		if !slices.Contains(workspaceIDs, workspaceID) {
			return false, nil
		}
	}
	return true, nil
}

// userKeyReachesAllWorkspaces says whether the key is for all of its
// person's workspaces — what it was made for, not how many it names
// (2026-11-13: when one of a person's two such keys made a workspace, the
// other, no longer naming every one, was refused what only they may do) —
// and reaches each of them now: one an organization holds its kind of
// connection out of keeps it from what crosses them.
func userKeyReachesAllWorkspaces(app core.App, authority userKeyAuthority) (bool, error) {
	if !authority.AllWorkspaces || authority.AppInstallID != "" {
		return false, nil
	}
	workspaceIDs, err := userKeyPersonWorkspaces(app, authority.OwnerID)
	if err != nil {
		return false, err
	}
	for _, workspaceID := range workspaceIDs {
		if !slices.Contains(authority.WorkspaceIDs, workspaceID) {
			return false, nil
		}
	}
	return true, nil
}

// userKeyEffectiveScope is what the key may do in these workspaces: its own
// scope, held to what their organizations allow keys. An app's token is
// held by its grants instead.
func userKeyEffectiveScope(app core.App, authority userKeyAuthority, workspaceIDs []string) (userKeyScope, error) {
	if authority.AppInstallID != "" {
		return authority.Scope, nil
	}
	if len(workspaceIDs) == 0 {
		workspaceIDs = authority.WorkspaceIDs
	}
	capped, err := userKeyPolicyCap(app, workspaceIDs)
	if err != nil {
		return userKeyScopeClosed, err
	}
	return min(authority.Scope, capped), nil
}
