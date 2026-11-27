package app

import (
	"context"
	"slices"
	"strings"
)

// A database of several data sources holds properties for each, as Notion
// shows them: a view shows one data source, and its properties are that
// source's (2026-11-15, a database of two in Notion's view settings, under
// "데이터 소스 설정"). The import brings each as a data source of one
// database (notion_import_data_sources.go): the first's properties are the
// database's own, each other's are staged under its key. The scan keeps the
// first's name beside the database's properties and each other data source
// with its own, read once its database is, so a plan maps each one's
// properties as the run brings them.

// notionMigrationScanProperties lists an object's properties as a plan reads
// them, the title first, and notes each Leafen can't bring as it is, about
// the database or data source about names: each loss the import itself finds
// in the property (notionImportPropertyPlanFor) — a type Leafen has none for,
// a formula naming a property that is gone or a function Leafen can't run, a
// setting it leaves out (a button's action, a rollup's calculation, a number
// format). Who made and changed rows the agenda says with the people
// (notion_migration_agenda.go).
func notionMigrationScanProperties(
	change *notionMigrationScanChange, object notionProviderObject, about notionMigrationIssue,
) []notionMigrationProperty {
	properties := []notionMigrationProperty{}
	for key, property := range object.Properties {
		properties = append(properties, notionMigrationPropertyOf(key, property))
		issue := about
		issue.PropertyID, issue.Detail = property.ID, property.Type
		_, reasons, supported := notionImportPropertyPlanFor(object, key, property)
		if !supported {
			reasons = []string{notionImportIssuePropertyUnsupported}
		}
		for _, reason := range reasons {
			if reason != notionImportIssuePropertyValueRecomputed {
				issue.Code = reason
				change.issues = append(change.issues, issue)
			}
		}
		// Shown by property names, as the formula becomes in Leafen.
		if expression, ok := notionImportFormulaExpression(notionMigrationFormulaExpression(property), object); ok && property.Type == "formula" {
			properties[len(properties)-1].Expression = expression
		}
	}
	sortNotionMigrationProperties(properties)
	return properties
}

// notionMigrationScanDataSources are the data sources after a database's
// first, in Notion's order, by the names the database lists, their
// properties still to read.
func notionMigrationScanDataSources(others []notionProviderOtherDataSource) []notionMigrationDataSource {
	sources := make([]notionMigrationDataSource, 0, len(others))
	for _, other := range others {
		sources = append(sources, notionMigrationDataSource{ID: other.ID, Name: other.Name, Properties: []notionMigrationProperty{}})
	}
	return sources
}

// scanNotionMigrationSource reads the properties of a data source after its
// database's first, named as Notion names it. One Notion won't give is left
// out, as the run leaves it out, and said.
func scanNotionMigrationSource(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	entry, found, err := store.database(item.ID)
	if err != nil || !found {
		return err
	}
	index := slices.IndexFunc(entry.DataSources, func(source notionMigrationDataSource) bool { return source.ID == item.Source })
	if index < 0 {
		return nil
	}
	about := notionMigrationIssue{
		SourceID: notionImportDataSourceKey(entry.ID, item.Source), ParentID: entry.ID, SourceTitle: entry.DataSources[index].Name,
	}
	source, err := client.dataSource(ctx, item.Source)
	if notionProviderRefused(err) {
		about.Code = notionImportIssueDataSourceUnavailable
		change.issues = append(change.issues, about)
		entry.DataSources = slices.Delete(entry.DataSources, index, index+1)
		change.updated = &entry
		return nil
	}
	if err != nil {
		return err
	}
	if title := notionObjectTitle(source); strings.TrimSpace(title) != "" {
		entry.DataSources[index].Name, about.SourceTitle = title, title
	}
	entry.DataSources[index].Properties = notionMigrationScanProperties(change, source, about)
	change.updated = &entry
	return nil
}
