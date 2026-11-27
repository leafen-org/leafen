package organization

import (
	"github.com/pocketbase/pocketbase/core"
	leafen "leafen.org/leafen/internal/app"
)

// The organization's blocks on sharing outside it, as Notion's workspace
// security settings: publishing to the web, sharing with guests and outside
// emails, exports and copies to other workspaces. Who may invite guests or
// add members, access requests, view links and HTML are the core's.

// enforceSharingSecurityPublicSharing refuses publishing to the web — share
// links, sites and web forms — while the organization has it turned off, as
// Notion's setting to disable publishing sites and forms does.
func blockPublicSharing(app core.App, organizationID string) error {
	policy, err := leafen.LoadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil {
		return err
	}
	if policy.PublicWebSharing == 0 {
		return leafen.ErrOrganizationSharingSecurityBlocked
	}
	return nil
}

// enforceSharingSecurityExports refuses a member's export — of a page, a
// database or the workspace, in any format, and taking one already made —
// while the organization has exports turned off, as Notion's setting to
// disable export does.
func blockExports(app core.App, organizationID string) error {
	policy, err := leafen.LoadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil {
		return err
	}
	if policy.Exports == 0 {
		return leafen.ErrOrganizationSharingSecurityBlocked
	}
	return nil
}

// enforceSharingSecurityCrossWorkspaceCopy refuses moving or copying a
// page out of its workspace into another while the organization of the
// workspace it leaves has that turned off, as Notion's setting to disable
// duplicating pages to other workspaces does.
func blockCrossWorkspaceCopy(app core.App, source, target leafen.WorkspaceAuthorization) error {
	if source.WorkspaceID == target.WorkspaceID {
		return nil
	}
	policy, err := leafen.LoadEnforcedSharingSecurityPolicy(app, source.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if policy.CrossWorkspaceCopies == 0 {
		return leafen.ErrOrganizationSharingSecurityBlocked
	}
	return nil
}

// enforceSharingSecurityShare refuses sharing a page of the workspace with
// someone the organization's settings keep out: anyone who isn't a member
// of the workspace while guests are turned off, and an email outside the
// organization while external email sharing is turned off. Shares with the
// workspace's groups and integrations stay inside it.
func blockShare(
	app core.App, organizationID, workspaceID, principalType, principalID string,
) error {
	policy, err := leafen.LoadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil {
		return err
	}
	if policy.GuestAccess == 1 && policy.ExternalEmailSharing == 1 {
		return nil
	}
	userID := ""
	switch principalType {
	case leafen.PublicMutationPrincipalUser:
		userID = principalID
	case leafen.PublicMutationPrincipalEmail:
		userID, err = leafen.SharingSecurityEmailMember(app, organizationID, principalID)
		if err != nil {
			return err
		}
		if userID == "" {
			// Someone outside the organization comes in as a guest.
			if policy.ExternalEmailSharing == 0 || policy.GuestAccess == 0 {
				return leafen.ErrOrganizationSharingSecurityBlocked
			}
			return nil
		}
	default:
		return nil
	}
	if policy.GuestAccess == 1 {
		return nil
	}
	_, err = leafen.AuthorizeWorkspace(app, userID, workspaceID, leafen.PermissionWorkspaceRead)
	if leafen.IsPermissionDenied(err) {
		return leafen.ErrOrganizationSharingSecurityBlocked
	}
	return err
}

// sharingBlocks answers the core's sharing checks.
type sharingBlocks struct{}

func (sharingBlocks) PublicSharing(app core.App, organizationID string) error {
	return blockPublicSharing(app, organizationID)
}

func (sharingBlocks) Exports(app core.App, organizationID string) error {
	return blockExports(app, organizationID)
}

func (sharingBlocks) CrossWorkspaceCopy(app core.App, source, target leafen.WorkspaceAuthorization) error {
	return blockCrossWorkspaceCopy(app, source, target)
}

func (sharingBlocks) Share(app core.App, organizationID, workspaceID, principalType, principalID string) error {
	return blockShare(app, organizationID, workspaceID, principalType, principalID)
}

// Options are whether the organization lets its pages be published and
// shared with guests and outside emails.
func (sharingBlocks) Options(app core.App, organizationID string) (bool, bool, bool, error) {
	policy, err := leafen.LoadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil {
		return false, false, false, err
	}
	return policy.PublicWebSharing == 1, policy.GuestAccess == 1, policy.ExternalEmailSharing == 1, nil
}
