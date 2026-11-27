package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's row page and a full-page database keep the icon and cover
// their export names as any page does (workspace_import_page_head.go): a row
// page's document opens with the line too, and a database's CSV, its
// export drawing its page beside it (Notion's HTML export, Leafen's), opens
// with the line of that page's header (fileImportSources.ts). Brought back,
// they are the row page's and the database page's (2026-11-14: they came for
// pages only, a row page's being no page of the page metadata and a
// database's drawing left out).

// headWorkspaceImportRowPage gives a row page the icon and cover its
// document's header names, each while it has none, on its page record as
// a row page keeps them (row_page_presentation.go), as the archive import
// gives them (workspace_import_archive_after_apply.go). Its views read it
// again once the import is done.
func headWorkspaceImportRowPage(app core.App, actorID string, page *core.Record, head workspaceImportStagedPageHead) error {
	rowID, workspaceID := page.Id, page.GetString("workspace")
	var containerPageID string
	err := app.DB().NewQuery(`
		SELECT [[container_page_id]] FROM {{leafen_database_row_pages}} WHERE [[row_id]]={:rowId} LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID}).Row(&containerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if kind := page.GetString("icon_type"); head.Icon != "" && page.GetString("icon") == "" && (kind == "" || kind == "none") {
		if err := updateDatabaseRowPagePresentation(app, rowID, workspaceID, containerPageID, actorID, head.Icon, head.IconType); err != nil {
			return err
		}
	}
	if head.Cover != "" && page.GetString("cover") == "" {
		position := head.Position
		if _, err := setRowPageRecordCover(app, rowID, workspaceID, actorID, head.Cover, &position); err != nil {
			return err
		}
		return replaceProductFilePageReferences(app, workspaceID, rowID, "", "none", head.Cover)
	}
	return nil
}

// workspaceImportHeadedCSV takes a CSV's page line off its start, the head of
// the database's page, which its items give first.
func workspaceImportHeadedCSV(value string, parse func(string) (workspaceImportItemSource, error)) (workspaceImportItemSource, error) {
	value, head, headed := workspaceImportPageHeadOf(value)
	source, err := parse(value)
	if err != nil || !headed {
		return source, err
	}
	return func(emit func(workspaceImportStagedItem) error) error {
		if err := emit(workspaceImportStagedItem{Head: &head}); err != nil {
			return err
		}
		return source(emit)
	}, nil
}

// headWorkspaceImportStagingPage gives the import's own page, which no one
// sees until the import is done (a CSV's database page,
// workspace_import_runner.go), the icon and cover its header names, each
// while it has none, on its record, as the import gave it its title there.
func headWorkspaceImportStagingPage(app core.App, page *core.Record, head workspaceImportStagedPageHead) (*core.Record, error) {
	input := pageMetadataInput{Title: page.GetString("title"), MutationID: "workspace-import-head", BaseCursor: page.GetInt("cursor")}
	if kind := page.GetString("icon_type"); head.Icon != "" && page.GetString("icon") == "" && (kind == "" || kind == "none") {
		input.Icon, input.IconType = &head.Icon, &head.IconType
	}
	if head.Cover != "" && page.GetString("cover") == "" {
		input.Cover, input.CoverPosition = &head.Cover, &head.Position
	}
	if input.Icon == nil && input.Cover == nil {
		return page, nil
	}
	if input = normalizePageMetadataInput(input); !validPageMetadataMutationInput(page.Id, input) {
		return page, nil
	}
	if input.Icon != nil {
		page.Set("icon", *input.Icon)
		page.Set("icon_type", *input.IconType)
	}
	if input.Cover != nil {
		page.Set("cover", *input.Cover)
		page.Set("cover_position", *input.CoverPosition)
	}
	if err := app.Save(page); err != nil {
		return nil, err
	}
	return page, replaceProductFilePageReferences(app, page.GetString("workspace"), page.Id,
		page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"))
}
