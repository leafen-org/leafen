package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A column beside a relation (notion_migration_split.go) is named after the
// column it stands beside and the data source its rows went to, "<column> ·
// <data source>". The column is the relation's own, or for a relation the
// plan gathers into another, the role's (notion_migration_roles.go), so each
// relation of a role names it alike, whichever of their rows makes it first
// (2026-11-24: it took the name of the gathered relation whose row made it
// first, so the same plan named it apart by the order of its rows). A run
// gives a column made before the name it gives now while it has a name a run
// gave it, as a column a run made elsewhere for a property does
// (notion_migration_rerun_column_names.go).

// siblingRelationName is the name a run gives a column beside a relation
// going to target, beside column.
func (linker *notionImportLinker) siblingRelationName(column, target string) (string, error) {
	title, err := notionBesideTitle(linker.app, target)
	if err != nil {
		return "", err
	}
	names := notionMigrationColumnNames{app: linker.app, jobID: linker.job.ID, migrationID: linker.payload.MigrationID, run: linker.run}
	name, err := names.besideColumn(column)
	return notionBesideName(name, title), err
}

// notionBesideName names a column beside column relating to the data
// source title names.
func notionBesideName(column, title string) string {
	return strings.TrimSpace(column + " · " + title)
}

// besideColumn is the name of the column a column beside column is named
// after: the plan's for its property, or, when Notion no longer gives that,
// its name in Leafen.
func (names notionMigrationColumnNames) besideColumn(column string) (string, error) {
	name, _, staged, err := names.planned(column)
	if err != nil || staged {
		return name, err
	}
	propertyID, err := notionImportMappedID(names.app, names.jobID, column, "property")
	if err != nil {
		return "", err
	}
	if err := names.app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": propertyID}).Row(&name); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return name, nil
}

// beside is the name a run gives a column beside column relating to target
// now, and the names runs gave it: after the column, or after a relation
// gathered into it, as runs before named it — each with the name Notion
// gives it now or gave it at the run before — and after target's name in
// Leafen now or Notion's at the run before.
func (names notionMigrationColumnNames) beside(column, target string) (string, map[string]bool, error) {
	title, err := notionBesideTitle(names.app, target)
	if err != nil {
		return "", nil, err
	}
	name, err := names.besideColumn(column)
	if err != nil {
		return "", nil, err
	}
	_, columns, _, err := names.planned(column)
	if err != nil {
		return "", nil, err
	}
	if database, _, found := strings.Cut(column, ":property:"); found {
		prefix := notionPropertySourceID(database, "")
		gathered := []string{}
		if err := names.app.DB().NewQuery(`
			SELECT [[source_id]] FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND [[source_id]] > {:prefix} AND [[source_id]] < {:after}
		`).Bind(dbx.Params{
			"jobId": names.jobID, "prefix": prefix, "after": strings.TrimSuffix(prefix, ":") + ";",
		}).Column(&gathered); err != nil {
			return "", nil, err
		}
		for _, source := range gathered {
			if role, into := names.run.roleColumn(source); source == column || !into || role != column {
				continue
			}
			_, named, _, err := names.planned(source)
			if err != nil {
				return "", nil, err
			}
			for name := range named {
				columns[name] = true
			}
		}
	}
	titles, err := names.titlesBefore(target)
	if err != nil {
		return "", nil, err
	}
	titles[strings.TrimSpace(title)] = true
	given := map[string]bool{}
	for column := range columns {
		for title := range titles {
			given[notionBesideName(column, title)] = true
		}
	}
	return notionBesideName(name, title), given, nil
}

// titlesBefore is the name a data source's own database had at the run
// before — Notion's, or the plan's — as the ledger keeps it; not that of a
// database merged into it.
func (names notionMigrationColumnNames) titlesBefore(dataSourceID string) (map[string]bool, error) {
	kept := []struct {
		SourceID string `db:"source_id"`
		State    string `db:"state"`
	}{}
	if err := names.app.DB().NewQuery(`
		SELECT [[source_id]],[[state]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[native_id]]={:id} AND [[kind]]='data_source'
	`).Bind(dbx.Params{"migrationId": names.migrationID, "id": dataSourceID}).All(&kept); err != nil {
		return nil, err
	}
	titles := map[string]bool{}
	for _, entry := range kept {
		if _, merged := names.run.mergeTarget(entry.SourceID); !merged && strings.TrimSpace(entry.State) != "" {
			titles[strings.TrimSpace(entry.State)] = true
		}
	}
	return titles, nil
}

// notionBesideTitle is the name of the data source a column beside a
// relation relates to.
func notionBesideTitle(app core.App, target string) (string, error) {
	var title string
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": target}).Row(&title); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return title, nil
}
