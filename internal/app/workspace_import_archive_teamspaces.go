package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An archive carries the Teamspaces its pages belong to. Import makes each
// one again through the ordinary Teamspace mutation, owned by the importer,
// before the pages that live in it, and a page keeps its Teamspace. Members,
// join requests and the source's page permissions belong to the source
// workspace's people and are not carried.

// An archived teamspace. An archive made before teamspaces said whether the
// rest of the workspace has OpenPageRole gave it only in open ones; one made
// before page security has none of it turned off.
type archiveTeamspaceValue struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Icon                  string `json:"icon"`
	Description           string `json:"description"`
	Access                string `json:"access"`
	MemberPageRole        string `json:"memberPageRole"`
	OpenPageRole          string `json:"openPageRole"`
	OutsideAccess         *bool  `json:"outsideAccess,omitempty"`
	MembersCanInvite      bool   `json:"membersCanInvite"`
	MembersCanEditSidebar bool   `json:"membersCanEditSidebar"`
	DisablePublishing     *bool  `json:"disablePublishing,omitempty"`
	DisableGuests         *bool  `json:"disableGuests,omitempty"`
	DisableExport         *bool  `json:"disableExport,omitempty"`
}

type workspaceImportStagedTeamspace = archiveTeamspaceValue

// workspaceImportArchivePageTeamspace returns the archived Teamspace a root
// page belongs to, or nil when the archive does not carry it.
func workspaceImportArchivePageTeamspace(db *sql.DB, sourceID string) (*workspaceImportStagedTeamspace, error) {
	if sourceID == "" {
		return nil, nil
	}
	var encoded string
	err := db.QueryRow(`SELECT value_json FROM entities WHERE kind = ? AND source_id = ?`,
		workspaceExportItemTeamspace, sourceID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, workspaceImportStorageError(err)
	}
	value := archiveTeamspaceValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil || value.ID != sourceID ||
		strings.TrimSpace(value.Name) == "" || utf8.RuneCountInString(value.Name) > maxTeamspaceNameRunes ||
		utf8.RuneCountInString(value.Description) > maxTeamspaceDescriptionRunes ||
		!validTeamspaceAccess(value.Access) || !validTeamspacePageRole(value.MemberPageRole) ||
		!validTeamspaceOutsideRole(value.OpenPageRole) {
		return nil, workspaceImportInputError()
	}
	return &value, nil
}

// ensureWorkspaceArchiveTeamspaces makes the Teamspaces a page chunk's root
// pages live in and returns their imported IDs by archived ID. A Teamspace
// that already lives in the workspace is used as it is; one the importer may
// not create leaves its pages at the workspace level.
func (r *durableJobRunner) ensureWorkspaceArchiveTeamspaces(
	job storedJob, chunk workspaceImportStagedChunk,
) (map[string]string, error) {
	result := map[string]string{}
	for _, item := range chunk.Items {
		if item.Page == nil || item.Page.Teamspace == nil {
			continue
		}
		teamspace := item.Page.Teamspace
		if _, done := result[teamspace.ID]; done {
			continue
		}
		var live int
		if err := r.app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_teamspaces}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0
		`).Bind(dbx.Params{"id": teamspace.ID, "workspaceId": job.WorkspaceID}).Row(&live); err != nil {
			return nil, err
		}
		if live == 1 {
			result[teamspace.ID] = teamspace.ID
			continue
		}
		canInvite, canEditSidebar := teamspace.MembersCanInvite, teamspace.MembersCanEditSidebar
		outsideRole := teamspace.OpenPageRole
		if outside := teamspace.Access == teamspaceAccessOpen; teamspace.OutsideAccess != nil && !*teamspace.OutsideAccess ||
			teamspace.OutsideAccess == nil && !outside {
			outsideRole = teamspacePageRoleNone
		}
		response, _, err := executeTeamspaceMutation(context.Background(), r.app, r.rooms, job.Owner, job.WorkspaceID,
			teamspaceMutationInput{
				RequestID: "workspace-import:" + job.ID + ":" + teamspace.ID, Action: teamspaceActionCreate,
				Name: teamspace.Name, Icon: withoutHeldIconFile(teamspace.Icon), Description: teamspace.Description,
				Access: teamspace.Access, MemberPageRole: teamspace.MemberPageRole, OpenPageRole: outsideRole,
				MembersCanInvite: &canInvite, MembersCanEditSidebar: &canEditSidebar,
				DisablePublishing: teamspace.DisablePublishing, DisableGuests: teamspace.DisableGuests,
				DisableExport: teamspace.DisableExport,
			})
		if errors.Is(err, errTeamspaceNotFound) {
			result[teamspace.ID] = ""
			continue
		}
		if err != nil {
			return nil, err
		}
		result[teamspace.ID] = response.TeamspaceID
	}
	return result, nil
}

// workspaceImportArchivePageTeamspaceID resolves where an imported root page
// lives: its re-made Teamspace, or a Teamspace of that ID in the workspace.
func workspaceImportArchivePageTeamspaceID(
	app core.App, workspaceID string, definition *workspaceImportStagedPage, teamspaces map[string]string,
) (string, error) {
	if mapped, found := teamspaces[definition.TeamspaceID]; found {
		return mapped, nil
	}
	var available int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_teamspaces}}
		WHERE [[id]]={:teamspaceId} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0
	`).Bind(dbx.Params{"teamspaceId": definition.TeamspaceID, "workspaceId": workspaceID}).Row(&available); err != nil {
		return "", err
	}
	if available == 1 {
		return definition.TeamspaceID, nil
	}
	return "", nil
}

// deleteWorkspaceArchiveImportTeamspaces removes the Teamspaces a cancelled
// or failed import made, found through the import's mutation receipts.
func deleteWorkspaceArchiveImportTeamspaces(app core.App, jobID, workspaceID string) error {
	created := `
		SELECT json_extract([[response_json]], '$.teamspaceId') FROM {{leafen_teamspace_mutation_receipts}}
		WHERE [[workspace_id]]={:workspaceId} AND substr([[request_id]], 1, length({:prefix}))={:prefix}`
	params := dbx.Params{"workspaceId": workspaceID, "prefix": "workspace-import:" + jobID + ":"}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_teamspace_settings}} SET [[default_teamspace_ids]]=(
			SELECT json_group_array([[listed]].[[value]]) FROM json_each([[default_teamspace_ids]]) [[listed]]
			WHERE [[listed]].[[value]] NOT IN (` + created + `))
		WHERE [[workspace_id]]={:workspaceId} AND EXISTS (
			SELECT 1 FROM json_each([[default_teamspace_ids]]) [[listed]] WHERE [[listed]].[[value]] IN (` + created + `))
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_teamspaces}} WHERE [[workspace_id]]={:workspaceId} AND [[id]] IN (` + created + `)
	`).Bind(params).Execute()
	return err
}
