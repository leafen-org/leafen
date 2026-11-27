package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's "워크스페이스 소유자 + 선택된 그룹" (seen 2026-10-02 in the
// connections' permissions, its own picker behind its Enterprise plan): the
// workspace's owners and the people of chosen groups may make personal
// access tokens. Leafen's groups are the organization's (the "조직" item).

const (
	workspacePolicyOwnersAndGroups = "owners_and_groups"
	// The policy's group choices are read this many at a time.
	workspacePolicyGroupWindow = 500
)

type workspacePolicyGroup struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// workspacePolicyLimit is who a policy leaves a thing to: everyone, or the
// owners and the people of some groups.
type workspacePolicyLimit struct {
	OwnersOnly bool
	Groups     []string
}

func customEmojiLimit(policies workspaceConnectionPolicies) workspacePolicyLimit {
	return workspacePolicyLimit{OwnersOnly: policies.CustomEmojiOwnersOnly}
}

func personalAccessTokensLimit(policies workspaceConnectionPolicies) workspacePolicyLimit {
	limit := workspacePolicyLimit{OwnersOnly: policies.PersonalAccessTokens != workspacePolicyMembers}
	if policies.PersonalAccessTokens == workspacePolicyOwnersAndGroups {
		limit.Groups = policies.PersonalAccessTokenGroups
	}
	return limit
}

// requireWorkspacePolicy returns denied when the workspace leaves what a
// person asks for to its owners, and to groups the person isn't in.
func requireWorkspacePolicy(
	app core.App, workspaceID, principalID, role string,
	limit func(workspaceConnectionPolicies) workspacePolicyLimit, denied error,
) error {
	policies, err := loadWorkspaceConnectionPolicies(app, workspaceID)
	if err != nil {
		return err
	}
	applied := limit(policies)
	if !applied.OwnersOnly || role == workspaceRoleOwner {
		return nil
	}
	if len(applied.Groups) > 0 && principalID != "" {
		encoded, err := json.Marshal(applied.Groups)
		if err != nil {
			return err
		}
		var member int
		if err := app.DB().NewQuery(`
			SELECT EXISTS (SELECT 1 FROM {{leafen_organization_group_members}} [[member]]
				JOIN {{leafen_organization_memberships}} [[membership]]
					ON [[membership]].[[id]] = [[member]].[[organization_member_id]]
				WHERE [[membership]].[[user]] = {:userId} AND [[membership]].[[status]] = {:active}
					AND [[member]].[[group_id]] IN (SELECT [[value]] FROM json_each({:groups})))
		`).Bind(dbx.Params{"userId": principalID, "active": organizationMemberStatusActive, "groups": string(encoded)}).
			Row(&member); err != nil {
			return err
		}
		if member == 1 {
			return nil
		}
	}
	return denied
}

// workspacePolicyGroupChoices are a window of the groups of the workspace's
// organization a policy may name, by name from the cursor, and where the
// next window starts ("" at the end).
func workspacePolicyGroupChoices(app core.App, organizationID, cursor string) ([]workspacePolicyGroup, string, error) {
	afterName, afterID := "", ""
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		name, id, found := strings.Cut(string(decoded), "\x00")
		if err != nil || !found || id == "" {
			return nil, "", errWorkspaceConnectionPolicyInvalid
		}
		afterName, afterID = name, id
	}
	rows := []struct {
		workspacePolicyGroup
		Key string `db:"normalized_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[normalized_name]] FROM {{leafen_organization_groups}}
		WHERE [[organization_id]] = {:organizationId} AND ([[normalized_name]], [[id]]) > ({:afterName}, {:afterID})
		ORDER BY [[normalized_name]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "afterName": afterName, "afterID": afterID, "limit": workspacePolicyGroupWindow + 1,
	}).All(&rows); err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > workspacePolicyGroupWindow {
		rows = rows[:workspacePolicyGroupWindow]
		last := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.Key + "\x00" + last.ID))
	}
	groups := make([]workspacePolicyGroup, len(rows))
	for index, row := range rows {
		groups[index] = row.workspacePolicyGroup
	}
	return groups, next, nil
}

// validWorkspacePolicyGroups holds named groups to the organization's.
func validWorkspacePolicyGroups(app core.App, organizationID string, groups []string) (bool, error) {
	// Each named group is found by its id, however many the organization has.
	distinct := map[string]struct{}{}
	for _, group := range groups {
		distinct[group] = struct{}{}
	}
	encoded, err := json.Marshal(groups)
	if err != nil {
		return false, err
	}
	var found int
	if err := app.DB().NewQuery(`
		SELECT COUNT(DISTINCT [[id]]) FROM {{leafen_organization_groups}}
		WHERE [[organization_id]] = {:organizationId} AND [[id]] IN (SELECT [[value]] FROM json_each({:groups}))
	`).Bind(dbx.Params{"organizationId": organizationID, "groups": string(encoded)}).Row(&found); err != nil {
		return false, err
	}
	return found == len(distinct), nil
}
