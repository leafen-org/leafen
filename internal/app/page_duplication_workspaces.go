package app

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copy into another workspace leaves the page where it is and makes one
// there, as Notion's "옮기기" does across workspaces (its help, read
// 2026-10-01); on this server a move there moves the page itself
// (page_workspace_move.go). A copy then reads its pages from one workspace and
// writes them to another: the job keeps the workspace it reads, with its
// owner, beside the one it writes. A copy within a workspace reads and writes
// the same one.
//
// A linked database of a database the copy doesn't take comes along, its
// views with it, as a linked view not yet linked, as Notion's copy leaves it
// (page_duplication_unlinked_views.go).

// sourceOwner is the owner of the pages a copy reads.
func (job storedPageDuplicationJob) sourceOwner() string {
	if job.SourceOwnerID != "" {
		return job.SourceOwnerID
	}
	return job.OwnerID
}

// sourceWorkspace is the workspace of the pages a copy reads.
func (job storedPageDuplicationJob) sourceWorkspace() string {
	if job.SourceWorkspaceID != "" {
		return job.SourceWorkspaceID
	}
	return job.WorkspaceID
}

// crossWorkspace reports a copy into another workspace.
func (job storedPageDuplicationJob) crossWorkspace() bool {
	return job.sourceWorkspace() != job.WorkspaceID
}

// pageDuplicationTargetWorkspace is the workspace a copy goes to: the one
// the request names, which the person must be able to write, or the page's.
func pageDuplicationTargetWorkspace(
	app core.App, actorID string, source workspaceAuthorization, input pageDuplicationInput,
) (workspaceAuthorization, error) {
	if input.DestinationWorkspaceID == "" || input.DestinationWorkspaceID == source.WorkspaceID {
		return source, nil
	}
	target, err := authorizeWorkspace(app, actorID, input.DestinationWorkspaceID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return workspaceAuthorization{}, destinationWorkspaceRefused(app, actorID, input.DestinationWorkspaceID, errorCodePageDuplicationInvalid)
	}
	return target, err
}

// pageDuplicationTargetTeamspaceFailure checks the teamspace a copy into
// another workspace goes to the top of: one of that workspace's that the
// person may add pages to, with no page named to go under. A copy to the top
// of the person's private pages — asked for, into another workspace without a
// teamspace, or beside a private page of theirs — is refused a restricted
// member the organization keeps from making private pages
// (organization_request_controls.go).
func pageDuplicationTargetTeamspaceFailure(
	app core.App, actorID string, source, target workspaceAuthorization, input pageDuplicationInput, sourcePage *core.Record,
) error {
	if input.DestinationTeamspaceID == "" {
		private := input.DestinationParentID == "" && (input.ToPrivatePages || target.WorkspaceID != source.WorkspaceID ||
			sourcePage != nil && sourcePage.GetString("teamspace_id") == "" && sourcePage.GetString("parent_id") == "")
		if !private {
			return nil
		}
		refused, err := restrictedPrivatePagesRefused(app, target)
		if err == nil && refused {
			return destinationPrivatePagesRefused("destinationTeamspaceId")
		}
		return err
	}
	if input.DestinationParentID != "" || target.WorkspaceID == source.WorkspaceID {
		return refusedFor(http.StatusBadRequest, errorCodePageDuplicationInvalid, "Check destinationTeamspaceId: a copy goes to the top "+
			"of a teamspace only in another workspace (destinationWorkspaceId), with no destinationParentId.",
			apiErrorField{Name: "destinationTeamspaceId", Problem: "not taken with these"})
	}
	return destinationTeamspaceRefused(app, actorID, target.WorkspaceID, input.DestinationTeamspaceID, errorCodePageDuplicationInvalid)
}

func pageDuplicationTeamspaceWritable(app core.App, actorID, workspaceID, teamspaceID string) error {
	teamspaceWorkspace, role, err := effectiveTeamspacePageAccessRole(app, actorID, workspaceID, teamspaceID)
	if errors.Is(err, errTeamspaceNotFound) || isPermissionDenied(err) ||
		(err == nil && (role < pageAccessRoleEdit || teamspaceWorkspace.WorkspaceID != workspaceID)) {
		return errPageDuplicationConflict
	}
	return err
}

// pageDuplicationToPrivatePages says whether the copy goes to the top of the
// person's private pages, as a wiki page's "Duplicate to private pages".
func pageDuplicationToPrivatePages(app core.App, jobID string) (bool, error) {
	raw := ""
	if err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": jobID}).Row(&raw); err != nil {
		return false, err
	}
	payload := struct {
		Private bool `json:"toPrivatePages"`
	}{}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.Private, nil
}

// pageDuplicationDestinationTeamspace is the teamspace a copy into another
// workspace goes to the top of, if any.
func pageDuplicationDestinationTeamspace(app core.App, jobID string) (string, error) {
	raw := ""
	if err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": jobID}).Row(&raw); err != nil {
		return "", err
	}
	payload := struct {
		TeamspaceID string `json:"destinationTeamspaceId"`
	}{}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.TeamspaceID, nil
}

// pageDuplicationCopiesDataSource reports whether a copy takes a data
// source: one a page it copies holds, or, for a database block's copy
// (blockCopyDuplicationJob), one of the databases it copies.
func pageDuplicationCopiesDataSource(app core.App, jobID, dataSourceID string) (bool, error) {
	var copied int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
			JOIN {{leafen_page_duplication_items}} AS [[item]]
				ON [[item]].[[job_id]]={:jobId} AND [[item]].[[source_page_id]]=[[source]].[[page_id]]
			WHERE [[source]].[[id]]={:dataSourceId})
		OR EXISTS(SELECT 1 FROM {{leafen_block_move_database_scopes}}
			WHERE [[job_id]]={:jobId} AND [[data_source_id]]={:dataSourceId})
	`).Bind(dbx.Params{"jobId": jobID, "dataSourceId": dataSourceID}).Row(&copied)
	return copied == 1, err
}

// pageDuplicationLeftOutProperties are the properties of a page's databases
// a copy into another workspace leaves out: relations to databases it
// doesn't copy, which stay in their workspace, and the rollups through them.
func pageDuplicationLeftOutProperties(
	app core.App, job storedPageDuplicationJob, pageID string,
) (map[string]struct{}, error) {
	if !job.crossWorkspace() {
		return map[string]struct{}{}, nil
	}
	return crossWorkspaceLeftOutProperties(app, job.sourceOwner(), job.sourceWorkspace(), pageID,
		func(dataSourceID string) (bool, error) {
			return pageDuplicationCopiesDataSource(app, job.ID, dataSourceID)
		})
}

// crossWorkspaceLeftOutProperties are the properties of the databases on a
// page that a copy into another workspace leaves out: relations to data
// sources it doesn't copy, as copied says, and the rollups through them.
func crossWorkspaceLeftOutProperties(
	app core.App, sourceOwnerID, sourceWorkspaceID, pageID string, copied func(string) (bool, error),
) (map[string]struct{}, error) {
	left := map[string]struct{}{}
	properties := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[type]],[[config]] FROM {{leafen_database_properties}}
		WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
			AND [[type]] IN ({:relation},{:rollup})
		ORDER BY [[type]],[[id]]
	`).Bind(dbx.Params{
		"ownerId": sourceOwnerID, "workspaceId": sourceWorkspaceID, "pageId": pageID,
		"relation": graphValueTypeRelation, "rollup": graphValueTypeRollup,
	}).All(&properties); err != nil {
		return nil, err
	}
	for _, property := range properties {
		if property.Type != graphValueTypeRelation {
			continue
		}
		target := graphRelationTarget(property.Type, json.RawMessage(property.Config))
		if target == "" {
			continue
		}
		taken, err := copied(target)
		if err != nil {
			return nil, err
		}
		if !taken {
			left[property.ID] = struct{}{}
		}
	}
	for _, property := range properties {
		if property.Type != graphValueTypeRollup {
			continue
		}
		if config, err := decodeDatabaseComputedConfig(property.Config); err == nil {
			relationID, _, _ := config.rollupFields()
			if _, through := left[relationID]; through {
				left[property.ID] = struct{}{}
			}
		}
	}
	return left, nil
}

// pageDuplicationViewConfigWithout drops from a view's config the
// properties it is grouped or dated by that its copy doesn't have.
func pageDuplicationViewConfigWithout(app core.App, jobID string, raw json.RawMessage) (json.RawMessage, error) {
	references := graphViewPropertyReferences(raw)
	if len(references) == 0 {
		return raw, nil
	}
	mappings, err := pageDuplicationReferenceMappings(app, jobID, "property", references)
	if err != nil {
		return nil, err
	}
	config := map[string]any{}
	if json.Unmarshal(raw, &config) != nil {
		return raw, nil
	}
	changed := false
	for _, key := range []string{"groupBy", "calendarBy", "startDatePropertyId", "endDatePropertyId"} {
		value, _ := config[key].(string)
		if value == "" {
			continue
		}
		if _, kept := mappings[value]; !kept {
			delete(config, key)
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(config)
}
