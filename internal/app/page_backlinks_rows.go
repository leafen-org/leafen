package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pageBacklinkSourceIDs names each source page once. A database page is
// read by its own access: through the page that holds its database or by
// its own shares.
func pageBacklinkSourceIDs(rows []storedPageBacklink) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SourcePageID)
	}
	return uniqueStrings(ids)
}

// pageBacklinkPathIDs names the pages whose paths a window shows: a page
// itself, or the page that holds a database item.
func pageBacklinkPathIDs(rows []storedPageBacklink) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id := row.SourcePageID
		if row.RowContainerPageID != "" {
			id = row.RowContainerPageID
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// withoutTrashedPageBacklinkSources leaves out the sources in the trash with
// a page above them: a page under a trashed page, or a database item whose
// database sits in one, went there with it (loadPageTrashEffects), as
// Notion's backlinks leave a trashed page's sub-page out (seen 2026-11-24 in
// Chrome: two backlinks, one once its source's parent was in the trash).
func withoutTrashedPageBacklinkSources(app core.App, rows []storedPageBacklink) ([]storedPageBacklink, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	trashed, err := loadPageTrashEffects(app, pageBacklinkPathIDs(rows))
	if err != nil {
		return nil, err
	}
	if len(trashed) == 0 {
		return rows, nil
	}
	kept := rows[:0]
	for _, row := range rows {
		through := row.SourcePageID
		if row.RowContainerPageID != "" {
			through = row.RowContainerPageID
		}
		if _, inTrash := trashed[through]; inTrash {
			continue
		}
		kept = append(kept, row)
	}
	return kept, nil
}

// loadPageBacklinkRowPages shows each database item source as a page under
// the page that holds its database, with the item's title and icon.
func loadPageBacklinkRowPages(ctx context.Context, app core.App, principalID, workspaceID string, rows []storedPageBacklink) ([]pageResponse, error) {
	containers := map[string]string{}
	for _, row := range rows {
		if row.RowContainerPageID != "" {
			containers[row.SourcePageID] = row.RowContainerPageID
		}
	}
	if len(containers) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(containers))
	for id := range containers {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	items := []struct {
		RowID        string `db:"row_id"`
		DataSourceID string `db:"data_source_id"`
		Icon         string `db:"icon"`
		IconType     string `db:"icon_type"`
		Title        string `db:"title"`
		Created      int64  `db:"created"`
		Updated      int64  `db:"updated"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row_page]].[[row_id]], [[row_page]].[[data_source_id]], [[row_record]].[[icon]], [[row_record]].[[icon_type]],
			[[row_page]].[[created]], [[row_page]].[[updated]],
			COALESCE((
				SELECT json_extract([[value]].[[value]], '$') FROM {{leafen_database_row_values}} [[value]]
				WHERE [[value]].[[row_id]] = [[row_page]].[[row_id]] AND [[value]].[[type]] = 'title' LIMIT 1
			), '') AS [[title]]
		FROM {{leafen_database_row_pages}} [[row_page]]
		JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]] = [[row_page]].[[row_id]]
		WHERE [[row_page]].[[row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&items); err != nil {
		return nil, err
	}
	pages := make([]pageResponse, 0, len(items))
	// An item is called after its database's default template while it has no title.
	untitled := newDatabaseUntitledRowNames(ctx, app, principalID)
	for _, item := range items {
		iconType := item.IconType
		if strings.TrimSpace(iconType) == "" || iconType == "none" {
			iconType = "emoji"
		}
		name, err := untitled.of(item.DataSourceID)
		if err != nil {
			return nil, err
		}
		// Its times order it among the sources, as Notion lists the last
		// edited first (seen 2026-11-10).
		pages = append(pages, pageResponse{
			ID: item.RowID, WorkspaceID: workspaceID, Title: item.Title,
			ParentID: containers[item.RowID], Icon: item.Icon, IconType: iconType, UntitledName: name,
			CreatedAt: pageBacklinkRowTime(item.Created), UpdatedAt: pageBacklinkRowTime(item.Updated),
		})
	}
	return pages, nil
}

// pageBacklinkRowTime writes a database page's time as a page record's.
func pageBacklinkRowTime(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format("2006-01-02 15:04:05.000Z")
}

// withPageBacklinkRowDatabases names, for each database page source, the
// inline database between the page that holds it and the item, as Notion's
// path shows a row's database (seen 2026-11-10: "Top / … / Database").
func withPageBacklinkRowDatabases(ctx context.Context, app core.App, principalID, workspaceID string, rows []storedPageBacklink) error {
	locations := newInlineDatabaseLocations(app, principalID, workspaceID).reading(ctx)
	for index, row := range rows {
		if row.RowContainerPageID == "" {
			continue
		}
		location, err := locations.of(row.RowContainerPageID, row.RowDataSourceID)
		if err != nil {
			return err
		}
		rows[index].RowDatabase = location
	}
	return nil
}

// pageBacklinkFieldRowsSQL reads the sources that are no blocks: a database
// page's cell, shown under the database's page as its body is, and an
// ordinary page's title (page_field_references_schema.go). A cell's
// relation names only the page it links here. A cell is found by its row
// and property; its owner and workspace are read after (+), or SQLite reads
// the workspace's every cell for each reference.
func pageBacklinkFieldRowsSQL(notParent, comparison string) string {
	spans := func(column string) string { return `json(` + jsonArraySQL(column) + `)` }
	milliseconds := func(column string) string {
		return `IFNULL(CAST((julianday(` + column + `) - 2440587.5) * 86400000 AS INTEGER), 0)`
	}
	return `
		SELECT [[reference]].[[block_id]], [[reference]].[[source_page_id]],
			'' AS [[parent_id]], [[reference]].[[source_position]], '` + pageFieldReferenceCellType + `' AS [[type]],
			CASE WHEN [[cell]].[[type]] = 'relation'
				THEN json_object('relation', json_array([[reference]].[[target_page_id]]))
				ELSE json_object('rich', ` + spans("[[cell]].[[spans]]") + `) END AS [[content]],
			[[cell]].[[created]] AS [[created_at]], [[cell]].[[updated]] AS [[updated_at]],
			[[row_page]].[[container_page_id]] AS [[row_container_page_id]],
			[[row_page]].[[data_source_id]] AS [[row_data_source_id]]
		FROM {{leafen_block_page_references}} [[reference]]
			INDEXED BY [[idx_leafen_block_page_references_target]]
		JOIN {{leafen_database_row_values}} [[cell]] ON [[cell]].[[row_id]]=[[reference]].[[source_page_id]]
			AND [[reference]].[[block_id]]=` + pageFieldCellKeySQL("[[cell]]") + `
		JOIN {{leafen_database_row_pages}} [[row_page]] ON [[row_page]].[[row_id]]=[[reference]].[[source_page_id]]
		WHERE [[reference]].[[workspace_id]]={:workspaceId}
			AND [[reference]].[[target_page_id]]={:targetPageId}
			AND +[[cell]].[[owner]]={:ownerId} AND +[[cell]].[[workspace_id]]={:workspaceId}
			AND [[row_page]].[[owner]]={:ownerId} AND [[row_page]].[[workspace_id]]={:workspaceId}
			AND ` + databaseRowNotTrashedSQL("[[row_page]].[[row_id]]") + `
			AND ` + databaseSourceNotTrashedSQL("[[row_page]].[[data_source_id]]") + `
			AND ` + searchSourceNotDeletedSQL("[[row_page]].[[workspace_id]]", "[[row_page]].[[data_source_id]]") + `
			AND ` + notParent + `
			AND (` + comparison + `)
		UNION ALL
		SELECT [[reference]].[[block_id]], [[reference]].[[source_page_id]],
			'' AS [[parent_id]], [[reference]].[[source_position]], '` + pageFieldReferenceTitleType + `' AS [[type]],
			json_object('rich', ` + spans("[[page]].[[title_spans]]") + `) AS [[content]],
			` + milliseconds("[[page]].[[created]]") + ` AS [[created_at]],
			` + milliseconds("[[page]].[[updated]]") + ` AS [[updated_at]],
			'' AS [[row_container_page_id]], '' AS [[row_data_source_id]]
		FROM {{leafen_block_page_references}} [[reference]]
			INDEXED BY [[idx_leafen_block_page_references_target]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[reference]].[[source_page_id]]
			AND [[reference]].[[block_id]]=` + pageFieldTitleKeySQL("[[page]]") + `
		WHERE [[reference]].[[workspace_id]]={:workspaceId}
			AND [[reference]].[[target_page_id]]={:targetPageId}
			AND [[page]].[[owner]]={:ownerId} AND [[page]].[[workspace]]={:workspaceId}
			AND [[page]].[[notion_import_staging]]=0 AND [[page]].[[in_trash]]=0
			AND ` + pageTitleSpellsSQL("[[page]]") + `
			AND ` + notParent + `
			AND (` + comparison + `)`
}

// authorizePageBacklinksTarget reads the pages or database item whose
// backlinks are asked for: a database item is mentioned by its row's ID and
// read through its own access.
func authorizePageBacklinksTarget(app core.App, principalID, targetID string) (string, string, error) {
	// Other pages' links to a page an operation holds a while still read (page_access_hold.go).
	authorization, err := pageAccessDespiteHold(authorizePage(app, principalID, targetID, permissionWorkspaceRead))
	if err == nil {
		return authorization.Workspace.WorkspaceID, authorization.Workspace.OwnerID, nil
	}
	if !isPermissionDenied(err) {
		return "", "", err
	}
	row, rowErr := authorizeDatabaseRowPage(app, principalID, targetID, permissionWorkspaceRead)
	if rowErr == nil {
		return row.WorkspaceID, row.OwnerID, nil
	}
	if isPermissionDenied(rowErr) || errors.Is(rowErr, errDatabaseRowPageNotFound) ||
		errors.Is(rowErr, errDatabaseRowPageInTrash) {
		return "", "", errPageBacklinksNotFound
	}
	return "", "", rowErr
}
