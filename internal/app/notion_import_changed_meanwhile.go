package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A person, or their agent, may go on working in the workspace while a run
// goes on (2026-11-16: a real run's agent moved to the trash the rows its
// templates came as and tidied views and layouts, and the run stopped at its
// very end, its templates unmade and its report silent about it). What a
// run's last steps would change on something a person moved to the trash or
// deleted since is left as they left it: the step passes it by, the run's
// report counts it (changedMeanwhile), and the run goes on. A row that stood
// for a template is the one exception: it only ever carried the template, so
// the template is still made and the row goes (removeNotionImportTemplateRow).

// errNotionImportPassedBy is a step's item a person took away meanwhile,
// passed by.
var errNotionImportPassedBy = errors.New("notion import item changed meanwhile")

// notionImportChangedMeanwhile reports a write a step was refused because a
// person moved its page, row or database to the trash, or deleted it, while
// the run went on: the import's owner may write everything the import made,
// so a refusal of its own item says the item was taken out of its reach.
func notionImportChangedMeanwhile(err error) bool {
	if err == nil {
		return false
	}
	requestError := &graphRequestError{}
	return errors.Is(err, errNotionImportPassedBy) || databaseRowTrashRefused(err) || errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) ||
		errors.As(err, &requestError) && (requestError.code == errorCodePageInTrash || requestError.code == errorCodePageNotFound)
}

// notionImportPageTakenAway reports a page the import made that is in the
// trash now, under a page that is, or deleted.
func notionImportPageTakenAway(app core.App, pageID string) (bool, error) {
	trashed, err := pageInTrashPath(app, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return trashed, err
}

// countNotionMigrationChangedMeanwhile adds to a run's report what its steps
// passed by for a person's change.
func countNotionMigrationChangedMeanwhile(app core.App, migrationID string, count int) error {
	if migrationID == "" || count == 0 {
		return nil
	}
	return updateNotionMigrationRunReport(app, migrationID, func(report *notionMigrationRunReport) {
		report.ChangedMeanwhile += count
	})
}

// removeNotionImportTemplateRow takes a row that stood for a template away
// for good, the databases of its body with their views first. Until the run's
// end such a row shows among the database's rows, and a person may move it to
// the trash meanwhile: it goes from the trash for good as it would have from
// its database — unless it went there with another row, whose trash keeps it.
func removeNotionImportTemplateRow(app core.App, ownerID, authorityOwnerID, workspaceID, rowID string) error {
	if err := deleteNotionImportPageDatabases(app, authorityOwnerID, workspaceID, ownerID, rowID); err != nil {
		return err
	}
	rowAuthority, err := authorizeDatabaseRowPageInTrash(app, ownerID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	switch {
	case rowAuthority.TrashedAt == 0:
		if _, _, err := databaseRowPageMutationAuthorization(app, ownerID, rowID, permissionWorkspaceWrite); err != nil {
			return err
		}
		if _, err := trashDatabaseRowEvent(app, rowAuthority, ownerID, time.Now().UTC()); err != nil {
			return err
		}
	case rowAuthority.TrashRootID != rowID:
		return nil
	}
	_, err = deleteDatabaseRowTrashEvent(app, rowID, ownerID)
	return err
}
