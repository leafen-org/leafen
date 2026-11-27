package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageDuplicationSourceDataSource struct {
	ID                string `db:"id"`
	TargetID          string `db:"target_id"`
	Title             string `db:"title"`
	TitleSpans        string `db:"title_spans"`
	Icon              string `db:"icon"`
	IconType          string `db:"icon_type"`
	Description       string `db:"description"`
	DescriptionSpans  string `db:"description_spans"`
	DescriptionHidden bool   `db:"description_hidden"`
	Cover             string `db:"cover"`
	CoverPosition     int    `db:"cover_position"`
}

type pageDuplicationSourceDatabaseView struct {
	ID                 string `db:"id"`
	TargetID           string `db:"target_id"`
	DataSourceID       string `db:"data_source_id"`
	TargetDataSourceID string `db:"target_data_source_id"`
	TargetContainerID  string `db:"target_container_id"`
	Type               string `db:"type"`
	Name               string `db:"name"`
	Position           string `db:"position"`
	Config             string `db:"config"`
}

type pageDuplicationSourceDatabaseProperty struct {
	ID                 string `db:"id"`
	TargetID           string `db:"target_id"`
	DataSourceID       string `db:"data_source_id"`
	TargetDataSourceID string `db:"target_data_source_id"`
	Type               string `db:"type"`
	Name               string `db:"name"`
	Position           string `db:"position"`
	Config             string `db:"config"`
}

type pageDuplicationSourceDatabaseRow struct {
	ID                 string `db:"id"`
	TargetID           string `db:"target_id"`
	DataSourceID       string `db:"data_source_id"`
	TargetDataSourceID string `db:"target_data_source_id"`
	SubitemParentID    string `db:"subitem_parent_id"`
	TargetParentID     string `db:"target_parent_id"`
	Position           string `db:"position"`
	Created            int64  `db:"created"`
}

type pageDuplicationSourceDatabaseCell struct {
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Value      string `db:"value"`
	Spans      string `db:"spans"`
}

func loadPageDuplicationDatabaseOperations(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, *graphReferenceCache, string, bool, error) {
	switch state.Kind {
	case "data_source":
		operations, cursor, done, err := loadPageDuplicationDataSources(app, job, state)
		return operations, nil, cursor, done, err
	case "view":
		operations, cursor, done, err := loadPageDuplicationDatabaseViews(app, job, state)
		return operations, nil, cursor, done, err
	case "property":
		operations, cursor, done, err := loadPageDuplicationDatabaseProperties(app, job, state)
		return operations, nil, cursor, done, err
	case "row":
		return loadPageDuplicationDatabaseRows(app, job, state)
	case "dependency_edge":
		return loadPageDuplicationDatabaseDependencyOperations(app, job, state)
	default:
		return nil, nil, state.CopyCursor, false, errPageDuplicationConflict
	}
}

func loadPageDuplicationDataSources(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, string, bool, error) {
	rows := []pageDuplicationSourceDataSource{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]],[[mapping]].[[target_id]],[[source]].[[title]],[[source]].[[title_spans]],
			[[source]].[[icon]],[[source]].[[icon_type]],[[source]].[[description]],[[source]].[[description_spans]],
			[[source]].[[description_hidden]],[[source]].[[cover]],[[source]].[[cover_position]]
		FROM {{leafen_data_sources}} [[source]]
		JOIN {{leafen_page_duplication_identity_map}} [[mapping]]
			ON [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='data_source'
			AND [[mapping]].[[source_id]]=[[source]].[[id]]
		WHERE [[source]].[[workspace_id]]={:workspaceId} AND [[source]].[[owner]]={:ownerId}
			AND [[source]].[[page_id]]={:pageId} AND [[source]].[[id]]>{:cursor}
		ORDER BY [[source]].[[id]] LIMIT {:limit}
	`).Bind(pageDuplicationDatabaseWindowParams(job, state)).All(&rows)
	if err != nil {
		return nil, state.CopyCursor, false, err
	}
	hasMore := trimPageDuplicationWindow(&rows)
	// A name's and a description's formatting point at the copies, as text does.
	descriptions, titles := make([][]graphTextSpan, len(rows)), make([][]graphTextSpan, len(rows))
	for index, row := range rows {
		descriptions[index] = decodeStoredTextSpans(row.DescriptionSpans)
		titles[index] = decodeStoredTextSpans(row.TitleSpans)
	}
	if err := remapPageDuplicationSpans(app, job.ID, append(append([][]graphTextSpan(nil), descriptions...), titles...)); err != nil {
		return nil, state.CopyCursor, false, err
	}
	operations := make([]graphOperation, len(rows))
	root := loadPageDuplicationRoot(app, job, state.SourcePageID)
	for index, row := range rows {
		icon, iconType := row.Icon, row.IconType
		if iconID, managed := productFilePresentationID(icon); iconType == graphDataSourceIconImage && managed {
			mappings, err := pageDuplicationReferenceMappings(app, job.ID, "file", []string{iconID})
			if err != nil {
				return nil, state.CopyCursor, false, err
			}
			if mappings[iconID] == "" {
				return nil, state.CopyCursor, false, errPageDuplicationConflict
			}
			icon = productFilePresentationWithID(icon, mappings[iconID])
		}
		// A workspace emoji icon names the one a copy into another workspace brings.
		if icon, err = pageDuplicationCustomEmojiIcon(app, job.ID, icon, iconType); err != nil {
			return nil, state.CopyCursor, false, err
		}
		cover := row.Cover
		if coverID, managed := productFilePresentationID(cover); managed {
			mappings, err := pageDuplicationReferenceMappings(app, job.ID, "file", []string{coverID})
			if err != nil {
				return nil, state.CopyCursor, false, err
			}
			if mappings[coverID] == "" {
				return nil, state.CopyCursor, false, errPageDuplicationConflict
			}
			cover = productFilePresentationWithID(cover, mappings[coverID])
		}
		description, descriptionHidden, coverPosition := row.Description, row.DescriptionHidden, row.CoverPosition
		title, titleSpans := pageDuplicationSourceName(job, root, row.ID, row.Title, titles[index])
		operations[index] = graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{
			ID: row.TargetID, Title: title, TitleSpans: titleSpans, Icon: &icon, IconType: &iconType,
			Description: &description, DescriptionSpans: descriptions[index], DescriptionHidden: &descriptionHidden,
			Cover: &cover, CoverPosition: &coverPosition,
		}}
	}
	return operations, pageDuplicationDataSourceCursor(state.CopyCursor, rows), !hasMore, nil
}

func loadPageDuplicationDatabaseViews(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, string, bool, error) {
	rows := []pageDuplicationSourceDatabaseView{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]],[[self]].[[target_id]],[[source]].[[data_source_id]],
			COALESCE([[dataSource]].[[target_id]], [[source]].[[data_source_id]]) AS [[target_data_source_id]],
			COALESCE([[container]].[[target_id]], '') AS [[target_container_id]],[[source]].[[type]],
			[[source]].[[name]],[[source]].[[position]],[[source]].[[config]]
		FROM {{leafen_database_views}} [[source]]
		JOIN {{leafen_page_duplication_identity_map}} [[self]]
			ON [[self]].[[job_id]]={:jobId} AND [[self]].[[kind]]='view'
			AND [[self]].[[source_id]]=[[source]].[[id]]
		LEFT JOIN {{leafen_page_duplication_identity_map}} [[dataSource]]
			ON [[dataSource]].[[job_id]]={:jobId} AND [[dataSource]].[[kind]]='data_source'
			AND [[dataSource]].[[source_id]]=[[source]].[[data_source_id]]
		LEFT JOIN {{leafen_page_duplication_identity_map}} [[container]]
			ON [[container]].[[job_id]]={:jobId} AND [[container]].[[kind]]='block'
			AND [[container]].[[source_id]]=[[source]].[[container_id]]
		WHERE [[source]].[[workspace_id]]={:workspaceId} AND [[source]].[[owner]]={:ownerId}
			AND [[source]].[[page_id]]={:pageId} AND [[source]].[[id]]>{:cursor}
			AND (CASE WHEN [[source]].[[container_id]] = ''
				THEN [[dataSource]].[[target_id]] IS NOT NULL
				ELSE [[container]].[[target_id]] IS NOT NULL END)
		ORDER BY [[source]].[[id]] LIMIT {:limit}
	`).Bind(pageDuplicationDatabaseWindowParams(job, state)).All(&rows)
	if err != nil {
		return nil, state.CopyCursor, false, err
	}
	hasMore := trimPageDuplicationWindow(&rows)
	configs := make([]json.RawMessage, len(rows))
	for index := range rows {
		configs[index] = json.RawMessage(rows[index].Config)
		// A copy into another workspace has no property it left out.
		if job.crossWorkspace() {
			if configs[index], err = pageDuplicationViewConfigWithout(app, job.ID, configs[index]); err != nil {
				return nil, state.CopyCursor, false, err
			}
		}
	}
	configs, err = remapPageDuplicationDatabaseJSON(app, job.ID, configs)
	if err != nil {
		return nil, state.CopyCursor, false, err
	}
	operations := make([]graphOperation, len(rows))
	unlinks := newPageDuplicationUnlinks(app, job)
	for index, row := range rows {
		// A linked database's copied views stay linked to its source and
		// belong to the copied linked block; one its copy unlinks waits
		// for a source (page_duplication_unlinked_views.go).
		target := row.TargetDataSourceID
		if target == row.DataSourceID && row.TargetContainerID != "" {
			if target, err = unlinks.viewSource(target); err != nil {
				return nil, state.CopyCursor, false, err
			}
		}
		operations[index] = graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: row.TargetID, DataSourceID: target, ContainerID: row.TargetContainerID,
			Type: row.Type, Name: row.Name, Position: row.Position, Config: configs[index],
		}}
	}
	return operations, pageDuplicationViewCursor(state.CopyCursor, rows), !hasMore, nil
}

func loadPageDuplicationDatabaseProperties(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, string, bool, error) {
	rows := []pageDuplicationSourceDatabaseProperty{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]],[[self]].[[target_id]],[[source]].[[data_source_id]],
			[[dataSource]].[[target_id]] AS [[target_data_source_id]],[[source]].[[type]],
			[[source]].[[name]],[[source]].[[position]],[[source]].[[config]]
		FROM {{leafen_database_properties}} [[source]]
		JOIN {{leafen_page_duplication_identity_map}} [[self]]
			ON [[self]].[[job_id]]={:jobId} AND [[self]].[[kind]]='property'
			AND [[self]].[[source_id]]=[[source]].[[id]]
		JOIN {{leafen_page_duplication_identity_map}} [[dataSource]]
			ON [[dataSource]].[[job_id]]={:jobId} AND [[dataSource]].[[kind]]='data_source'
			AND [[dataSource]].[[source_id]]=[[source]].[[data_source_id]]
		WHERE [[source]].[[workspace_id]]={:workspaceId} AND [[source]].[[owner]]={:ownerId}
			AND [[source]].[[page_id]]={:pageId} AND [[source]].[[id]]>{:cursor}
		ORDER BY [[source]].[[id]] LIMIT {:limit}
	`).Bind(pageDuplicationDatabaseWindowParams(job, state)).All(&rows)
	if err != nil {
		return nil, state.CopyCursor, false, err
	}
	hasMore := trimPageDuplicationWindow(&rows)
	configs := make([]json.RawMessage, len(rows))
	types := make([]string, len(rows))
	for index := range rows {
		configs[index] = json.RawMessage(rows[index].Config)
		types[index] = rows[index].Type
	}
	if err := pageDuplicationRelationsOneWayOutside(app, job.ID, types, configs); err != nil {
		return nil, state.CopyCursor, false, err
	}
	configs, err = remapPageDuplicationDatabaseJSON(app, job.ID, configs)
	if err != nil {
		return nil, state.CopyCursor, false, err
	}
	operations := make([]graphOperation, len(rows))
	for index, row := range rows {
		operations[index] = graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: row.TargetID, DataSourceID: row.TargetDataSourceID, Type: row.Type,
			Name: row.Name, Position: row.Position, Config: configs[index],
		}}
	}
	return operations, pageDuplicationPropertyCursor(state.CopyCursor, rows), !hasMore, nil
}

func loadPageDuplicationDatabaseRows(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, *graphReferenceCache, string, bool, error) {
	rows := []pageDuplicationSourceDatabaseRow{}
	// Oldest first, one database at a time (page_duplication_row_order.go).
	params := pageDuplicationDatabaseWindowParams(job, state)
	after, order := parsePageDuplicationRowCursor(state.CopyCursor).window(params)
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]],[[self]].[[target_id]],[[source]].[[data_source_id]],
			[[dataSource]].[[target_id]] AS [[target_data_source_id]],
			CASE WHEN ` + databaseRowNotTrashedSQL("[[source]].[[subitem_parent_id]]") + `
				THEN [[source]].[[subitem_parent_id]] ELSE '' END AS [[subitem_parent_id]],
			COALESCE([[parent]].[[target_id]],'') AS [[target_parent_id]],
			[[source]].[[position]],[[source]].[[created]]
		FROM {{leafen_database_rows}} [[source]]
		JOIN {{leafen_page_duplication_identity_map}} [[self]]
			ON [[self]].[[job_id]]={:jobId} AND [[self]].[[kind]]='row'
			AND [[self]].[[source_id]]=[[source]].[[id]]
		JOIN {{leafen_page_duplication_identity_map}} [[dataSource]]
			ON [[dataSource]].[[job_id]]={:jobId} AND [[dataSource]].[[kind]]='data_source'
			AND [[dataSource]].[[source_id]]=[[source]].[[data_source_id]]
		LEFT JOIN {{leafen_page_duplication_identity_map}} [[parent]]
			ON [[parent]].[[job_id]]={:jobId} AND [[parent]].[[kind]]='row'
			AND [[parent]].[[source_id]]=[[source]].[[subitem_parent_id]]
		WHERE [[source]].[[workspace_id]]={:workspaceId} AND [[source]].[[owner]]={:ownerId}
			AND [[source]].[[page_id]]={:pageId} AND ` + after + `
		ORDER BY ` + order + ` LIMIT {:limit}
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	hasMore := trimPageDuplicationWindow(&rows)
	rowIDs := make([]string, len(rows))
	for index := range rows {
		rowIDs[index] = rows[index].ID
		if rows[index].SubitemParentID != "" && rows[index].TargetParentID == "" {
			// One whose item its maker can't open comes on its own (page_duplication_guest.go).
			if left, err := pageDuplicationParentLeftOut(app, job, rows[index].SubitemParentID); err != nil || !left {
				return nil, nil, state.CopyCursor, false, errors.Join(errPageDuplicationConflict, err)
			}
		}
	}
	cells, err := loadPageDuplicationDatabaseCells(app, job, state.SourcePageID, rowIDs)
	if err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	if cells, err = withoutHiddenPageDuplicationCells(app, job.ActorID, rows, cells); err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	values, err := remapPageDuplicationDatabaseCells(app, job.ID, cells, job.crossWorkspace())
	if err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	operations := make([]graphOperation, len(rows))
	cache := &graphReferenceCache{references: map[string]graphEntityReference{}, completeIDs: map[string]struct{}{}}
	for index, row := range rows {
		var parentID *string
		if row.TargetParentID != "" {
			parent := row.TargetParentID
			parentID = &parent
		}
		operations[index] = graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: row.TargetID, DataSourceID: row.TargetDataSourceID,
			SubitemParentID: parentID, Position: row.Position, Values: values[row.ID],
		}}
		cache.completeIDs[row.TargetID] = struct{}{}
	}
	return operations, cache, pageDuplicationRowCursor(state.CopyCursor, rows), !hasMore, nil
}

func loadPageDuplicationDatabaseCells(
	app core.App,
	job storedPageDuplicationJob,
	pageID string,
	rowIDs []string,
) ([]pageDuplicationSourceDatabaseCell, error) {
	if len(rowIDs) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	cells := []pageDuplicationSourceDatabaseCell{}
	err = app.DB().NewQuery(`
		SELECT [[row_id]],[[property_id]],[[type]],[[value]],[[spans]]
		FROM {{leafen_database_row_values}}
		WHERE [[workspace_id]]={:workspaceId} AND [[owner]]={:ownerId} AND [[page_id]]={:pageId}
			AND [[row_id]] IN (SELECT [[value]] FROM json_each({:sourceIds}))
		ORDER BY [[row_id]],[[property_id]]
	`).Bind(dbx.Params{
		"workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(),
		"pageId": pageID, "sourceIds": string(encoded),
	}).All(&cells)
	return cells, err
}

func pageDuplicationDatabaseWindowParams(
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) dbx.Params {
	return dbx.Params{
		"jobId": job.ID, "workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(),
		"pageId": state.SourcePageID, "cursor": state.CopyCursor,
		"limit": maxPageDuplicationItemsPerTurn + 1,
	}
}

func trimPageDuplicationWindow[T any](rows *[]T) bool {
	hasMore := len(*rows) > maxPageDuplicationItemsPerTurn
	if hasMore {
		*rows = (*rows)[:maxPageDuplicationItemsPerTurn]
	}
	return hasMore
}

func pageDuplicationDataSourceCursor(current string, rows []pageDuplicationSourceDataSource) string {
	if len(rows) == 0 {
		return current
	}
	return rows[len(rows)-1].ID
}

func pageDuplicationViewCursor(current string, rows []pageDuplicationSourceDatabaseView) string {
	if len(rows) == 0 {
		return current
	}
	return rows[len(rows)-1].ID
}

func pageDuplicationPropertyCursor(current string, rows []pageDuplicationSourceDatabaseProperty) string {
	if len(rows) == 0 {
		return current
	}
	return rows[len(rows)-1].ID
}
