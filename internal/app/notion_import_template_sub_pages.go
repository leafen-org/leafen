package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A template an import makes of a row takes copies of the sub-pages the
// row's body held (database_row_page_sub_page_copies.go), and the row goes
// once they are copied (removeNotionImportTemplateRow). The row's own
// sub-pages were only ever the template's: each one the template has a copy
// of goes for good with the row, as the trash's "delete forever" takes a
// page, rather than staying in the trash beside the template's copy
// (2026-11-16: a template's sub-page was left in the trash after a run); and
// a migration's ledger names the copy for the page Notion gave, so a run
// again knows the template's own. One whose copy didn't come stays where the
// row leaves it. A run again that gives a template Notion changed its body
// again (updateNotionImportTemplate) passes by the sub-pages it knows, so
// the template's own keep their blocks in its new body.

// retireNotionImportTemplateSubPages takes away, for good, a removed
// template row's sub-pages the template holds copies of.
func retireNotionImportTemplateSubPages(app core.App, ownerID, workspaceID, rowID, templateID string) error {
	copies := []struct {
		Source string `db:"source_root_page_id"`
		Target string `db:"target_root_page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[copy]].[[source_root_page_id]], [[copy]].[[target_root_page_id]]
		FROM {{leafen_page_duplication_jobs}} AS [[copy]]
		JOIN {{leafen_pages}} AS [[source]] ON [[source]].[[id]]=[[copy]].[[source_root_page_id]]
		JOIN {{leafen_pages}} AS [[target]] ON [[target]].[[id]]=[[copy]].[[target_root_page_id]]
		WHERE [[copy]].[[workspace_id]]={:workspaceId} AND [[copy]].[[destination_parent_id]]={:templateId}
			AND [[copy]].[[phase]]='completed' AND [[source]].[[parent_id]]={:rowId} AND [[source]].[[data_source_id]]=''
			AND [[target]].[[parent_id]]={:templateId}
		ORDER BY [[copy]].[[source_root_page_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "templateId": templateID, "rowId": rowID}).All(&copies); err != nil {
		return err
	}
	for _, copied := range copies {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_migration_ledger}} SET [[native_id]]={:target}
			WHERE [[workspace_id]]={:workspaceId} AND [[kind]]='page' AND [[native_id]]={:source}
		`).Bind(dbx.Params{"workspaceId": workspaceID, "source": copied.Source, "target": copied.Target}).Execute(); err != nil {
			return err
		}
		// The row is in the trash by now, and the page with it.
		_, _, err := holdPageForPermanentDeletion(app, personAccessSubject(ownerID), copied.Source, "notion-import-template-sub-page:"+copied.Source)
		if err != nil && !isPermissionDenied(err) && !errors.Is(err, errPageDeletionJobExists) &&
			!errors.Is(err, errPagePermanentDeleteNotTrashed) {
			return err
		}
	}
	return nil
}
