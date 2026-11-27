package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// validateWorkspaceExportLinksInside refuses an archive whose linked
// databases or relations reach a database outside it, as decided 2026-09-24:
// a page's archive names why, so the person can export the whole workspace
// instead. `linkedScope` and `sourceScope` test whether the linking and the
// linked database are inside. A database out of everyone's sight, in its 30
// days or being deleted, is none: the archive leaves out what names it, as
// once it is gone (workspace_export_gone_links.go).
func validateWorkspaceExportLinksInside(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	targetID, linkedScope, sourceScope string,
) error {
	// Every linked member, including a linked database's own member, needs its
	// source database in the same export.
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*)
		FROM {{leafen_database_containers}} AS [[linked]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]]=[[linked]].[[id]]
		JOIN {{leafen_database_containers}} AS [[source]]
			ON [[source]].[[id]]=[[membership]].[[source_container_id]]
			AND [[source]].[[owner]]=[[linked]].[[owner]]
			AND [[source]].[[workspace_id]]=[[linked]].[[workspace_id]]
			AND [[source]].[[kind]]='source'
		WHERE [[linked]].[[owner]]={:owner} AND [[linked]].[[workspace_id]]={:workspaceId}
			AND [[membership]].[[source_container_id]]!='' AND ` + linkedScope + ` AND NOT ` + sourceScope + `
			AND ` + pageOutsideDeletionFenceSQL("[[source]].[[page_id]]", "{:workspaceId}"),
	).Bind(workspaceExportLinkParams(authorization, job, targetID)).Row(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return newWorkspaceExportFailure(errorCodeWorkspaceExportLinkOutside)
	}
	return validateWorkspaceExportScopedRelationTargets(app, authorization, job, targetID, linkedScope, sourceScope)
}

func workspaceExportLinkParams(authorization workspaceAuthorization, job storedJob, targetID string) dbx.Params {
	return dbx.Params{
		"jobId": job.ID, "owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID,
		"actorId": authorization.PrincipalID, "targetId": targetID,
	}
}

// validateWorkspaceExportScopedRelationTargets refuses a scoped archive whose
// relations target a database outside it; `linkedScope` and `sourceScope`
// test the relation's own database and its target's.
func validateWorkspaceExportScopedRelationTargets(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	targetID, linkedScope, sourceScope string,
) error {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*)
		FROM {{leafen_database_relation_property_targets}} AS [[relation]]
		JOIN {{leafen_database_container_sources}} AS [[own]]
			ON [[own]].[[data_source_id]]=[[relation]].[[source_id]] AND [[own]].[[source_container_id]]=''
		JOIN {{leafen_database_containers}} AS [[linked]]
			ON [[linked]].[[id]]=[[own]].[[container_id]] AND [[linked]].[[owner]]=[[relation]].[[owner]]
		JOIN {{leafen_database_container_sources}} AS [[target]]
			ON [[target]].[[data_source_id]]=[[relation]].[[target_source_id]] AND [[target]].[[source_container_id]]=''
		JOIN {{leafen_database_containers}} AS [[source]]
			ON [[source]].[[id]]=[[target]].[[container_id]] AND [[source]].[[owner]]=[[relation]].[[owner]]
		WHERE [[relation]].[[owner]]={:owner} AND [[relation]].[[workspace_id]]={:workspaceId}
			AND ` + linkedScope + ` AND NOT ` + sourceScope + `
			AND ` + pageOutsideDeletionFenceSQL("[[source]].[[page_id]]", "{:workspaceId}"),
	).Bind(workspaceExportLinkParams(authorization, job, targetID)).Row(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return newWorkspaceExportFailure(errorCodeWorkspaceExportLinkOutside)
	}
	return nil
}
