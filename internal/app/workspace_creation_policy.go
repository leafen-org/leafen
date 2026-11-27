package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// An organization that verified a person's email domain may keep new
// workspaces to its owners and admins. The organization module decides that
// (WorkspaceCreation); without it anyone may create workspaces.

const errorCodeWorkspaceCreationRestricted = "LEAFEN_WORKSPACE_CREATION_RESTRICTED"

var errWorkspaceCreationRestricted = errors.New(errorCodeWorkspaceCreationRestricted)

// authorizeDomainWorkspaceCreation fails with errWorkspaceCreationRestricted
// when an organization keeps the person from creating workspaces.
func authorizeDomainWorkspaceCreation(app core.App, user *core.Record) error {
	if check := organizationPolicy.WorkspaceCreation; check != nil {
		return check(app, user)
	}
	return nil
}
