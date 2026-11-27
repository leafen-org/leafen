package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's member reads a file where it is held, as a guest does
// (2026-10-24: a member opened by its ID the files of a room they weren't
// in; Notion opens a file by its block's page): a page's file — a block's,
// the page's icon or cover, a row's, a comment's, one its history, trash or
// templates keep — to who may read that page; the workspace's own — its
// emojis, its public sites' pictures and its icon — to every member, a
// group's icon too and a teamspace's to who may see the teamspace (to every
// member while its invite link is on), and an app's to
// the person it kept it for; and a file held nowhere to whoever sent it, as
// Notion keeps an upload nothing shows yet to its sender. Only reading
// changes: who sends and deletes files stays as it was.

// memberProductFileOpen says whether a member of the file's workspace may
// read it where it is held.
func memberProductFileOpen(app core.App, principalID, workspaceID, fileID, createdBy string) (bool, error) {
	held := struct {
		Workspace  bool `db:"workspace_wide"`
		AppFor     bool `db:"app_for"`
		Referenced bool `db:"referenced"`
	}{}
	if err := app.DB().NewQuery(`SELECT
		EXISTS (SELECT 1 FROM {{leafen_custom_emojis}} WHERE [[file_id]] = {:file} AND [[workspace_id]] = {:workspace})
			OR EXISTS (SELECT 1 FROM {{leafen_file_references}} WHERE [[file_id]] = {:file} AND [[workspace_id]] = {:workspace}
				AND [[owner_kind]] IN ({:sitePreview}, {:siteFavicon})) AS [[workspace_wide]],
		EXISTS (SELECT 1 FROM {{leafen_app_endpoint_files}} WHERE [[file_id]] = {:file} AND [[user_id]] = {:principal}) AS [[app_for]],
		EXISTS (SELECT 1 FROM {{leafen_file_references}} WHERE [[file_id]] = {:file} AND [[workspace_id]] = {:workspace}) AS [[referenced]]`).
		Bind(dbx.Params{"file": fileID, "workspace": workspaceID, "principal": principalID,
			"sitePreview": productFileReferenceOwnerSitePreviewImage, "siteFavicon": productFileReferenceOwnerSiteFavicon}).One(&held); err != nil {
		return false, err
	}
	if held.Workspace || held.AppFor {
		return true, nil
	}
	// The workspace's, a teamspace's or a group's icon (held_icon_files.go).
	iconOpen, iconHeld, err := heldIconFileOpen(app, principalID, workspaceID, fileID)
	if err != nil || iconOpen {
		return iconOpen, err
	}
	if held.Referenced {
		if _, err := authorizeReferencedProductFileReadAt(app, principalID, workspaceID, fileID, true); err == nil {
			return true, nil
		} else if !isPermissionDenied(err) {
			return false, err
		}
	}
	var heldElsewhere bool
	if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_app_endpoint_files}} WHERE [[file_id]] = {:file})`).
		Bind(dbx.Params{"file": fileID}).Row(&heldElsewhere); err != nil {
		return false, err
	}
	if held.Referenced || heldElsewhere || iconHeld {
		return false, nil
	}
	return createdBy != "" && createdBy == principalID, nil
}
