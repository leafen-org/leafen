package app

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A property made in a database stays out of the database's tables whose
// shown properties were set, but for the table it was made from, wherever
// they are, as Notion's (seen 2026-11-04: "오른쪽에 삽입" in one table, not in
// the next; a linked table on another page hides it too; a table never
// touched shows it). The page whose screen made it sets its own tables
// (nativeTableProductAdapter otherTablesWithout), a program's change sets
// the source page's (databaseNewPropertiesHiddenInTables). The tables on other
// pages are each that page's, so the relation pair runner sets them after the
// change, as it writes a two-way relation's other side. A table that already
// names the property — the one it was made from, or one its screen hid it in
// — is left as it is: made from a linked table on another page, that page's
// tables name it before the source has it, which also tells the change that
// the source page's own tables are its to set. A new Verification's Owner,
// which the server makes beside it, goes where the table has the
// Verification: right after it, or hidden with it.

const databaseRelationPairHideNew = "hide-new"

// databaseTableKeepsItsShownProperties tells a table that hides some property,
// or lists the ones it shows, from one never touched, which shows every
// property its source has, as Notion's (seen 2026-11-04, a linked table on
// another page too), and from one whose columns were only moved, sized or
// wrapped, which shows a new one after its own by name (seen 2026-11-12).
func databaseTableKeepsItsShownProperties(config map[string]any) bool {
	if _, lists := config["visibleProperties"]; lists {
		return true
	}
	hidden, _ := config["hiddenProperties"].([]any)
	return len(hidden) > 0
}

// databaseTableNamesProperty tells a table whose settings already say where
// the property shows, or that it hides.
func databaseTableNamesProperty(config map[string]any, propertyID string) bool {
	for _, key := range []string{"hiddenProperties", "visibleProperties", "propertyOrder"} {
		if listed, ok := config[key].([]any); ok && slices.Contains(listed, any(propertyID)) {
			return true
		}
	}
	return false
}

// databaseNewPropertiesHiddenInTables keeps properties made outside a view (by
// a program, or a relation's other side) out of the page's tables of the
// source whose shown properties were set, as Notion's (seen 2026-11-02,
// 2026-11-03, 2026-11-04).
func databaseNewPropertiesHiddenInTables(app core.App, pageID, dataSourceID string, made []string) ([]graphOperation, error) {
	return databaseNewPropertiesSettledInTables(app, pageID, dataSourceID, made, nil)
}

// databaseMadeVerificationOwners pairs each new Verification among the made
// properties with the Owner made with it, which its Verification property
// names.
func databaseMadeVerificationOwners(made []string, typeAndConfig func(id string) (string, map[string]any)) []verificationSourceLink {
	links := []verificationSourceLink{}
	for _, id := range made {
		kind, config := typeAndConfig(id)
		if owner, _ := config["ownerPropertyId"].(string); kind == graphValueTypeVerification && owner != "" && slices.Contains(made, owner) {
			links = append(links, verificationSourceLink{verification: id, owner: owner})
		}
	}
	return links
}

// databaseNewPropertiesSettledInTables keeps the made properties out of the
// page's tables of the source whose shown properties were set, but for those
// a table names already. A table that names a new Verification and not the
// Owner made with it takes the Owner where it has the Verification: right
// after it, or hidden with it (showVerificationOwners), as Notion's table
// puts its Owner column there (seen 2026-11-03).
func databaseNewPropertiesSettledInTables(
	app core.App, pageID, dataSourceID string, made []string, owners []verificationSourceLink,
) ([]graphOperation, error) {
	if len(made) == 0 {
		return nil, nil
	}
	views, err := loadDatabaseViewRecords(app, pageID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(views))
	for id := range views {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	operations := []graphOperation{}
	for _, id := range ids {
		view := views[id]
		if view.Type != graphViewTypeTable || view.DataSourceID != dataSourceID || !databaseTableKeepsItsShownProperties(view.Config) {
			continue
		}
		config := map[string]any{}
		for key, value := range view.Config {
			config[key] = value
		}
		placed := []verificationSourceLink{}
		for _, link := range owners {
			if databaseTableNamesProperty(view.Config, link.verification) && !databaseTableNamesProperty(view.Config, link.owner) {
				placed = append(placed, link)
			}
		}
		hidden, _ := config["hiddenProperties"].([]any)
		hides := false
		for _, property := range made {
			if databaseTableNamesProperty(view.Config, property) || slices.ContainsFunc(placed, func(link verificationSourceLink) bool {
				return link.owner == property
			}) {
				continue
			}
			hidden, hides = append(hidden, property), true
		}
		if !hides && len(placed) == 0 {
			continue
		}
		if hides {
			config["hiddenProperties"] = hidden
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		settled := []graphOperation{{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: id, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: encoded,
		}}}
		showVerificationOwners(settled, placed)
		operations = append(operations, settled...)
	}
	return operations, nil
}

// planDatabaseNewPropertyHiding answers, for the properties a change to the
// page makes, the page's own tables to hide them in when they were made from
// a table on another page, and the work that hides them in the tables of the
// other pages.
func planDatabaseNewPropertyHiding(
	app core.App, ownerID, pageID string, operations []graphOperation,
) ([]graphOperation, []databaseRelationPairWork, error) {
	made := map[string][]string{}
	asked := []string{}
	written := map[string]struct{}{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			asked = append(asked, operation.Property.ID)
		case operation.Kind == graphOperationKindUpsertView && operation.View != nil:
			written[operation.View.ID] = struct{}{}
		}
	}
	if len(asked) == 0 {
		return operations, nil, nil
	}
	stored, err := loadDatabaseRelationPairProperties(app, ownerID, asked)
	if err != nil {
		return nil, nil, err
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil {
			continue
		}
		if _, exists := stored[operation.Property.ID]; !exists && !slices.Contains(made[operation.Property.DataSourceID], operation.Property.ID) {
			made[operation.Property.DataSourceID] = append(made[operation.Property.DataSourceID], operation.Property.ID)
		}
	}
	// A new Verification's Owner goes where the Verification goes.
	configs := map[string]*graphProperty{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil {
			configs[operation.Property.ID] = operation.Property
		}
	}
	typeAndConfig := func(id string) (string, map[string]any) {
		config := map[string]any{}
		property := configs[id]
		if property == nil {
			return "", config
		}
		if len(property.Config) > 0 {
			_ = json.Unmarshal(property.Config, &config)
		}
		return property.Type, config
	}
	work := []databaseRelationPairWork{}
	for _, dataSourceID := range workspaceExportSortedKeys(made) {
		properties := made[dataSourceID]
		tables := []struct {
			PageID string `db:"page_id"`
			Config string `db:"config"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[page_id]], [[config]] FROM {{leafen_database_views}}
			WHERE [[owner]]={:owner} AND [[data_source_id]]={:source} AND [[type]]={:table} AND [[page_id]]<>{:page}
			ORDER BY [[page_id]], [[id]]`).Bind(dbx.Params{
			"owner": ownerID, "source": dataSourceID, "table": graphViewTypeTable, "page": pageID,
		}).All(&tables); err != nil {
			return nil, nil, err
		}
		// A page whose table names one of them made them all, as a new
		// Verification its Owner: its screen set its own tables naming what it
		// knew, and the runner places with them what the server made beside
		// it, a Verification's Owner. Another page's tables hide them.
		hiding, madeOn := []string{}, map[string]bool{}
		for _, table := range tables {
			config, err := databaseRecordConfig(table.Config)
			if err != nil || !databaseTableKeepsItsShownProperties(config) {
				continue
			}
			for _, property := range properties {
				if databaseTableNamesProperty(config, property) {
					madeOn[table.PageID] = true
				}
			}
			if !slices.Contains(hiding, table.PageID) {
				hiding = append(hiding, table.PageID)
			}
		}
		for _, page := range hiding {
			work = append(work, databaseRelationPairWork{Kind: databaseRelationPairHideNew, Payload: databaseRelationPairPayload{
				TargetDataSourceID: dataSourceID, PageID: page, PropertyIDs: properties,
			}})
		}
		if len(madeOn) == 0 {
			continue
		}
		hidden, err := databaseNewPropertiesSettledInTables(app, pageID, dataSourceID, properties,
			databaseMadeVerificationOwners(properties, typeAndConfig))
		if err != nil {
			return nil, nil, err
		}
		for _, operation := range hidden {
			if _, own := written[operation.View.ID]; !own {
				operations = append(operations, operation)
			}
		}
	}
	return operations, work, nil
}

// databaseNewPropertiesHiddenOnPage is the runner's turn of that work: the
// page's tables of the source hide the properties the source still has, a
// new Verification's Owner going where its Verification is.
func databaseNewPropertiesHiddenOnPage(app core.App, ownerID string, payload databaseRelationPairPayload) ([]graphOperation, error) {
	stored, err := loadDatabaseRelationPairProperties(app, ownerID, payload.PropertyIDs)
	if err != nil {
		return nil, err
	}
	kept := []string{}
	for _, id := range payload.PropertyIDs {
		if property, exists := stored[id]; exists && property.DataSourceID == payload.TargetDataSourceID {
			kept = append(kept, id)
		}
	}
	owners := databaseMadeVerificationOwners(kept, func(id string) (string, map[string]any) {
		return stored[id].Type, stored[id].Config
	})
	return databaseNewPropertiesSettledInTables(app, payload.PageID, payload.TargetDataSourceID, kept, owners)
}
