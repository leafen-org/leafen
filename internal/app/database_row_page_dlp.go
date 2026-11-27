package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A database page's icon and cover are what it is called, as a page's are
// (page_metadata.go): one that changes meets its organization's DLP policy
// however it comes — the screen's page head and PATCH /pages/{rowId}/metadata
// (database_row_page_presentation.go), and the Notion-compatible API's page
// update and create and MCP's page update (notion_database_row_page_presentation.go)
// — and a blocked one is refused as a page's is. Its font, width, small text
// and the cover's place are no content; an icon or cover set as it already is
// passes, as a page's does. Imports check what they bring as they are admitted
// (workspace_import_admission.go).
func enforceDatabaseRowPageLookDLP(app core.App, workspaceID, rowID string, icon, cover *string) error {
	if icon == nil && cover == nil {
		return nil
	}
	workspace, err := findRecordRemembered(app, workspacesCollection, workspaceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errDatabaseRowPageNotFound
		}
		return err
	}
	organizationID := workspace.GetString("organization")
	if organizationID == "" {
		return nil
	}
	current, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	was := func(field string) string {
		if current == nil {
			return ""
		}
		return current.GetString(field)
	}
	changed := []any{}
	if icon != nil && *icon != was("icon") {
		changed = append(changed, *icon)
	}
	if cover != nil && *cover != was("cover") {
		changed = append(changed, *cover)
	}
	if len(changed) == 0 {
		return nil
	}
	return enforceOrganizationDLPContent(app, organizationID, changed...)
}
