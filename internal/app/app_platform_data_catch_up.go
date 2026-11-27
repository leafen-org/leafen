package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A workspace made from an earlier version of an app catches up with the
// installed one (docs/app-platform.md 6): what is new is made, a database's
// columns change, and what is gone goes to the trash.
// catchUpAppData brings a workspace made from an earlier version up to the
// installed one.
func catchUpAppData(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace) error {
	var applied, accepted string
	if err := app.DB().NewQuery(`SELECT w.[[data_package]], i.[[data_removals_accepted]] FROM {{leafen_app_workspaces}} w
		JOIN {{leafen_app_installs}} i ON i.[[id]] = w.[[install_id]] WHERE w.[[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"workspace": workspace.WorkspaceID}).Row(&applied, &accepted); err != nil {
		return err
	}
	if applied == install.PackageHash {
		return nil
	}
	state, err := loadAppDataMadeState(app, install.ID, workspace.TenancyKey)
	if err != nil {
		return err
	}
	steps, err := loadAppDataSteps(app, install.PackageHash, manifest)
	if err != nil {
		return err
	}
	before, results, roots, madeIn := state.before, state.results, state.roots, state.madeIn
	session, err := newAppDataSession(app, install, manifest, workspace)
	if err != nil {
		return err
	}
	removals, language := accepted == install.PackageHash, appOrganizationLanguage(app, workspace.TenancyKey)
	for _, change := range appDataChanges(before, steps) {
		if change.Kind != appDataChangeRemove || !removals {
			continue
		}
		if err := trashAppData(app, session, change.Tool, results[change.Key]); err != nil {
			return err
		}
		if _, err := app.DB().Delete(appDataTable, dbx.HashExp{"install_id": install.ID, "tenancy_key": workspace.TenancyKey, "data_key": change.Key}).Execute(); err != nil {
			return err
		}
	}
	for _, step := range steps {
		previous, known := before[step.Key]
		made, recorded := results[step.Key]
		// What was made keeps the language and names it took.
		spec := appDataSpecWith(step, madeIn[step.Key].Language, madeIn[step.Key].Names)
		switch {
		case !recorded:
			root, err := appDataRoot(app, roots, appDataPlace(manifest, step.Key), results, before, manifest)
			if err != nil {
				return err
			}
			// A new step is made in the organization's language.
			var args json.RawMessage
			args, spec = appDataMakeArgs(step, language)
			if made, err = session.runStep(app, step, fillAppDataPlaceholders(args, root, results)); err != nil {
				return err
			}
			results[step.Key] = made
		case known && step.Tool == appDataToolDatabase && previous.Tool == appDataToolDatabase && !appDataSameArgs(previous.Args, step.Args):
			// New columns name earlier steps' data as they were made.
			filled := appDataStep{Key: step.Key, Tool: step.Tool, Args: fillAppDataPlaceholders(step.Args, "", results), words: step.words}
			var statements []string
			statements, spec = appDataColumnStatements(previous, step, filled, madeIn[step.Key], removals)
			if len(statements) > 0 {
				if _, _, err := updateMCPDataSource(session.mcp, nil, app, notionMCPUpdateDataSourceInput{
					DataSourceID: made["data_source_id"], Statements: strings.Join(statements, "; "),
				}); err != nil {
					return fmt.Errorf("step %s: %w", step.Key, err)
				}
			}
		case known && appDataSameArgs(previous.Args, step.Args):
			continue
		}
		if err := recordAppData(app, install, workspace, step.Key, made["id"], made["data_source_id"], made["view_id"], spec); err != nil {
			return err
		}
	}
	// The update counts as done with its line in the app's audit; without it,
	// it runs again, its steps already made skipped.
	return app.RunInTransaction(func(tx core.App) error {
		if err := setAppDataPackage(tx, workspace, install.PackageHash); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "data-update", "", map[string]any{"tenancy": workspace.TenancyKey, "removals": removals})
	})
}

// appDataMadeState is what a workspace's data was made as: each step's
// made ids and the step it was made from (with the language and names it
// took), and its places' root pages. A step recorded before what it was made
// from was kept has ids but no step.
type appDataMadeState struct {
	before  map[string]appDataStep
	results map[string]map[string]string
	roots   map[string]string
	madeIn  map[string]appDataMade
}

func loadAppDataMadeState(app core.App, installID, tenancyKey string) (appDataMadeState, error) {
	rows, err := loadAppDataRows(app, installID, tenancyKey)
	if err != nil {
		return appDataMadeState{}, err
	}
	state := appDataMadeState{before: map[string]appDataStep{}, results: map[string]map[string]string{},
		roots: map[string]string{}, madeIn: map[string]appDataMade{}}
	for _, row := range rows {
		if place, isRoot := strings.CutPrefix(row.Key, "root-"); isRoot {
			state.roots[place] = row.Page
			continue
		}
		if strings.HasPrefix(row.Key, "teamspace-") {
			continue
		}
		state.results[row.Key] = map[string]string{"id": row.Page, "data_source_id": row.Source, "view_id": row.View}
		made := appDataMade{}
		if json.Unmarshal([]byte(row.Spec), &made) == nil && made.Key == row.Key {
			state.before[row.Key], state.madeIn[row.Key] = appDataStep{Key: made.Key, Tool: made.Tool, Args: made.Args}, made
		}
	}
	return state, nil
}

// appDataColumnStatements change a database made from one version's step
// to the next's, by the names its columns took: a column dropped or
// converted by its made name, one added in the language the database was
// made in. They answer what is kept of it then.
func appDataColumnStatements(previous, step, filled appDataStep, made appDataMade, removals bool) ([]string, string) {
	names := map[string]string{}
	for written, as := range made.Names {
		names[written] = as
	}
	madeName := func(name string) string {
		if as, found := names[name]; found {
			return as
		}
		return name
	}
	table, _ := appDataNamesIn(step.words, made.Language)
	localized, _ := appDataNamed(filled.Args, table)
	_, order := appDataColumns(filled)
	madeColumns, madeOrder := appDataColumns(appDataStep{Args: localized})
	columns := appDataColumnDiff(previous, filled)
	statements := []string{}
	if removals {
		for _, column := range columns.removed {
			statements = append(statements, "DROP COLUMN "+appDataQuoted(madeName(column.Name)))
		}
	}
	for _, column := range columns.added {
		if !removals && appDataColumnTaken(columns.removed, column.Name) {
			continue
		}
		as := column
		if index := slices.Index(order, column.Name); index >= 0 && index < len(madeOrder) {
			as = madeColumns[madeOrder[index]]
		}
		if as.Name != column.Name {
			names[column.Name] = as.Name
		}
		for index, option := range column.Options {
			if index < len(as.Options) && as.Options[index].Name != option.Name {
				names[option.Name] = as.Options[index].Name
			}
		}
		statements = append(statements, "ADD COLUMN "+appDataColumnDDL(as))
	}
	for _, column := range columns.converted {
		statements = append(statements, "ALTER COLUMN "+appDataQuoted(madeName(column.Name))+" SET "+appDataTypeKeyword(column.Type))
	}
	return statements, appDataSpecWith(step, made.Language, names)
}

// appDataRoot is the root page a step's place has; one the workspace made
// before roots were kept is the parent of what an earlier step there made.
func appDataRoot(app core.App, roots map[string]string, place string, results map[string]map[string]string, before map[string]appDataStep, manifest appManifest) (string, error) {
	if roots[place] != "" {
		return roots[place], nil
	}
	for key, made := range results {
		if _, known := before[key]; !known || appDataPlace(manifest, key) != place || made["id"] == "" {
			continue
		}
		var parent string
		err := app.DB().NewQuery(`SELECT COALESCE((SELECT [[parent_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}),
			(SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}), '')`).Bind(dbx.Params{"id": made["id"]}).Row(&parent)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if parent != "" {
			roots[place] = parent
			return parent, nil
		}
	}
	return "", fmt.Errorf("the app's %s root page wasn't found", place)
}

// trashAppData moves what a step made to the trash, where people can bring
// it back: a database's data source, or a page; a view stays with its
// database.
func trashAppData(app core.App, session appDataSession, tool string, made map[string]string) error {
	switch tool {
	case appDataToolDatabase:
		var host string
		err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": made["id"]}).Row(&host)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = session.native("POST", "/pages/"+host+"/data-sources/"+made["data_source_id"]+"/trash", map[string]any{})
		return err
	case appDataToolPages:
		answer, err := session.native("GET", "/pages/"+made["id"], nil)
		if err != nil {
			return err
		}
		title, _ := answer["title"].(string)
		parent, _ := answer["parentId"].(string)
		cursor, _ := answer["cursor"].(float64)
		change := map[string]any{
			"title": title, "parentId": parent, "inTrash": true, "mutationId": "app-data-update:" + security.RandomString(20), "baseCursor": cursor,
		}
		if teamspace, ok := answer["teamspaceId"].(string); ok && teamspace != "" {
			change["teamspaceId"] = teamspace
		}
		_, err = session.native("PATCH", "/pages/"+made["id"]+"/metadata", change)
		return err
	}
	return nil
}

func appDataQuoted(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func appDataColumnTaken(columns []mcpDatabaseSchemaProperty, name string) bool {
	for _, column := range columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func appDataTypeKeyword(propertyType string) string {
	for keyword, known := range mcpDatabaseSchemaPropertyTypes {
		if known == propertyType {
			return keyword
		}
	}
	return strings.ToUpper(propertyType)
}

// appDataColumnDDL writes a column back as CREATE TABLE does.
func appDataColumnDDL(column mcpDatabaseSchemaProperty) string {
	ddl := appDataQuoted(column.Name) + " " + appDataTypeKeyword(column.Type)
	if len(column.Options) > 0 {
		options := []string{}
		for _, option := range column.Options {
			options = append(options, "'"+strings.ReplaceAll(option.Name, "'", "''")+"':"+option.Color)
		}
		ddl += "(" + strings.Join(options, ", ") + ")"
	}
	quoted := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }
	switch column.Type {
	case graphValueTypeFormula:
		ddl += "(" + quoted(column.Formula) + ")"
	case graphValueTypeRelation:
		ddl += "(" + quoted(column.Relation)
		if column.Dual {
			ddl += ", DUAL"
			if column.SyncedName != "" {
				ddl += " " + quoted(column.SyncedName)
			}
		}
		ddl += ")"
	case graphValueTypeRollup:
		ddl += "(" + quoted(column.RollupRelation) + ", " + quoted(column.RollupTarget) + ", " + quoted(column.RollupFunction) + ")"
	case graphValueTypeUniqueID:
		if column.IDPrefix != "" {
			ddl += " PREFIX " + quoted(column.IDPrefix)
		}
	}
	if column.NumberFormat != "" {
		ddl += " FORMAT '" + column.NumberFormat + "'"
	}
	if column.Description != "" {
		ddl += " COMMENT '" + strings.ReplaceAll(column.Description, "'", "''") + "'"
	}
	return ddl
}
