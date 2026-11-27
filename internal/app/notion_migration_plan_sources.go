package app

import (
	"slices"
	"strings"
)

// What a run does with the properties of a database's data sources after
// its first, which the import stages under each source's own key
// (notion_import_data_sources.go): the plan's changes to them are steps of
// their own after the database's, and the run's report lists them as it
// lists a database's, under the database's and the data source's names.

// notionMigrationDatabaseChanges are a planned database's property changes
// and reported properties, as restructure steps: its own properties', then
// each other data source's.

func notionMigrationDatabaseChanges(
	plan notionMigrationPlan, databases map[string]notionMigrationDatabase, planned notionMigrationPlanDatabase,
	source notionMigrationDatabase,
) (steps, reports []notionMigrationStep) {
	add := func(sourceID string, properties []notionMigrationProperty, entries []notionMigrationPlanProperty, merged bool) {
		change, reported := notionMigrationPropertyChanges(plan, databases, planned, sourceID, properties, entries, merged)
		if len(change.Properties) > 0 {
			steps = append(steps, notionMigrationStep{Kind: notionMigrationStepProperties, Database: change})
		}
		reports = append(reports, reported...)
	}
	add(source.ID, source.Properties, planned.Properties, planned.Action == "merge")
	scanned := map[string]notionMigrationDataSource{}
	for _, dataSource := range source.DataSources {
		scanned[notionMigrationSourceKey(dataSource.ID)] = dataSource
	}
	for _, entry := range planned.DataSources {
		// The scan's own IDs name what the import staged.
		if dataSource, found := scanned[notionMigrationSourceKey(entry.SourceID)]; found {
			add(notionImportDataSourceKey(source.ID, dataSource.ID), dataSource.Properties, entry.Properties, false)
		}
	}
	return steps, reports
}

// notionMigrationPropertyChanges is what the plan changes of the properties
// staged under sourceID: names, types and option names, and the properties
// it reports that the engine brought. A merged database's column that is
// its target's takes the target's changes.
func notionMigrationPropertyChanges(
	plan notionMigrationPlan, databases map[string]notionMigrationDatabase, planned notionMigrationPlanDatabase,
	sourceID string, scanned []notionMigrationProperty, entries []notionMigrationPlanProperty, merged bool,
) (notionMigrationDatabaseChange, []notionMigrationStep) {
	properties := map[string]notionMigrationProperty{}
	for _, property := range scanned {
		properties[notionMigrationPropertyKey(property.ID)] = property
	}
	change, reports := notionMigrationDatabaseChange{SourceID: sourceID}, []notionMigrationStep{}
	for _, entry := range entries {
		property, found := properties[notionMigrationPropertyKey(entry.SourceID)]
		// The engine reports a property type Leafen has no property for.
		if found && entry.Action == "report" && property.LeafenType != "" {
			reports = append(reports, notionMigrationStep{
				Kind: notionMigrationStepReport, Database: notionMigrationDatabaseChange{SourceID: sourceID}, SourceID: property.ID,
			})
		}
		// A relation gathered into another takes that one's changes.
		if !found || (entry.Action != "map" && entry.Action != "preserve") || strings.TrimSpace(entry.Into) != "" {
			continue
		}
		if merged && notionMergeAliasedEntry(plan, databases, planned, entry) {
			continue
		}
		next := notionMigrationPropertyChange{SourceID: property.ID, Name: strings.TrimSpace(entry.Name), Type: strings.TrimSpace(entry.Type)}
		if entry.Action == "preserve" {
			next.Type = graphValueTypeRichText
		}
		if next.Name == property.Name {
			next.Name = ""
		}
		if next.Type == property.LeafenType {
			next.Type = ""
		}
		for _, value := range entry.Values {
			if next.Values == nil {
				next.Values = map[string]string{}
			}
			if to := strings.TrimSpace(value.To); to != value.From {
				next.Values[value.From] = to
			}
		}
		if next.Name != "" || next.Type != "" || len(next.Values) > 0 {
			change.Properties = append(change.Properties, next)
		}
	}
	return change, reports
}

// notionMigrationReportSources gives the run's report the scan's databases
// and properties by key, and the plan's databases followed, for each that
// comes, by each of its other data sources as an entry of its own.
func notionMigrationReportSources(summary notionMigrationSummary, plan notionMigrationPlan) (
	map[string]notionMigrationDatabase, map[string]notionMigrationProperty, []notionMigrationPlanDatabase,
) {
	databases, properties := map[string]notionMigrationDatabase{}, map[string]notionMigrationProperty{}
	add := func(database notionMigrationDatabase) {
		key := notionMigrationSourceKey(database.ID)
		databases[key] = database
		for _, property := range database.Properties {
			properties[key+"/"+notionMigrationPropertyKey(property.ID)] = property
		}
	}
	for _, database := range summary.Databases {
		add(database)
		for _, source := range database.DataSources {
			title := source.Name
			if strings.TrimSpace(database.Title) != "" {
				title = database.Title + " · " + source.Name
			}
			add(notionMigrationDatabase{ID: notionImportDataSourceKey(database.ID, source.ID), Title: title, Properties: source.Properties})
		}
	}
	planned := slices.Clone(plan.Databases)
	for _, database := range plan.Databases {
		if database.Action != "import" && database.Action != "merge" {
			continue
		}
		for _, source := range database.DataSources {
			planned = append(planned, notionMigrationPlanDatabase{
				SourceID: notionImportDataSourceKey(database.SourceID, source.SourceID), Action: "import", Properties: source.Properties,
			})
		}
	}
	return databases, properties, planned
}
