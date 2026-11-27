package app

import (
	"slices"
	"sort"
)

// What a migration plan may do with each scanned property: the actions its
// check takes for that property and, for "map", the Leafen types it may
// become. The scan's databases list them on each property (`actions`,
// `types`), so the agent and the person weigh only choices that run, and the
// plan's check refuses exactly the others (notion_migration_plan_check.go
// asks the same functions).

// notionMigrationShownAsText are the computed property types whose values
// "preserve" keeps as text as Notion showed them when the run read them
// (notion_import_row_shown.go): Leafen can't always compute them again as
// Notion did, as a formula naming a property that is gone.
var notionMigrationShownAsText = map[string]bool{graphValueTypeFormula: true, graphValueTypeRollup: true}

// notionMigrationPreservable says whether a property of a Leafen type can be
// kept as text: a stored value but a file, or a computed one as shown.
func notionMigrationPreservable(leafenType string) bool {
	return notionMigrationShownAsText[leafenType] || notionMigrationTypeConvertible(leafenType, graphValueTypeRichText)
}

// notionMigrationLeafenTypes are the Leafen types a Notion property can be,
// in a steady order.
var notionMigrationLeafenTypes = func() []string {
	types := []string{}
	for _, kind := range notionDataSourceRequestTypes {
		if !slices.Contains(types, kind) {
			types = append(types, kind)
		}
	}
	sort.Strings(types)
	return types
}()

// withNotionMigrationPropertyChoices gives a scanned property the actions
// and map types the plan's check takes for it. A property of a type Leafen
// has none for is only reported; a title stays the title.
func withNotionMigrationPropertyChoices(property notionMigrationProperty) notionMigrationProperty {
	property.Actions, property.Types = []string{}, []string{}
	if property.LeafenType != "" {
		property.Actions = append(property.Actions, "map")
		for _, kind := range notionMigrationLeafenTypes {
			if notionMigrationTypeConvertible(property.LeafenType, kind) {
				property.Types = append(property.Types, kind)
			}
		}
		if property.LeafenType == graphValueTypeTitle {
			return property
		}
		if notionMigrationPreservable(property.LeafenType) {
			property.Actions = append(property.Actions, "preserve")
		}
	}
	property.Actions = append(property.Actions, "report")
	if notionMigrationObjectTypes[property.LeafenType] {
		property.Actions = append(property.Actions, "object")
	}
	return property
}

// withNotionMigrationDatabaseChoices gives each property of a scanned
// database, and of each of its other data sources, its choices.
func withNotionMigrationDatabaseChoices(database notionMigrationDatabase) notionMigrationDatabase {
	properties := make([]notionMigrationProperty, 0, len(database.Properties))
	for _, property := range database.Properties {
		properties = append(properties, withNotionMigrationPropertyChoices(property))
	}
	database.Properties = properties
	sources := make([]notionMigrationDataSource, 0, len(database.DataSources))
	for _, source := range database.DataSources {
		listed := make([]notionMigrationProperty, 0, len(source.Properties))
		for _, property := range source.Properties {
			listed = append(listed, withNotionMigrationPropertyChoices(property))
		}
		source.Properties = listed
		sources = append(sources, source)
	}
	if database.DataSources != nil {
		database.DataSources = sources
	}
	return database
}
