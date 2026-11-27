package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A teamspace's page security, as Notion's Security tab: publishing its
// pages to the web (share links, sites and web forms), sharing them with
// people outside the workspace, and exporting them may each be turned off.
// Notion offers these on its Enterprise plan; Leafen to everyone. They add
// to the organization's sharing security settings and never loosen them.

type storedTeamspacePageSecurity struct {
	DisablePublishing bool `db:"disable_publishing"`
	DisableGuests     bool `db:"disable_guests"`
	DisableExport     bool `db:"disable_export"`
}

// teamspacePageSecurity is the page security of the teamspace a page
// belongs to; a page outside any teamspace has none turned off.
func teamspacePageSecurity(app core.App, pageID string) (storedTeamspacePageSecurity, error) {
	security := storedTeamspacePageSecurity{}
	err := app.DB().NewQuery(`
		SELECT [[teamspace]].[[disable_publishing]],[[teamspace]].[[disable_guests]],[[teamspace]].[[disable_export]]
		FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_teamspaces}} [[teamspace]] ON [[teamspace]].[[id]]=[[page]].[[teamspace_id]]
			AND [[teamspace]].[[workspace_id]]=[[page]].[[workspace]] AND [[teamspace]].[[deleted_at]]=0
		WHERE [[page]].[[id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).One(&security)
	if errors.Is(err, sql.ErrNoRows) {
		return storedTeamspacePageSecurity{}, nil
	}
	return security, err
}

// enforceTeamspacePublishing refuses publishing a page of a teamspace that
// has publishing turned off.
func enforceTeamspacePublishing(app core.App, pageID string) error {
	security, err := teamspacePageSecurity(app, pageID)
	if err != nil {
		return err
	}
	if security.DisablePublishing {
		return errOrganizationSharingSecurityBlocked
	}
	return nil
}

// enforceTeamspaceExport refuses exporting a page of a teamspace that has
// export turned off.
func enforceTeamspaceExport(app core.App, pageID string) error {
	security, err := teamspacePageSecurity(app, pageID)
	if err != nil {
		return err
	}
	if security.DisableExport {
		return errOrganizationSharingSecurityBlocked
	}
	return nil
}

// enforceTeamspaceShare refuses sharing a page of a teamspace that has guests
// turned off with anyone who isn't a member of its workspace: a person
// outside it, or an email no one in the organization holds.
func enforceTeamspaceShare(
	app core.App, pageID, organizationID, workspaceID, principalType, principalID string,
) error {
	security, err := teamspacePageSecurity(app, pageID)
	if err != nil || !security.DisableGuests {
		return err
	}
	userID := ""
	switch principalType {
	case publicMutationPrincipalUser:
		userID = principalID
	case publicMutationPrincipalEmail:
		userID, err = sharingSecurityEmailMember(app, organizationID, principalID)
		if err != nil {
			return err
		}
		if userID == "" {
			return errOrganizationSharingSecurityBlocked
		}
	default:
		return nil
	}
	_, err = authorizeWorkspace(app, userID, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return errOrganizationSharingSecurityBlocked
	}
	return err
}
