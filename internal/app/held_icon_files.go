package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's, a teamspace's or a group's icon may be a picture uploaded
// from its picker's "업로드" tab, as Notion's (seen 2026-11-11). The icon is
// then the file's address in its workspace (productFilePresentationPrefix and
// its ID), the file one of the workspace's own pictures. The file stays while
// the workspace, a teamspace not deleted (archived ones too, which come back)
// or a group not deleted shows it, and goes the usual way
// (reconcileProductFileReferenceLifecycle) once none does: the icon changed,
// or its holder was deleted. A workspace's or a group's is read by every
// member of the workspace, who sees its groups; a teamspace's by who may see
// the teamspace, and by every member while its invite link, which shows it,
// is on. A workspace's is read outside it too, by anyone signed in
// (authorizeWorkspaceIconFileRead).

// heldIconImageTypes are the pictures an icon may be, as a published site's
// images: raster ones only.
var heldIconImageTypes = publicSiteAssetContentTypes

// heldIconFileID is the managed file a kept icon shows, if it shows one.
func heldIconFileID(icon string) (string, bool) {
	icon = strings.TrimSpace(icon)
	if !strings.HasPrefix(icon, productFilePresentationPrefix) {
		return "", false
	}
	return productFilePresentationID(icon)
}

// withoutHeldIconFile is an icon brought from elsewhere, an archive's, whose
// uploaded picture didn't come with it: none then, so its letter shows.
func withoutHeldIconFile(icon string) string {
	if strings.HasPrefix(strings.TrimSpace(icon), productFilePresentationPrefix) {
		return ""
	}
	return icon
}

// checkHeldIconFile refuses an icon showing a file that isn't one of the
// workspace's whole pictures; any other icon passes.
func checkHeldIconFile(app core.App, workspaceID, icon string) bool {
	fileID, managed := heldIconFileID(icon)
	if !managed {
		return !strings.HasPrefix(strings.TrimSpace(icon), productFilePresentationPrefix)
	}
	files, err := loadGraphFileAuthorityRecords(app, workspaceID, []string{fileID})
	if err != nil {
		return false
	}
	_, picture := heldIconImageTypes[strings.ToLower(files[fileID].ContentType)]
	return picture
}

// settleHeldIconFiles keeps or lets go the files that icons showed or show
// now, after a teamspace's or group's icon changed or its holder went.
func settleHeldIconFiles(app core.App, workspaceID string, icons ...string) error {
	fileIDs := []string{}
	for _, icon := range icons {
		if fileID, managed := heldIconFileID(icon); managed {
			fileIDs = append(fileIDs, fileID)
		}
	}
	if len(fileIDs) == 0 {
		return nil
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}

// heldIconFileReferenceSQL says, for findProductFileReference, that the
// file's workspace, or a teamspace or a group of it, shows it
// ({:fileAddress}).
const heldIconFileReferenceSQL = `EXISTS(
			SELECT 1 FROM {{leafen_files}} AS [[held_workspace_file]]
			JOIN {{leafen_workspaces}} AS [[held_workspace]] ON [[held_workspace]].[[id]]=[[held_workspace_file]].[[workspace]]
			WHERE [[held_workspace_file]].[[id]]={:fileId} AND [[held_workspace]].[[icon]]={:fileAddress} LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_teamspaces}} AS [[held_teamspace]]
			JOIN {{leafen_files}} AS [[held_teamspace_file]] ON [[held_teamspace_file]].[[id]]={:fileId}
				AND [[held_teamspace_file]].[[workspace]]=[[held_teamspace]].[[workspace_id]]
			WHERE [[held_teamspace]].[[deleted_at]]=0 AND [[held_teamspace]].[[icon]]={:fileAddress} LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_workspace_permission_groups}} AS [[held_group]]
			JOIN {{leafen_files}} AS [[held_group_file]] ON [[held_group_file]].[[id]]={:fileId}
				AND [[held_group_file]].[[workspace]]=[[held_group]].[[workspace_id]]
			WHERE [[held_group]].[[deleted_at]] IS NULL AND [[held_group]].[[icon]]={:fileAddress} LIMIT 1
		)`

// heldIconFileOpen says whether a member of the file's workspace reads it as
// the icon of the workspace, of a group there, or of a teamspace there they
// may see or whose invite link is on; held says whether any of them shows it.
func heldIconFileOpen(app core.App, principalID, workspaceID, fileID string) (open, held bool, err error) {
	address := productFilePresentationPrefix + fileID
	var groups int
	if err := app.DB().NewQuery(`
		SELECT (SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[workspace_id]]={:workspace} AND [[deleted_at]] IS NULL AND [[icon]]={:address})
		+ (SELECT COUNT(*) FROM {{leafen_workspaces}} WHERE [[id]]={:workspace} AND [[icon]]={:address})
	`).Bind(dbx.Params{"workspace": workspaceID, "address": address}).Row(&groups); err != nil {
		return false, false, err
	}
	if groups > 0 {
		return true, true, nil
	}
	// A teamspace's invite link shows its icon to every member of the
	// workspace who opens it (teamspace_invite_links.go teamspaceInvite),
	// one they may not see yet too: its icon is open to them while the link
	// is on and the teamspace in use. Every teamspace showing it counts, a
	// window at a time, with one decision per teamspace as its own reading
	// takes (decideTeamspaceAccess): the one a person sees may come after any
	// number they don't, as copies of one teamspace keep its icon.
	var authority *teamspaceAuthority
	var settings storedTeamspaceSettings
	after := ""
	for {
		rows := []struct {
			ID         string `db:"id"`
			Access     string `db:"access"`
			ArchivedAt int64  `db:"archived_at"`
			Invited    bool   `db:"invited"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[teamspace]].[[id]],[[teamspace]].[[access]],[[teamspace]].[[archived_at]],
				[[teamspace]].[[archived_at]]=0 AND EXISTS(SELECT 1 FROM {{leafen_teamspace_invite_links}} AS [[link]]
					WHERE [[link]].[[teamspace_id]]=[[teamspace]].[[id]] AND [[link]].[[enabled]]=1) AS [[invited]]
			FROM {{leafen_teamspaces}} AS [[teamspace]]
			WHERE [[teamspace]].[[workspace_id]]={:workspace} AND [[teamspace]].[[deleted_at]]=0
				AND [[teamspace]].[[icon]]={:address} AND [[teamspace]].[[id]]>{:after}
			ORDER BY [[teamspace]].[[id]] LIMIT {:window}
		`).Bind(dbx.Params{
			"workspace": workspaceID, "address": address, "after": after, "window": teamspaceCollectionWindow,
		}).All(&rows); err != nil {
			return false, held, err
		}
		if len(rows) == 0 {
			return false, held, nil
		}
		held = true
		ids := make([]string, len(rows))
		for index, row := range rows {
			if row.Invited {
				return true, true, nil
			}
			ids[index] = row.ID
		}
		if authority == nil {
			found, err := loadTeamspaceAuthority(app, principalID, workspaceID)
			if err != nil {
				// Not the workspace's member: no teamspace of it is theirs to see.
				return false, true, nil
			}
			if found.Workspace.Role == workspaceRoleOwner {
				return true, true, nil
			}
			if settings, err = loadTeamspaceSettings(app, found); err != nil {
				return false, true, err
			}
			authority = &found
		}
		memberships, err := loadActorTeamspaceMembers(app, *authority, ids)
		if err != nil {
			return false, true, err
		}
		for _, row := range rows {
			teamspace := storedTeamspace{ID: row.ID, Access: row.Access, ArchivedAt: row.ArchivedAt}
			if decideTeamspaceAccess(teamspace, settings, memberships, nil, *authority).Visible {
				return true, true, nil
			}
		}
		if len(rows) < teamspaceCollectionWindow {
			return false, true, nil
		}
		after = rows[len(rows)-1].ID
	}
}

// authorizeWorkspaceIconFileRead lets anyone signed in on the server read the
// picture a workspace shows as its icon, as Notion shows a workspace's icon
// outside it: to whom its invite links reach, to its guests beside the pages
// shared with them, on a teamspace's invite. Only that file, and only while
// the workspace shows it.
func authorizeWorkspaceIconFileRead(app core.App, principalID, fileID string) (*core.Record, workspaceAuthorization, error) {
	denied := func(err error) (*core.Record, workspaceAuthorization, error) {
		if err == nil || errors.Is(err, sql.ErrNoRows) {
			err = permissionDenied()
		}
		return nil, workspaceAuthorization{}, err
	}
	if principalID == "" {
		return denied(nil)
	}
	record, err := app.FindRecordById(filesCollection, fileID)
	if err != nil {
		return denied(err)
	}
	workspaceID := record.GetString("workspace")
	if !workspaceFileRecordBelongsTo(record, workspaceID) {
		return denied(nil)
	}
	if pending, err := productFileDeletionPending(app, fileID); err != nil || pending {
		return denied(err)
	}
	if incomplete, err := productFileUploadIncomplete(app, fileID); err != nil || incomplete {
		return denied(err)
	}
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if err != nil {
		return denied(err)
	}
	if workspace.GetString("deletion_job_id") != "" || workspace.GetString("icon") != productFilePresentationPrefix+fileID {
		return denied(nil)
	}
	return record, workspaceAuthorization{
		Workspace: workspace, PrincipalID: principalID, WorkspaceID: workspaceID,
		OwnerID: workspace.GetString("owner"), WorkspaceOwnerID: workspacePermissionOwnerID(workspace),
		Organization: organizationAuthorization{
			OrganizationID: workspace.GetString("organization"), PrincipalID: principalID,
		},
	}, nil
}
