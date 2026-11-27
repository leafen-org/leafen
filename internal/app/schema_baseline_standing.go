package app

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// validateSchemaBaseline is the baseline's own check, as every migration
// has one: it runs as the baseline is applied, and again at each start whose
// schema or migrations changed (schema_migrations.go), so it holds the later
// migrations to the schema the baseline made. Every table, view, index and
// trigger the baseline creates is there, each table with at least the
// columns the baseline gives it, and the instance product export tracks
// exactly the tables its policy carries. One a later migration took out or
// reshaped on purpose is listed in schemaBaselineRetired with that
// migration's version, and its check stands down once that migration is
// recorded, as earlier checks did for later migrations before the baseline.
// The rows the baseline seeded aren't checked again: they are data, which
// people change.
func validateSchemaBaseline(app core.App) error {
	expected, err := readSchemaBaselineObjects()
	if err != nil {
		return err
	}
	held := map[string]bool{}
	objects := []struct {
		Type string `db:"type"`
		Name string `db:"name"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[type]], [[name]] FROM [[sqlite_master]]`).All(&objects); err != nil {
		return err
	}
	for _, object := range objects {
		held[object.Type+" "+object.Name] = true
	}
	columns := []struct {
		Table  string `db:"table_name"`
		Column string `db:"column_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[m]].[[name]] AS [[table_name]], [[p]].[[name]] AS [[column_name]]
		FROM [[sqlite_master]] [[m]], pragma_table_info([[m]].[[name]]) [[p]]
		WHERE [[m]].[[type]] = 'table'
	`).All(&columns); err != nil {
		return err
	}
	heldColumns := map[string]bool{}
	for _, column := range columns {
		heldColumns[column.Table+"."+column.Column] = true
	}
	for _, object := range expected {
		if retired, err := schemaBaselineObjectRetired(app, object.name); err != nil {
			return err
		} else if retired {
			continue
		}
		if !held[object.kind+" "+object.name] {
			return fmt.Errorf("schema baseline: %s %s is missing", object.kind, object.name)
		}
		for _, column := range object.columns {
			if retired, err := schemaBaselineObjectRetired(app, object.name+"."+column); err != nil {
				return err
			} else if retired {
				continue
			}
			if !heldColumns[object.name+"."+column] {
				return fmt.Errorf("schema baseline: table %s has no column %s", object.name, column)
			}
		}
	}
	return validateInstanceProductExportSourceRevisionSchema(app, instanceProductExportTrackedTableNames(app), true)
}

// schemaBaselineRetired names what of the baseline a later migration took
// out or reshaped on purpose — a table, index, trigger or view, or a
// table's column as "table.column" — each with that migration's version and
// name; what 835 took of areas is retiredAreaBaselineObjects.
var schemaBaselineRetired = map[string]struct {
	version int
	name    string
}{
	// 832: the list an earlier wiki made of its pages (wiki_earlier_listing.go),
	// with its indexes and triggers.
	"leafen_wiki_pages":               earlierWikiListingRetired,
	"idx_leafen_wiki_pages_root":      earlierWikiListingRetired,
	"idx_leafen_wiki_pages_root_page": earlierWikiListingRetired,
	"leafen_instance_product_export_revision_leafen_wiki_pages_insert": earlierWikiListingRetired,
	"leafen_instance_product_export_revision_leafen_wiki_pages_update": earlierWikiListingRetired,
	"leafen_instance_product_export_revision_leafen_wiki_pages_delete": earlierWikiListingRetired,
	"leafen_bootstrap_authority_revision_leafen_wiki_pages_INSERT":     earlierWikiListingRetired,
	"leafen_bootstrap_authority_revision_leafen_wiki_pages_UPDATE":     earlierWikiListingRetired,
	"leafen_bootstrap_authority_revision_leafen_wiki_pages_DELETE":     earlierWikiListingRetired,
	"leafen_page_list_changes_wiki_page_insert":                        earlierWikiListingRetired,
	"leafen_page_list_changes_wiki_page_update":                        earlierWikiListingRetired,
	"leafen_page_list_changes_wiki_page_delete":                        earlierWikiListingRetired,
	// 833: where the feed's walk of the audit log's earlier rows stood
	// (workspace_activity_backfill_retired.go).
	"leafen_workspace_activity_backfill": {833, "native_retire_workspace_activity_backfill"},
	// 839: the setting that took only the agent's program's own form answer
	// for an organization's app tools (app_agent_form_answers_retired.go).
	"leafen_app_agent_form_answers": {839, "native_retire_app_agent_form_answers"},
	// 859: the triggers that count a page whole, which count every counted
	// table since (page_export_count_whole.go).
	"leafen_workspace_export_count_pages_INSERT": {nativePageExportCountWholeVersion, nativePageExportCountWholeName},
	"leafen_workspace_export_count_pages_UPDATE": {nativePageExportCountWholeVersion, nativePageExportCountWholeName},
	"leafen_page_kind_export_arrive":             {nativePageExportCountWholeVersion, nativePageExportCountWholeName},
	// 860: a page's trash marks its workspace's guests to be read again
	// whatever page holds the share (people_guest_trash_checks.go).
	peopleGuestPageTrashTrigger: {nativePeopleGuestTrashChecksVersion, nativePeopleGuestTrashChecksName},
}

var earlierWikiListingRetired = struct {
	version int
	name    string
}{832, "native_retire_earlier_wiki_listing"}

func schemaBaselineObjectRetired(app core.App, object string) (bool, error) {
	retiredBy, listed := schemaBaselineRetired[object]
	if !listed && retiredAreaBaselineObjects[object] {
		retiredBy.version, retiredBy.name, listed = nativeRetireAreasVersion, nativeRetireAreasName, true
	}
	if !listed {
		return false, nil
	}
	return productMigrationRecorded(app, retiredBy.version, retiredBy.name)
}

type schemaBaselineObject struct {
	kind    string // table, view, index or trigger, as sqlite_master names them
	name    string
	columns []string
}

var schemaBaselineObjectName = regexp.MustCompile("^CREATE (?:UNIQUE |VIRTUAL )?(TABLE|INDEX|TRIGGER|VIEW) (?:`((?:[^`]|``)+)`|\"((?:[^\"]|\"\")+)\")")

// readSchemaBaselineObjects reads the objects the baseline creates, and the
// columns of each of its ordinary tables as SQLite reads them from the
// baseline's own statements, once.
var readSchemaBaselineObjects = sync.OnceValues(func() ([]schemaBaselineObject, error) {
	baseline, err := readSchemaBaseline()
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		return nil, err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	objects := []schemaBaselineObject{}
	for _, statement := range baseline.Create {
		match := schemaBaselineObjectName.FindStringSubmatch(statement)
		if match == nil {
			return nil, fmt.Errorf("schema baseline: can't read the name of %s", strings.SplitN(statement, "\n", 2)[0])
		}
		object := schemaBaselineObject{kind: strings.ToLower(match[1]), name: strings.ReplaceAll(match[2], "``", "`")}
		if match[3] != "" {
			object.name = strings.ReplaceAll(match[3], `""`, `"`)
		}
		if object.kind == "table" && !strings.HasPrefix(statement, "CREATE VIRTUAL TABLE") {
			if _, err := database.Exec(statement); err != nil {
				return nil, fmt.Errorf("schema baseline: table %s: %w", object.name, err)
			}
			rows, err := database.Query(`SELECT name FROM pragma_table_info(?)`, object.name)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var column string
				if err := rows.Scan(&column); err != nil {
					rows.Close()
					return nil, err
				}
				object.columns = append(object.columns, column)
			}
			if err := rows.Close(); err != nil {
				return nil, err
			}
		}
		objects = append(objects, object)
	}
	return objects, nil
})
