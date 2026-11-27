package app

import (
	"context"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Turning sprints off permanently deletes what turning them on made, as
// Notion does: the Sprint board and the Sprints database with its sprints.
// An active legal hold on either of them, anything inside them or the people
// who own those pages stops it before anything changes. The deletion is the
// server's own follow-through on the person's choice, so it runs as the
// system (system_job_access.go) and resumes under the same request when
// retried.

const databaseSprintDeletionMaxRounds = 512

var errDatabaseSprintDeletionUnfinished = errors.New("sprint pages were not permanently deleted")

func disableDatabaseSprints(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	rooms workspaceRoomInvalidator,
	actorID string,
	stored storedDatabaseSprints,
	requestID string,
) error {
	if err := assertDatabaseSprintPagesDeletable(app, stored); err != nil {
		return err
	}
	for _, pageID := range []string{stored.BoardPageID, stored.SprintsPageID} {
		if err := trashDatabaseSprintPage(app, actorID, pageID, requestID); err != nil {
			return err
		}
	}
	mutationID := "sprint-off-" + databaseSprintDeterministicID("sprints-off", stored.DataSourceID, requestID)
	err := applyDatabaseSprintMutation(ctx, ygo, actorID, stored.PageID, mutationID, false,
		func(records databasePageRecords) ([]graphOperation, error) {
			if _, exists, err := loadDatabasePropertyRecord(records.app, records.pageID, stored.TaskSprintPropertyID); err != nil || !exists {
				return []graphOperation{}, err
			}
			return []graphOperation{{
				Kind: graphOperationKindDeleteProperty, PropertyDeletion: &graphDatabaseDelete{ID: stored.TaskSprintPropertyID},
			}}, nil
		})
	if err != nil {
		return err
	}
	// Sprints stay on until both pages are gone, so a retry resumes the
	// same deletion.
	if err := deleteDatabaseSprintPagesPermanently(app, rooms, stored); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`DELETE FROM {{leafen_database_sprints}} WHERE [[data_source_id]] = {:dataSourceId}`).
		Bind(dbx.Params{"dataSourceId": stored.DataSourceID}).Execute()
	return err
}

// assertDatabaseSprintPagesDeletable refuses turning sprints off while a
// legal hold covers the sprint pages, their subpages or their owners: every
// page under them is checked, however many, each once, before anything
// changes.
func assertDatabaseSprintPagesDeletable(app core.App, stored storedDatabaseSprints) error {
	workspace, err := app.FindRecordById(workspacesCollection, stored.WorkspaceID)
	if err != nil {
		return err
	}
	organizationID := workspace.GetString("organization")
	pending := []string{stored.BoardPageID, stored.SprintsPageID}
	seen := map[string]bool{}
	for len(pending) > 0 {
		pageID := pending[0]
		pending = pending[1:]
		if pageID == "" || seen[pageID] {
			continue
		}
		seen[pageID] = true
		page, err := app.FindRecordById(pagesCollection, pageID)
		if err != nil {
			continue
		}
		if err := assertNoActiveLegalHoldForPermanentDelete(app, organizationID, legalHoldDeletionTarget{
			WorkspaceID: stored.WorkspaceID, PageIDs: []string{pageID},
			CustodianUserIDs: []string{page.GetString("owner")},
		}); err != nil {
			return err
		}
		children := []struct {
			ID string `db:"id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_pages}}
			WHERE [[workspace]] = {:workspaceId} AND [[parent_id]] = {:parentId}
		`).Bind(dbx.Params{"workspaceId": stored.WorkspaceID, "parentId": pageID}).All(&children); err != nil {
			return err
		}
		for _, child := range children {
			pending = append(pending, child.ID)
		}
	}
	return nil
}

// deleteDatabaseSprintPagesPermanently runs the permanent deletion of the
// trashed sprint pages to the end.
func deleteDatabaseSprintPagesPermanently(
	app core.App,
	rooms workspaceRoomInvalidator,
	stored storedDatabaseSprints,
) error {
	workspace, err := app.FindRecordById(workspacesCollection, stored.WorkspaceID)
	if err != nil {
		return err
	}
	system := systemJobSubjectFor(workspace)
	for _, pageID := range []string{stored.BoardPageID, stored.SprintsPageID} {
		if _, err := app.FindRecordById(pagesCollection, pageID); err != nil {
			continue
		}
		requestID := "sprint-delete-" + databaseSprintDeterministicID("sprints-delete", stored.DataSourceID, pageID)
		admission, err := admitPagePermanentDelete(app, system, pageID, requestID)
		if err != nil {
			return err
		}
		if admission.Job.Status == pagePermanentDeleteStatusCompleted {
			continue
		}
		if rooms != nil {
			if err := invalidatePermanentPageRooms(rooms, admission.Job.WorkspaceID); err != nil {
				return err
			}
		}
		finished := false
		for round := 0; round < databaseSprintDeletionMaxRounds && !finished; round++ {
			job, err := advancePagePermanentDelete(app, admission.Job.ID)
			if err != nil {
				return err
			}
			finished = job.Status == pagePermanentDeleteStatusCompleted
		}
		if !finished {
			return errDatabaseSprintDeletionUnfinished
		}
	}
	return nil
}
