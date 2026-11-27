package app

import (
	"context"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The Admin API names an organization's active member by email or ID — the
// person a managed-user action or a workspace export acts on.

var (
	errNotionAdminManagedUserSessionNotFound    = errors.New("Notion Admin managed user was not found")
	errNotionAdminManagedUserSessionUnavailable = errors.New("Notion Admin managed-user session authority is unavailable")
)

type normalizedNotionAdminManagedUserSelector struct {
	Email          string
	InternalUserID string
}

func resolveNotionAdminManagedUser(
	ctx context.Context,
	app core.App,
	organizationID string,
	selector normalizedNotionAdminManagedUserSelector,
) (string, error) {
	rows := []struct {
		ID string `db:"id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]]
		FROM {{users}} [[user]]
		JOIN {{leafen_organization_memberships}} [[membership]]
			ON [[membership]].[[user]] = [[user]].[[id]]
		WHERE [[membership]].[[organization]] = {:organizationId}
			AND [[membership]].[[status]] = {:activeStatus}
			AND [[membership]].[[role]] != {:guestRole}
			AND (({:userId} != '' AND [[user]].[[id]] = {:userId})
				OR ({:email} != '' AND [[user]].[[email]] = {:email}))
		ORDER BY [[user]].[[id]] LIMIT 2
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID,
		"activeStatus":   organizationMemberStatusActive,
		"guestRole":      organizationRoleGuest,
		"userId":         selector.InternalUserID,
		"email":          selector.Email,
	}).All(&rows)
	if err != nil {
		return "", errors.Join(errNotionAdminManagedUserSessionUnavailable, err)
	}
	if len(rows) == 0 {
		return "", errNotionAdminManagedUserSessionNotFound
	}
	if len(rows) != 1 || rows[0].ID == "" {
		return "", errNotionAdminManagedUserSessionUnavailable
	}
	return rows[0].ID, nil
}
