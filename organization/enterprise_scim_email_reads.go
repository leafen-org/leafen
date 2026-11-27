package organization

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Read a finite selected page's claims once, in its membership snapshot.
// Domain ownership is evaluated here for both point and list resources.
func loadSCIMUserEmails(app core.App, organizationID string, users []storedSCIMListUser) (map[string][]string, error) {
	addresses := map[string][]string{}
	if len(users) == 0 {
		return addresses, nil
	}
	params := dbx.Params{"organizationId": organizationID, "maxRows": len(users) * (leafen.AccountSecondaryEmailLimit + 2)}
	placeholders := make([]string, 0, len(users))
	for index, user := range users {
		key := fmt.Sprintf("user%d", index)
		params[key] = user.UserID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []struct {
		UserID  string `db:"user_id"`
		Email   string `db:"email"`
		Managed bool   `db:"managed"`
	}{}
	err := app.DB().NewQuery(`SELECT [[claim]].[[user_id]], [[claim]].[[email]], EXISTS (
		SELECT 1 FROM {{leafen_organization_domains}} [[domain]]
		WHERE [[domain]].[[organization_id]] = {:organizationId} AND [[domain]].[[status]] = 'verified'
		AND [[domain]].[[verified_at]] > 0
		AND [[domain]].[[domain]] = substr([[claim]].[[email]], instr([[claim]].[[email]], '@') + 1))
		AS [[managed]] FROM {{leafen_account_emails}} [[claim]]
		WHERE [[claim]].[[user_id]] IN (` + strings.Join(placeholders, ",") + `) AND [[claim]].[[verified]] = 1
		ORDER BY [[claim]].[[user_id]], [[claim]].[[email]] LIMIT {:maxRows}`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.UserID]++
		if counts[row.UserID] > leafen.AccountSecondaryEmailLimit+1 {
			return nil, leafen.ErrAccountEmailLimit
		}
		if row.Managed {
			addresses[row.UserID] = append(addresses[row.UserID], row.Email)
		}
	}
	return addresses, nil
}

func loadSCIMUserResource(app core.App, organizationID, membershipID string) (scim.Resource, error) {
	var resource scim.Resource
	err := app.RunInTransaction(func(txApp core.App) error {
		var row storedSCIMListUser
		err := txApp.DB().NewQuery(`SELECT [[membership]].[[id]], [[membership]].[[user]] AS [[user_id]],
			[[membership]].[[status]], [[membership]].[[scim_external_id]] AS [[external_id]],
			[[membership]].[[created]], [[membership]].[[updated]],
			[[user]].[[email]], [[user]].[[name]] AS [[display_name]]
			FROM {{leafen_organization_memberships}} [[membership]] JOIN {{users}} [[user]]
			ON [[user]].[[id]] = [[membership]].[[user]]
			WHERE [[membership]].[[id]] = {:membershipId} AND [[membership]].[[organization]] = {:organizationId}
			AND [[membership]].[[scim_managed]] = 1`).
			Bind(dbx.Params{"membershipId": membershipID, "organizationId": organizationID}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return scimErrors.ScimErrorResourceNotFound(membershipID)
		}
		if err != nil {
			return err
		}
		addresses, err := loadSCIMUserEmails(txApp, organizationID, []storedSCIMListUser{row})
		if err != nil {
			return err
		}
		resource, err = scimUserResource(row, addresses[row.UserID])
		return err
	})
	return resource, err
}
