package app

import (
	"context"
	"net/http"
	"net/url"
)

// A database's views and templates are read once its rows are queued: how
// many views Notion gives, and its data sources' templates by name, the
// default marked, as the run makes them the database's templates
// (notion_import_templates.go), not rows. The scan names them so the plan
// talks them over, a template being a page Notion keeps apart from the rows.

func scanNotionMigrationViews(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	entry, found, err := store.database(item.ID)
	if err != nil || !found {
		return err
	}
	views, err := client.databaseViews(ctx, item.ID, "")
	if err != nil {
		if notionProviderRefused(err) {
			change.issues = append(change.issues, notionMigrationIssue{Code: notionImportIssueViewsUnavailable, SourceID: item.ID})
			return nil
		}
		return err
	}
	entry.Views = len(views.IDs)
	if entry.Templates, err = notionMigrationScanTemplates(ctx, client, item.ID); err != nil {
		return err
	}
	change.updated = &entry
	return nil
}

// notionMigrationScanTemplates lists a database's templates, each of its
// data sources' in turn; none where Notion gives none.
func notionMigrationScanTemplates(ctx context.Context, client notionProviderClient, databaseID string) ([]notionMigrationTemplate, error) {
	listed := struct {
		DataSources []notionProviderOtherDataSource `json:"data_sources"`
	}{}
	err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &listed,
	)
	if notionProviderRefused(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found := []notionMigrationTemplate{}
	for _, source := range listed.DataSources {
		templates, err := client.dataSourceTemplates(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		for _, template := range templates {
			found = append(found, notionMigrationTemplate{Name: template.Name, IsDefault: template.IsDefault})
		}
	}
	return found, nil
}
