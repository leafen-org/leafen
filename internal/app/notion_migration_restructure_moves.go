package app

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The restructure steps that move what an import brought: a page under
// another of its pages and a database onto another of its pages, each
// through the move a person makes on screen. A merged database came over
// into its target as the import made it (notion_migration_merge.go).

var errNotionMigrationRestructure = errors.New(errorCodeNotionMigrationFailed)

func notionMigrationDataSourcePage(app core.App, dataSourceID string) (string, error) {
	var pageID string
	err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&pageID)
	return pageID, err
}

// restructureNotionPageMove puts an imported page under another one.
func restructureNotionPageMove(app core.App, job storedJob, step notionMigrationStep) error {
	pageID, err := notionImportMappedID(app, job.ID, step.SourceID, "page")
	if err != nil {
		return err
	}
	parentID, err := notionImportMappedID(app, job.ID, step.TargetID, "page")
	if err != nil || pageID == "" || parentID == "" {
		return err
	}
	return moveNotionMigrationPage(app, job, pageID, parentID)
}

func moveNotionMigrationPage(app core.App, job storedJob, pageID, parentID string) error {
	if err := passNotionMigrationMoveTakenAway(app, pageID, parentID); err != nil {
		return err
	}
	authorization, err := authorizePage(app, job.Owner, pageID, permissionWorkspaceRead)
	if err != nil {
		return err
	}
	if authorization.Page.GetString("parent_id") == parentID {
		return nil
	}
	position, err := nextPageSiblingPosition(app, job.WorkspaceID, parentID)
	if err != nil {
		return err
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: authorization.Page.GetString("title"), ParentID: parentID, InTrash: authorization.Page.GetBool("in_trash"),
		Position: &position, MutationID: "notion-migration-move-" + job.ID + "-" + pageID,
		BaseCursor: authorization.Page.GetInt("cursor"),
	})
	if !validPageMetadataMutationInput(pageID, input) {
		return errNotionMigrationRestructure
	}
	digest := sha256.Sum256([]byte("notion-migration-move\x00" + job.ID + "\x00" + pageID + "\x00" + parentID))
	_, status, err := applyPageMetadataTransaction(app, job.Owner, pageID, input, hex.EncodeToString(digest[:]))
	if err != nil {
		return notionImportAt("page", pageID, err)
	}
	if status != pageMetadataApplied {
		return notionImportAt("page", pageID, fmt.Errorf("%w: page move %v", errNotionMigrationRestructure, status))
	}
	return nil
}

// restructureNotionDatabaseMove puts an imported database on another of
// the import's pages: a database imported on its own moves with its page,
// one on a page moves as its block.
func restructureNotionDatabaseMove(app core.App, job storedJob, step notionMigrationStep) error {
	parentID, err := notionImportMappedID(app, job.ID, step.TargetID, "page")
	if err != nil || parentID == "" {
		return err
	}
	rootPageID, err := notionImportMappedID(app, job.ID, notionDatabaseRootPageSourceID(step.SourceID), "page")
	if err != nil {
		return err
	}
	if rootPageID != "" {
		return moveNotionMigrationPage(app, job, rootPageID, parentID)
	}
	dataSourceID, err := notionImportMappedID(app, job.ID, step.SourceID, "data_source")
	if err != nil {
		return err
	}
	blockID, err := notionImportMappedID(app, job.ID, step.SourceID+":block", "block")
	if err != nil || dataSourceID == "" || blockID == "" {
		return err
	}
	sourcePageID, err := notionMigrationDataSourcePage(app, dataSourceID)
	if err != nil || sourcePageID == parentID {
		return err
	}
	if err := passNotionMigrationMoveTakenAway(app, sourcePageID, parentID); err != nil {
		return err
	}
	_, err = admitBlockMove(app, job.Owner, sourcePageID, blockID, blockMoveInput{
		RequestID: "notion-migration-" + job.ID + "-" + blockID, DestinationPageID: parentID, Operation: blockMoveOperationMove,
	})
	return err
}

// passNotionMigrationMoveTakenAway is errNotionImportPassedBy when the page to
// move or the one it goes under was moved to the trash or deleted while the
// run went on (notion_import_changed_meanwhile.go).
func passNotionMigrationMoveTakenAway(app core.App, pageIDs ...string) error {
	for _, pageID := range pageIDs {
		if away, err := notionImportPageTakenAway(app, pageID); err != nil || away {
			return cmp.Or(err, errNotionImportPassedBy)
		}
	}
	return nil
}
