package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app may be granted to an organization's groups, made by hand or kept by
// SCIM (document apps decision of 2026-10-10, 10): the grant names the group,
// never its people, so whoever is in the group now may use the app, and one
// SCIM takes out of the group may not. The hourly pass over app workspaces
// takes people out of them as for any other grant change.

const (
	appGrantGroupPrefix = "group:"
	// An app asks after this many groups in one read of its people.
	appGrantsMaxGroups = 100
	appGroupsWindow    = 500
)

var errAppGroupCursorInvalid = errors.New("app group cursor invalid")

// appPersonGroupSubjects are the grant subjects of the groups of an
// organization the person is in now.
func appPersonGroupSubjects(app core.App, tenancyKey, userID string) ([]string, error) {
	organizationID := appTenancyOrganization(tenancyKey)
	if organizationID == "" || userID == "" {
		return nil, nil
	}
	subjects := []string{}
	err := app.DB().NewQuery(`
		SELECT {:prefix} || [[link]].[[group_id]] FROM {{leafen_organization_group_members}} [[link]]
		JOIN {{leafen_organization_groups}} [[group]] ON [[group]].[[id]] = [[link]].[[group_id]]
		JOIN {{leafen_organization_memberships}} [[membership]] ON [[membership]].[[id]] = [[link]].[[organization_member_id]]
		WHERE [[group]].[[organization_id]] = {:organizationId} AND [[membership]].[[user]] = {:userId} AND [[membership]].[[status]] = {:active}
		UNION
		SELECT {:prefix} || [[link]].[[group_id]] FROM {{leafen_scim_group_members}} [[link]]
		JOIN {{leafen_scim_groups}} [[group]] ON [[group]].[[id]] = [[link]].[[group_id]]
			AND [[link]].[[generation]] = [[group]].[[active_generation]] AND [[group]].[[deleted_at]] = 0
		JOIN {{leafen_organization_memberships}} [[membership]] ON [[membership]].[[id]] = [[link]].[[organization_member_id]]
		WHERE [[group]].[[organization_id]] = {:organizationId} AND [[membership]].[[user]] = {:userId} AND [[membership]].[[status]] = {:active}
	`).Bind(dbx.Params{"prefix": appGrantGroupPrefix, "organizationId": organizationID, "userId": userID,
		"active": organizationMemberStatusActive}).Column(&subjects)
	return subjects, err
}

// appGrantGroupWindow is a window of an organization's groups an app may be
// granted to, by name from the cursor (the last one's normalized name and
// id) on, with how many people each has now; next goes on, "" at the end.
func appGrantGroupWindow(app core.App, tenancyKey, cursor string) ([]appGroupWire, string, error) {
	organizationID := appTenancyOrganization(tenancyKey)
	groups := []appGroupWire{}
	if organizationID == "" {
		return groups, "", nil
	}
	afterName, afterID := "", ""
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		name, id, found := strings.Cut(string(decoded), "\x00")
		if err != nil || !found || id == "" {
			return nil, "", errAppGroupCursorInvalid
		}
		afterName, afterID = name, id
	}
	rows := []struct {
		appGroupWire
		Key string `db:"sort_name"`
	}{}
	err := app.DB().NewQuery(`
		SELECT * FROM (
			SELECT [[group]].[[id]], [[group]].[[name]], 'manual' AS [[managed_by]], [[group]].[[normalized_name]] AS [[sort_name]],
				(SELECT COUNT(*) FROM {{leafen_organization_group_members}} [[link]] WHERE [[link]].[[group_id]] = [[group]].[[id]]) AS [[members]]
			FROM {{leafen_organization_groups}} [[group]]
			WHERE [[group]].[[organization_id]] = {:organizationId}
				AND ([[group]].[[normalized_name]], [[group]].[[id]]) > ({:afterName}, {:afterID})
			ORDER BY [[group]].[[normalized_name]], [[group]].[[id]] LIMIT {:limit}
		)
		UNION ALL
		SELECT * FROM (
			SELECT [[group]].[[id]], [[group]].[[display_name]], 'scim', [[group]].[[normalized_name]],
				(SELECT COUNT(*) FROM {{leafen_scim_group_members}} [[link]] WHERE [[link]].[[group_id]] = [[group]].[[id]]
					AND [[link]].[[generation]] = [[group]].[[active_generation]])
			FROM {{leafen_scim_groups}} [[group]]
			WHERE [[group]].[[organization_id]] = {:organizationId} AND [[group]].[[deleted_at]] = 0
				AND ([[group]].[[normalized_name]], [[group]].[[id]]) > ({:afterName}, {:afterID})
			ORDER BY [[group]].[[normalized_name]], [[group]].[[id]] LIMIT {:limit}
		)
		ORDER BY [[sort_name]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"organizationId": organizationID, "afterName": afterName, "afterID": afterID,
		"limit": appGroupsWindow + 1}).All(&rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > appGroupsWindow {
		rows = rows[:appGroupsWindow]
		last := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.Key + "\x00" + last.ID))
	}
	for _, row := range rows {
		groups = append(groups, row.appGroupWire)
	}
	return groups, next, nil
}

// validAppGrantGroups holds a grant's groups to its organization's, each
// found by its id.
func validAppGrantGroups(app core.App, tenancyKey string, groupIDs []string) (bool, error) {
	if len(groupIDs) == 0 {
		return true, nil
	}
	organizationID := appTenancyOrganization(tenancyKey)
	if organizationID == "" {
		return false, nil
	}
	for _, id := range groupIDs {
		var known bool
		if err := app.DB().NewQuery(`
			SELECT EXISTS (SELECT 1 FROM {{leafen_organization_groups}} WHERE [[id]] = {:id} AND [[organization_id]] = {:organizationId})
				OR EXISTS (SELECT 1 FROM {{leafen_scim_groups}} WHERE [[id]] = {:id} AND [[organization_id]] = {:organizationId} AND [[deleted_at]] = 0)
		`).Bind(dbx.Params{"id": strings.TrimSpace(id), "organizationId": organizationID}).Row(&known); err != nil {
			return false, err
		}
		if !known {
			return false, nil
		}
	}
	return true, nil
}

// appGrantSubjects are who a person counts as in a grant: everyone, themself
// and their groups.
func appGrantSubjects(app core.App, tenancyKey, userID string) (string, error) {
	groups, err := appPersonGroupSubjects(app, tenancyKey, userID)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(append([]string{"*", userID}, groups...))
	return string(encoded), err
}
