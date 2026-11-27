package app

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	organizationSettingsGroupAuthorityManual = "manual"
	organizationSettingsGroupAuthoritySCIM   = "scim"
)

type storedOrganizationSettingsGroup struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	ManagedBy string `db:"managed_by"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
}

type storedOrganizationSettingsGroupMember struct {
	GroupID              string `db:"group_id"`
	ManagedBy            string `db:"managed_by"`
	ID                   string `db:"id"`
	OrganizationMemberID string `db:"organization_member_id"`
	UserID               string `db:"user_id"`
	DisplayName          string `db:"display_name"`
	Email                string `db:"email"`
	Role                 string `db:"role"`
	Status               string `db:"status"`
}

func loadOrganizationSettingsGroups(
	app core.App, organizationID, afterManagedBy, afterID, memberAfter string,
) ([]organizationSettingsGroup, string, string, string, bool, error) {
	// A page carries up to a window of groups, each with its first members;
	// a group with more members than a page holds ends the page and goes on
	// from where its members stopped (2026-11-06: one group a page took a
	// read per group each time the settings opened).
	rows := []storedOrganizationSettingsGroup{}
	condition := `[[managed_by]]>{:afterManagedBy}
		OR ([[managed_by]]={:afterManagedBy} AND [[id]]>{:afterId})`
	if memberAfter != "" {
		condition = `[[managed_by]]>{:afterManagedBy}
			OR ([[managed_by]]={:afterManagedBy} AND [[id]]>={:afterId})`
	}
	err := app.DB().NewQuery(`
		SELECT * FROM (
			SELECT [[id]], [[name]], 'manual' AS [[managed_by]], [[created_at]], [[updated_at]]
			FROM {{leafen_organization_groups}} WHERE [[organization_id]]={:organizationId}
			UNION ALL
			SELECT [[id]], [[display_name]] AS [[name]], 'scim' AS [[managed_by]], [[created_at]], [[updated_at]]
			FROM {{leafen_scim_groups}} WHERE [[organization_id]]={:organizationId} AND [[deleted_at]]=0
		) WHERE ` + condition + `
		ORDER BY [[managed_by]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "afterManagedBy": afterManagedBy,
		"afterId": afterID, "limit": organizationSettingsGroupPageSize + 1,
	}).All(&rows)
	if err != nil {
		return nil, "", "", "", false, err
	}
	// The group whose members were going on is gone: the next one starts fresh.
	if memberAfter != "" && (len(rows) == 0 || rows[0].ManagedBy != afterManagedBy || rows[0].ID != afterID) {
		memberAfter = ""
	}
	groups := make([]organizationSettingsGroup, 0, len(rows))
	for index, row := range rows {
		if index == organizationSettingsGroupPageSize {
			last := rows[index-1]
			return groups, last.ManagedBy, last.ID, "", false, nil
		}
		group, nextMember, membersDone, err := organizationSettingsGroupPage(
			app, organizationID, row, memberAfter,
		)
		if err != nil {
			return nil, "", "", "", false, err
		}
		memberAfter = ""
		groups = append(groups, group)
		if !membersDone {
			return groups, row.ManagedBy, row.ID, nextMember, false, nil
		}
	}
	return groups, "", "", "", true, nil
}

func organizationSettingsGroupPage(
	app core.App, organizationID string, group storedOrganizationSettingsGroup, memberAfter string,
) (organizationSettingsGroup, string, bool, error) {
	membershipTable, groupTable, generationJoin := "leafen_organization_group_members", "leafen_organization_groups", ""
	if group.ManagedBy == organizationSettingsGroupAuthoritySCIM {
		membershipTable, groupTable = "leafen_scim_group_members", "leafen_scim_groups"
		generationJoin = " AND [[link]].[[generation]]=[[group]].[[active_generation]]"
	}
	rows := []storedOrganizationSettingsGroupMember{}
	query := `SELECT [[link]].[[group_id]], '` + group.ManagedBy + `' AS [[managed_by]],
			[[membership]].[[id]], [[membership]].[[id]] AS [[organization_member_id]],
			[[membership]].[[user]] AS [[user_id]], [[user]].[[name]] AS [[display_name]],
			[[user]].[[email]], [[membership]].[[role]], [[membership]].[[status]]
			FROM {{` + membershipTable + `}} [[link]]
			JOIN {{` + groupTable + `}} [[group]] ON [[group]].[[id]]=[[link]].[[group_id]]` + generationJoin + `
			JOIN {{leafen_organization_memberships}} [[membership]] ON [[membership]].[[id]]=[[link]].[[organization_member_id]]
			JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user]]
			WHERE [[group]].[[organization_id]]={:organizationId} AND [[group]].[[id]]={:groupId}
				AND [[membership]].[[id]]>{:memberAfter}
			ORDER BY [[membership]].[[id]] LIMIT {:limit}`
	if err := app.DB().NewQuery(query).Bind(dbx.Params{
		"organizationId": organizationID, "groupId": group.ID,
		"memberAfter": memberAfter, "limit": organizationSettingsMemberPageSize + 1,
	}).All(&rows); err != nil {
		return organizationSettingsGroup{}, "", false, err
	}
	hasMore := len(rows) > organizationSettingsMemberPageSize
	if hasMore {
		rows = rows[:organizationSettingsMemberPageSize]
	}
	members := make([]organizationSettingsGroupMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, organizationSettingsGroupMember{
			ID: row.ID, OrganizationMemberID: row.OrganizationMemberID,
			UserID: row.UserID, DisplayName: row.DisplayName, Email: row.Email,
			Role: row.Role, Status: row.Status,
		})
	}
	var memberCount int
	countQuery := `SELECT COUNT(*) FROM {{` + membershipTable + `}} [[link]]
		JOIN {{` + groupTable + `}} [[group]] ON [[group]].[[id]]=[[link]].[[group_id]]` + generationJoin + `
		WHERE [[group]].[[organization_id]]={:organizationId} AND [[group]].[[id]]={:groupId}`
	if err := app.DB().NewQuery(countQuery).Bind(dbx.Params{
		"organizationId": organizationID, "groupId": group.ID,
	}).Row(&memberCount); err != nil {
		return organizationSettingsGroup{}, "", false, err
	}
	response := organizationSettingsGroup{
		ID: group.ID, Name: group.Name, ManagedBy: group.ManagedBy, Members: members,
		MemberCount: memberCount, MembersHasMore: hasMore,
		CreatedAt: organizationSettingsTime(group.CreatedAt), UpdatedAt: organizationSettingsTime(group.UpdatedAt),
	}
	if hasMore && len(rows) > 0 {
		return response, rows[len(rows)-1].ID, false, nil
	}
	return response, "", true, nil
}

// A group this organization doesn't have, named by an agent or a stale screen,
// is a request to check, as a missing member or domain is; it answered as a
// server fault before.
var errOrganizationGroupMissing = errors.Join(
	organizationSettingsRefused("targetId names no group of this organization; its settings list the groups"), sql.ErrNoRows)

// organizationSettingsRefused refuses an organization settings change for
// the reason given (request_refusal.go).
func organizationSettingsRefused(format string, args ...any) error {
	return refuse(errOrganizationSettingsInvalid, format, args...)
}

// errOrganizationGroupName is a group's name the change can't give it.
var errOrganizationGroupName = organizationSettingsRefused("value is the group's name, 1–%d characters", organizationSettingsGroupNameRunes)

func mutateManualOrganizationGroup(
	app core.App, organizationID, actorID string, input organizationSettingsMutationInput, now int64,
) (*organizationSettingsGroup, string, error) {
	groupID := strings.TrimSpace(input.TargetID)
	name := strings.TrimSpace(input.Value)
	switch input.Action {
	case organizationSettingsActionGroupCreate:
		if name == "" || len([]rune(name)) > organizationSettingsGroupNameRunes {
			return nil, "", errOrganizationGroupName
		}
		groupID = core.GenerateDefaultRandomId()
		_, err := app.DB().Insert(organizationGroupsTable, dbx.Params{
			"id": groupID, "organization_id": organizationID, "name": name,
			"normalized_name": strings.ToLower(name), "created_by": actorID,
			"created_at": now, "updated_at": now,
		}).Execute()
		if err != nil {
			return nil, "", errors.Join(errors.New(errorCodeOrganizationSettingsGroupConflict), err)
		}
	case organizationSettingsActionGroupUpdate:
		if groupID == "" {
			return nil, "", organizationSettingsRefused("targetId is empty: it names the group to rename")
		}
		if name == "" || len([]rune(name)) > organizationSettingsGroupNameRunes {
			return nil, "", errOrganizationGroupName
		}
		result, err := app.DB().NewQuery(`UPDATE {{leafen_organization_groups}}
			SET [[name]]={:name}, [[normalized_name]]={:normalizedName}, [[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[organization_id]]={:organizationId}`).Bind(dbx.Params{
			"name": name, "normalizedName": strings.ToLower(name), "now": now,
			"id": groupID, "organizationId": organizationID,
		}).Execute()
		if err != nil {
			return nil, "", errors.Join(errors.New(errorCodeOrganizationSettingsGroupConflict), err)
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return nil, "", errOrganizationGroupMissing
		}
	case organizationSettingsActionGroupDelete:
		result, err := app.DB().Delete(organizationGroupsTable,
			dbx.HashExp{"id": groupID, "organization_id": organizationID}).Execute()
		if err != nil {
			return nil, "", err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return nil, "", errOrganizationGroupMissing
		}
		return nil, groupID, nil
	case organizationSettingsActionGroupMemberAdd, organizationSettingsActionGroupMemberRemove:
		memberID := strings.TrimSpace(input.SecondaryTargetID)
		if groupID == "" || memberID == "" {
			return nil, "", organizationSettingsRefused("targetId names the group and secondaryTargetId the organization member")
		}
		var exists int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_groups}} [[group]]
			JOIN {{leafen_organization_memberships}} [[membership]]
				ON [[membership]].[[id]]={:memberId} AND [[membership]].[[organization]]=[[group]].[[organization_id]]
			WHERE [[group]].[[id]]={:groupId} AND [[group]].[[organization_id]]={:organizationId}`).Bind(dbx.Params{
			"memberId": memberID, "groupId": groupID, "organizationId": organizationID,
		}).Row(&exists); err != nil || exists != 1 {
			return nil, "", organizationSettingsRefused("targetId names no group of this organization, or secondaryTargetId no member of it")
		}
		if input.Action == organizationSettingsActionGroupMemberAdd {
			_, err := app.DB().NewQuery(`INSERT INTO {{leafen_organization_group_members}}
				([[group_id]], [[organization_member_id]], [[created_at]]) VALUES ({:groupId}, {:memberId}, {:now})
				ON CONFLICT ([[group_id]], [[organization_member_id]]) DO NOTHING`).Bind(dbx.Params{
				"groupId": groupID, "memberId": memberID, "now": now,
			}).Execute()
			if err != nil {
				return nil, "", err
			}
		} else if _, err := app.DB().Delete(organizationGroupMembersTable,
			dbx.HashExp{"group_id": groupID, "organization_member_id": memberID}).Execute(); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", organizationSettingsRefused("action %q isn't a group change", input.Action)
	}
	row := storedOrganizationSettingsGroup{}
	if readErr := app.DB().NewQuery(`SELECT [[id]], [[name]], 'manual' AS [[managed_by]], [[created_at]], [[updated_at]]
		FROM {{leafen_organization_groups}} WHERE [[organization_id]]={:organizationId} AND [[id]]={:id}`).Bind(dbx.Params{
		"organizationId": organizationID, "id": groupID,
	}).One(&row); readErr != nil {
		return nil, "", readErr
	}
	group, _, _, err := organizationSettingsGroupPage(app, organizationID, row, "")
	if err != nil {
		return nil, "", err
	}
	return &group, "", nil
}

func organizationSettingsTime(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}
