package app

import "github.com/pocketbase/dbx"

// A linked view names its source's data source and properties, which are on
// the source's page: its settings from the reading are saved with those
// allowed, as the import made the view (linkedViewPlace.references). Without
// them every linked view's settings were refused as another page's content
// and listed as read but not saved (2026-11-23, a synthetic Notion: the views
// linked in rows made from a template, read whole, came back not saved).

// linkedViewReferences are what the views a change saves may name on their
// sources' pages: each source on another page, and the properties of it the
// view names.
func (matcher *notionMigrationFormatsMatcher) linkedViewReferences(
	pageID string, operations []graphOperation,
) (notionGraphApplyReferences, error) {
	references := notionGraphApplyReferences{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertView || operation.View == nil {
			continue
		}
		view := operation.View
		sources, err := loadGraphReferences(matcher.app, "leafen_data_sources", map[string]struct{}{view.DataSourceID: {}})
		if err != nil {
			return notionGraphApplyReferences{}, err
		}
		source, found := sources[view.DataSourceID]
		if !found || source.PageID == pageID {
			continue
		}
		named, err := loadGraphReferences(matcher.app, "leafen_database_properties", linkedGraphViewPropertyIDs(view))
		if err != nil {
			return notionGraphApplyReferences{}, err
		}
		if references.sources == nil {
			references.sources, references.properties = map[string]graphEntityReference{}, map[string]graphEntityReference{}
		}
		references.sources[view.DataSourceID] = source
		for id, property := range named {
			if property.DataSourceID == view.DataSourceID && property.PageID == source.PageID {
				references.properties[id] = property
			}
		}
	}
	return references, nil
}

// viewDatabase is the Notion database whose properties a view's settings
// name: a linked view's source, by the data source the view shows, where
// the reading names the view by the block showing it; else that database.
func (matcher *notionMigrationFormatsMatcher) viewDatabase(databaseID, dataSourceID string) string {
	var source string
	if err := matcher.app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='data_source' AND [[native_id]]={:dataSource} LIMIT 1
	`).Bind(dbx.Params{"jobId": matcher.job.ID, "dataSource": dataSourceID}).Row(&source); err != nil || source == "" {
		return databaseID
	}
	return source
}
