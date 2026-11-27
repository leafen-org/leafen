package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A database page's formulas and rollups read through a page read (fetch,
// leafen_fetch) as values: the same values notion-query-data-sources' rows
// give them (projectMCPDataSourceRows), worked out for the agent reading.
// Notion's MCP names them by formulaResult:// and rollupResult:// addresses
// its fetch doesn't open (seen 2026-09-30); an agent read such an address
// and found no value to read (user 2026-11-20), so Leafen gives the value.

// mcpFetchedComputedValues are a database page's formula and rollup values
// for the agent, by property ID, its reading of the pages they name and the
// related rows they read as its queries' (mcpQueryMentions). None when they
// read more related rows than a page read on its own works out
// (databaseRowDependencyClosureRows), nor when its query can't work them
// out (more properties than a query reads): rows mode gives those, and the
// page's other values read as before. A template isn't a row its data
// source's queries read, and has none either.
func mcpFetchedComputedValues(
	app core.App,
	authority mcpAuthority,
	row databaseRowPageAuthority,
	stored []storedNotionDatabaseRowPageProperty,
) map[string]databaseComputedValue {
	if row.Template {
		return map[string]databaseComputedValue{}
	}
	computed, beyond, err := loadDatabaseRowPageComputedValuesFor(
		app, authority.PrincipalID, mcpQueryMentions(authority), row, stored, nil, authority.clock(),
	)
	if err != nil {
		app.Logger().Warn("leafen could not work out a database page's formulas and rollups for a page read",
			"page", row.RowID, "error", err)
	}
	if err != nil || beyond {
		return map[string]databaseComputedValue{}
	}
	return computed
}

// mcpFetchedComputedValue is a formula's or a rollup's value as a page read
// gives it: as rows mode gives it (databaseQueryRowValue), nil for one with
// no value, which a page read leaves out as it leaves out any empty value.
func mcpFetchedComputedValue(computed map[string]databaseComputedValue, propertyID string) any {
	value, found := computed[propertyID]
	if !found {
		return nil
	}
	return databaseQueryRawValue(value.Value)
}

// mcpFetchedDatabasePage is the database page a page record is, as a page
// read reads it: a template as its editor opens it, as a row page, and a
// page whose data source is in the trash as any.
func mcpFetchedDatabasePage(app core.App, authority mcpAuthority, record *core.Record) (databaseRowPageAuthority, error) {
	if isDatabaseTemplatePageRecord(record) {
		return authorizeDatabaseTemplatePage(app, authority.PrincipalID, record.Id, pageAccessRoleView)
	}
	return readDatabaseRowPageAuthorityWhere(app, record.Id, "TRUE")
}

// mcpFetchedPageProperties are a page's values as leafen_fetch gives them:
// a database page's by column, as notion-fetch's <properties> holds them
// (mcpDatabasePageFetchProperties) without the url the answer gives beside
// them; none for an ordinary page, whose one property is its title.
func mcpFetchedPageProperties(app core.App, authority mcpAuthority, pageID string) (map[string]any, error) {
	record, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil || !isDatabaseRowPageRecord(record) {
		return nil, err
	}
	row, err := mcpFetchedDatabasePage(app, authority, record)
	if err != nil {
		return nil, err
	}
	properties, err := mcpDatabasePageFetchProperties(app, authority, row)
	delete(properties, "url")
	return properties, err
}
