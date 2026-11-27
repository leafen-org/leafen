package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Areas, a second way to divide who sees what beside Notion's own tools
// (teamspaces, groups, shares and what pages above give, a database's page
// rules and property access), were taken out before release (2026-11-21).
// Migration 835 takes away the tables they were kept in, the area an
// invitation and the invite link named, and the triggers that followed
// them, where no area was ever made or named: a server that holds any keeps
// them as they are, which nothing reads now, and says so once in its log.
// What the tables are kept or gone for is read from the schema
// (retiredAreaTableHeld), so the deletion inventories name them only while
// they are there.

const (
	nativeRetireAreasVersion = 835
	nativeRetireAreasName    = "native_retire_areas"
)

// retiredAreaTables are the tables areas were kept in.
var retiredAreaTables = []string{
	"leafen_area_sources", "leafen_areas", "leafen_area_members", "leafen_area_closure",
	"leafen_area_joins", "leafen_page_areas", "leafen_teamspace_areas", "leafen_area_personal",
}

// retiredAreaColumns are the columns another table named an area in.
var retiredAreaColumns = [][2]string{
	{"leafen_workspace_invitations", "area_id"},
	{"leafen_workspace_invite_links", "area_id"},
}

// retiredAreaTriggers are the triggers on other tables that followed areas:
// a page's area going with the page, and an area property's change of who
// sees what. Those on the area tables go with them.
var retiredAreaTriggers = []string{
	"leafen_page_areas_page_delete",
	"leafen_area_property_access_revision_insert",
	"leafen_area_property_access_revision_update",
	"leafen_area_property_access_revision_delete",
}

// retiredAreaTableHeld is whether a server kept a table areas were kept in.
func retiredAreaTableHeld(app core.App, table string) bool {
	for _, retired := range retiredAreaTables {
		if retired == table {
			return app.HasTable(table)
		}
	}
	return true
}

// heldAreaData names what of areas a server holds: rows in their tables,
// an invitation or invite link naming an area, a database property or value
// of the area kind. Empty, nothing.
func heldAreaData(app core.App) (string, error) {
	held := []string{}
	count := func(name, query string) error {
		var rows int
		if err := app.DB().NewQuery(query).Row(&rows); err != nil {
			return err
		}
		if rows > 0 {
			held = append(held, fmt.Sprintf("%s %d", name, rows))
		}
		return nil
	}
	for _, table := range retiredAreaTables {
		if !app.HasTable(table) {
			continue
		}
		if err := count(table, "SELECT COUNT(*) FROM {{"+table+"}}"); err != nil {
			return "", err
		}
	}
	for _, column := range retiredAreaColumns {
		has, err := tableColumnHeld(app, column[0], column[1])
		if err != nil {
			return "", err
		}
		if !has {
			continue
		}
		if err := count(column[0]+"."+column[1], "SELECT COUNT(*) FROM {{"+column[0]+"}} WHERE [["+column[1]+"]] != ''"); err != nil {
			return "", err
		}
	}
	if err := count("area properties", "SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[type]] = 'area'"); err != nil {
		return "", err
	}
	if err := count("area values", "SELECT COUNT(*) FROM {{leafen_database_row_values}} WHERE [[type]] = 'area'"); err != nil {
		return "", err
	}
	return strings.Join(held, ", "), nil
}

func tableColumnHeld(app core.App, table, column string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info({:table}) WHERE [[name]] = {:column}`).
		Bind(dbx.Params{"table": table, "column": column}).Row(&found)
	return found > 0, err
}

// retireAreas is migration 835.
func retireAreas(app core.App) error {
	held, err := heldAreaData(app)
	if err != nil {
		return err
	}
	if held != "" {
		app.Logger().Warn("leafen areas are kept as they were: this server holds some, which nothing reads now", "held", held)
		return nil
	}
	for _, trigger := range retiredAreaTriggers {
		if _, err := app.DB().NewQuery("DROP TRIGGER IF EXISTS [[" + trigger + "]]").Execute(); err != nil {
			return err
		}
	}
	for _, table := range retiredAreaTables {
		if err := dropRetiredTable(app, table); err != nil {
			return err
		}
	}
	for _, column := range retiredAreaColumns {
		has, err := tableColumnHeld(app, column[0], column[1])
		if err != nil {
			return err
		}
		if has {
			if _, err := app.DB().NewQuery("ALTER TABLE {{" + column[0] + "}} DROP COLUMN [[" + column[1] + "]]").Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateAreasRetired is its check: where the areas' tables are gone, so
// is everything else of theirs; a server that kept them keeps them whole.
func validateAreasRetired(app core.App) error {
	kept := 0
	for _, table := range retiredAreaTables {
		if app.HasTable(table) {
			kept++
		}
	}
	if kept == len(retiredAreaTables) {
		return nil
	}
	if kept != 0 {
		return fmt.Errorf("areas retired: %d of their %d tables are left", kept, len(retiredAreaTables))
	}
	for _, table := range retiredAreaTables {
		if err := validateRetiredTableDropped(app, table); err != nil {
			return err
		}
	}
	for _, trigger := range retiredAreaTriggers {
		var left int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[type]] = 'trigger' AND [[name]] = {:name}`).
			Bind(dbx.Params{"name": trigger}).Row(&left); err != nil {
			return err
		}
		if left != 0 {
			return fmt.Errorf("areas retired: the trigger %s is still there", trigger)
		}
	}
	for _, column := range retiredAreaColumns {
		if has, err := tableColumnHeld(app, column[0], column[1]); err != nil {
			return err
		} else if has {
			return fmt.Errorf("areas retired: %s.%s is still there", column[0], column[1])
		}
	}
	return nil
}

// retiredAreaBaselineObjects are what of the schema baseline migration 835
// takes away (schemaBaselineRetired): the areas' tables, their indexes and
// triggers, the triggers on other tables that followed them, and the
// columns that named an area, as "table.column".
var retiredAreaBaselineObjects = func() map[string]bool {
	objects := map[string]bool{
		"idx_leafen_areas_workspace": true, "idx_leafen_area_members_area": true,
		"idx_leafen_area_closure_descendant": true, "idx_leafen_area_closure_workspace": true,
		"idx_leafen_page_areas_workspace": true, "idx_leafen_teamspace_areas_workspace": true,
		"idx_leafen_area_personal_area": true,
	}
	events := []string{"insert", "update", "delete"}
	for _, table := range retiredAreaTables {
		objects[table] = true
		for _, event := range events {
			objects["leafen_instance_product_export_revision_"+table+"_"+event] = true
		}
	}
	for _, table := range []string{"leafen_page_areas", "leafen_teamspace_areas", "leafen_area_members", "leafen_area_closure"} {
		for _, event := range events {
			objects["leafen_area_access_revision_"+table+"_"+event] = true
		}
	}
	for _, trigger := range retiredAreaTriggers {
		objects[trigger] = true
	}
	for _, column := range retiredAreaColumns {
		objects[column[0]+"."+column[1]] = true
	}
	return objects
}()
