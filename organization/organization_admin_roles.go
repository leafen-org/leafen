package organization

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	leafen "leafen.org/leafen/internal/app"
)

// adminRoleStore keeps the admin roles an organization's owner gives
// members, each over one area of its settings.
type adminRoleStore struct{}

// Assigned reads the person's admin roles in the organizations, by
// organization.
func (adminRoleStore) Assigned(app core.App, principalID string, organizationIDs []string) (map[string][]string, error) {
	roles := map[string][]string{}
	if len(organizationIDs) == 0 {
		return roles, nil
	}
	params := dbx.Params{"principalId": principalID}
	placeholders := make([]string, 0, len(organizationIDs))
	for index, organizationID := range organizationIDs {
		key := fmt.Sprintf("organization%d", index)
		params[key] = organizationID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []struct {
		OrganizationID string `db:"organization_id"`
		Role           string `db:"role"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[organization_id]], [[role]] FROM {{leafen_organization_admin_roles}}
		WHERE [[user_id]] = {:principalId} AND [[organization_id]] IN (` + leafen.JoinSQL(placeholders) + `)
		ORDER BY [[organization_id]], [[role]]
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		roles[row.OrganizationID] = append(roles[row.OrganizationID], row.Role)
	}
	return roles, nil
}

// Members reads the admin roles of an organization's members, by user.
func (adminRoleStore) Members(app core.App, organizationID string) (map[string][]string, error) {
	rows := []struct {
		UserID string `db:"user_id"`
		Role   string `db:"role"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]], [[role]] FROM {{leafen_organization_admin_roles}}
		WHERE [[organization_id]] = {:organizationId}
		ORDER BY [[user_id]], [[role]]
	`).Bind(dbx.Params{"organizationId": organizationID}).All(&rows); err != nil {
		return nil, err
	}
	roles := map[string][]string{}
	for _, row := range rows {
		roles[row.UserID] = append(roles[row.UserID], row.Role)
	}
	return roles, nil
}

func (adminRoleStore) Add(app core.App, organizationID, userID, role, assignedBy string) error {
	_, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_organization_admin_roles}}
			([[organization_id]], [[user_id]], [[role]], [[assigned_by]], [[created_at]])
		VALUES ({:organizationId}, {:userId}, {:role}, {:assignedBy}, {:now})
	`).Bind(dbx.Params{
		"organizationId": organizationID, "userId": userID, "role": role,
		"assignedBy": assignedBy, "now": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func (adminRoleStore) Remove(app core.App, organizationID, userID, role string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_organization_admin_roles}}
		WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId} AND [[role]] = {:role}
	`).Bind(dbx.Params{"organizationId": organizationID, "userId": userID, "role": role}).Execute()
	return err
}
