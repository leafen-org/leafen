package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceActivityDeletionTarget struct {
	EntityType string
	Metadata   workspaceActivityMetadata
}

// Only the last successful deletion turn records a user activity. Admission
// and background retention turns cannot impersonate a completed user change.
func captureWorkspaceActivityDeletion(app core.App, job storedDatabaseMutationDeletion) (workspaceActivityDeletionTarget, error) {
	target := workspaceActivityDeletionTarget{Metadata: workspaceActivityMetadata{LocationID: job.PageID}}
	query := ""
	switch job.TargetKind {
	case databaseMutationTargetSource:
		target.EntityType = "dataSource"
		target.Metadata.SourceID = job.TargetID
		query = `SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`
	case databaseMutationTargetProperty:
		target.EntityType = "property"
		query = `SELECT [[name]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}`
	case databaseMutationTargetRow:
		target.EntityType = "page"
		target.Metadata.SubjectPageID = job.TargetID
		target.Metadata.Name = databaseRowPageTitle(app, job.TargetID)
		return target, nil
	default:
		return target, errWorkspaceActivityInvalid
	}
	err := app.DB().NewQuery(query).Bind(dbx.Params{"id": job.TargetID}).Row(&target.Metadata.Name)
	return target, err
}

func recordWorkspaceActivityDeletion(app core.App, job storedDatabaseMutationDeletion, target workspaceActivityDeletionTarget, at int64) error {
	page, err := app.FindRecordById(pagesCollection, job.PageID)
	if err != nil {
		return err
	}
	return recordWorkspaceActivity(app, page, job.ActorID, "deletion:"+job.ID, target.EntityType, job.TargetID, "delete", target.Metadata, at)
}
