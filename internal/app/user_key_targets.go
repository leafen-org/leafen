package app

import (
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A call made with a key acts in a workspace the key names, and so must
// everything else it reaches: where it copies or moves something to, the
// source it reads, the pages it lists. A rule names the body fields and path
// values that hold those; each one that names something Leafen keeps must be
// in the key's workspaces, or the call reads as not found, as on screen for a
// workspace that isn't the person's. A value naming nothing known is left to
// the handler, which refuses it as it would for anyone.

// reaching names body fields (a string or a list of them) holding what the
// call reaches besides where it acts.
func (rule userKeyRule) reaching(fields ...string) userKeyRule {
	rule.bodyTargets = append(slices.Clone(rule.bodyTargets), fields...)
	return rule
}

// reachingPath names path values holding what the call reaches.
func (rule userKeyRule) reachingPath(names ...string) userKeyRule {
	rule.pathTargets = append(slices.Clone(rule.pathTargets), names...)
	return rule
}

// userKeyTargetsWithin reports whether everything a call reaches is in the
// key's workspaces, and those workspaces.
func userKeyTargetsWithin(app core.App, rule userKeyRule, path string, body userKeyBody, authority userKeyAuthority) ([]string, bool, error) {
	ids := []string{}
	// Every spelling the handler may read a field by, each value.
	for _, field := range rule.bodyTargets {
		ids = append(ids, body.values(field)...)
	}
	if len(rule.pathTargets) > 0 {
		patternSegments := strings.Split(strings.Trim(rule.path, "/"), "/")
		pathSegments := strings.Split(strings.Trim(strings.TrimPrefix(path, apiRoot), "/"), "/")
		for _, name := range rule.pathTargets {
			if index := slices.Index(patternSegments, "{"+name+"}"); index >= 0 && index < len(pathSegments) {
				ids = append(ids, pathSegments[index])
			}
		}
	}
	reached := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		workspaces, err := userKeyEntityWorkspaces(app, id)
		if err == nil && len(workspaces) == 0 {
			// A data source named as an agent's tools name it, by its public
			// ID or collection://, is the one the handler reads (databaseRowPageMoveSource).
			workspaces, err = userKeyEntityWorkspaces(app, databaseRowPageMoveSource(app, id))
		}
		if err != nil {
			return nil, false, err
		}
		for _, workspaceID := range workspaces {
			if !slices.Contains(authority.WorkspaceIDs, workspaceID) {
				return nil, false, nil
			}
		}
		reached = append(reached, workspaces...)
	}
	return reached, true, nil
}

// userKeyEntityWorkspaces finds the workspace of whatever an id names: a
// workspace, page, data source, row, database, view, teamspace, deadline
// notice, file or custom emoji.
func userKeyEntityWorkspaces(app core.App, id string) ([]string, error) {
	workspaces := []string{}
	err := app.DB().NewQuery(`
		SELECT DISTINCT [[workspace_id]] FROM (
			SELECT [[id]] AS [[workspace_id]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id} OR [[block_id]] = {:id}
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_database_views}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_teamspaces}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_deadline_notices}} WHERE [[id]] = {:id}
			UNION ALL SELECT [[workspace]] FROM {{leafen_files}} WHERE [[id]] = {:id} AND [[workspace]] != ''
			UNION ALL SELECT [[workspace_id]] FROM {{leafen_custom_emojis}} WHERE [[id]] = {:id}
		)
	`).Bind(dbx.Params{"id": id}).Column(&workspaces)
	return workspaces, err
}

var userKeyPathPlaceholder = regexp.MustCompile(`\{[A-Za-z]+\}`)

// userKeyRuleNamesItsRoute reports whether a rule names a route by its own
// path rather than covering it with a wider one.
func userKeyRuleNamesItsRoute(rule userKeyRule, routePath string) bool {
	return userKeyPathPlaceholder.ReplaceAllString(rule.path, "{}") == userKeyPathPlaceholder.ReplaceAllString(routePath, "{}")
}
