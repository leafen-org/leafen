package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Updating an app's data structure (docs/app-platform.md 6): a new version's
// setup steps are compared with what each workspace was made from. A new
// step is made; a database's new columns are added and changed ones
// converted where a conversion keeps the values; a step that is gone, and a
// column a new version drops, go to the trash — only when the administrator
// confirmed that for this version. A view or page whose arguments changed
// stays as people may have changed it. The administrator sees the changes in
// the package review; each workspace catches up right after the update, or
// when the app opens there next if that failed.

const (
	appDataChangeAdd        = "add"
	appDataChangeRemove     = "remove"
	appDataChangeProperties = "properties"
	appDataChangeKept       = "kept"
	appDataChangeLeft       = "left"
)

var errAppDataRemovals = errors.New(errorCodeAppDataRemovals)

func loadAppDataSteps(app core.App, packageHash string, manifest appManifest) ([]appDataStep, error) {
	steps := []appDataStep{}
	done := map[string]bool{}
	for _, data := range manifest.Data {
		// Each person's own steps run for them (app_platform_person_data.go).
		if done[data.File] || data.PerPerson {
			continue
		}
		done[data.File] = true
		raw, err := loadAppPackageFile(app, packageHash, data.File)
		if err != nil {
			return nil, err
		}
		setup, err := parseAppDataSetup(raw)
		if err != nil {
			return nil, err
		}
		steps = append(steps, setup.Steps...)
	}
	return steps, nil
}

// appDataStepName is what people know a step's data by: a database's or
// view's name, or the first page's title.
func appDataStepName(step appDataStep) string {
	named := struct {
		Title string `json:"title"`
		Name  string `json:"name"`
		Pages []struct {
			Properties map[string]any `json:"properties"`
		} `json:"pages"`
	}{}
	_ = json.Unmarshal(step.Args, &named)
	names := []string{named.Title, named.Name}
	if len(named.Pages) > 0 {
		if title, ok := named.Pages[0].Properties["title"].(string); ok {
			names = append(names, title)
		}
		for _, value := range named.Pages[0].Properties {
			if text, ok := value.(string); ok {
				names = append(names, text)
				break
			}
		}
	}
	for _, name := range names {
		if strings.TrimSpace(name) != "" && !strings.Contains(name, "{{") {
			if len(named.Pages) > 1 {
				return fmt.Sprintf("%s +%d", name, len(named.Pages)-1)
			}
			return name
		}
	}
	return step.Key
}

func appDataSameArgs(left, right json.RawMessage) bool {
	var a, b bytes.Buffer
	return json.Compact(&a, left) == nil && json.Compact(&b, right) == nil && bytes.Equal(a.Bytes(), b.Bytes())
}

// appDataColumns are a create-database step's columns by name.
func appDataColumns(step appDataStep) (map[string]mcpDatabaseSchemaProperty, []string) {
	input := notionMCPCreateDatabaseInput{}
	columns, order := map[string]mcpDatabaseSchemaProperty{}, []string{}
	if json.Unmarshal(step.Args, &input) != nil {
		return columns, order
	}
	properties, _, _ := parseMCPDatabaseSchema(input.Schema)
	for _, property := range properties {
		columns[property.Name] = property
		order = append(order, property.Name)
	}
	return columns, order
}

type appDataColumnChanges struct {
	added, removed, converted []mcpDatabaseSchemaProperty
}

// appDataColumnDiff is what a database's columns gain, lose and change to;
// a change no conversion keeps is losing the column and gaining a new one.
func appDataColumnDiff(before, after appDataStep) appDataColumnChanges {
	old, _ := appDataColumns(before)
	next, order := appDataColumns(after)
	changes := appDataColumnChanges{}
	for _, name := range order {
		column, previous := next[name], old[name]
		switch {
		case previous.Name == "":
			changes.added = append(changes.added, column)
		case previous.Type == column.Type:
		case previous.Type != graphValueTypeTitle && column.Type != graphValueTypeTitle && databasePropertyTypeChangeAllowed(previous.Type, column.Type):
			changes.converted = append(changes.converted, column)
		default:
			changes.removed = append(changes.removed, previous)
			changes.added = append(changes.added, column)
		}
	}
	_, oldOrder := appDataColumns(before)
	for _, name := range oldOrder {
		if _, kept := next[name]; !kept && old[name].Type != graphValueTypeTitle {
			changes.removed = append(changes.removed, old[name])
		}
	}
	return changes
}

func appDataColumnNames(columns []mcpDatabaseSchemaProperty) []string {
	names := []string{}
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

// appDataChanges compares what a workspace was made from with a version's steps.
func appDataChanges(before map[string]appDataStep, steps []appDataStep) []appDataChangeWire {
	changes := []appDataChangeWire{}
	after := map[string]bool{}
	for _, step := range steps {
		after[step.Key] = true
		previous, existed := before[step.Key]
		change := appDataChangeWire{Key: step.Key, Tool: step.Tool, Name: appDataStepName(step), Added: []string{}, Removed: []string{}, Converted: []string{}}
		switch {
		case !existed:
			change.Kind = appDataChangeAdd
		case appDataSameArgs(previous.Args, step.Args):
			continue
		case previous.Tool == appDataToolDatabase && step.Tool == appDataToolDatabase:
			columns := appDataColumnDiff(previous, step)
			if len(columns.added)+len(columns.removed)+len(columns.converted) == 0 {
				change.Kind = appDataChangeKept
				break
			}
			change.Kind = appDataChangeProperties
			change.Added, change.Removed, change.Converted =
				appDataColumnNames(columns.added), appDataColumnNames(columns.removed), appDataColumnNames(columns.converted)
			change.Destructive = len(columns.removed) > 0
		default:
			change.Kind = appDataChangeKept
		}
		changes = append(changes, change)
	}
	gone := []string{}
	for key := range before {
		if !after[key] {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		previous := before[key]
		// A view stays with its database and rows stay with theirs; a
		// database or page goes to the trash.
		change := appDataChangeWire{Kind: appDataChangeLeft, Key: key, Tool: previous.Tool, Name: appDataStepName(previous),
			Added: []string{}, Removed: []string{}, Converted: []string{}}
		if previous.Tool == appDataToolDatabase || previous.Tool == appDataToolPages && !appDataMakesRows(previous) {
			change.Kind, change.Destructive = appDataChangeRemove, true
		}
		if previous.Tool == appDataToolPages && appDataMakesRows(previous) {
			// Rows are known by their database.
			change.Rows = true
			if match := appDataPlaceholder.FindStringSubmatch(string(previous.Args)); match != nil {
				if database, known := before[match[1]]; known {
					change.Name = appDataStepName(database)
				}
			}
		}
		changes = append(changes, change)
	}
	sort.SliceStable(changes, func(left, right int) bool { return changes[left].Destructive && !changes[right].Destructive })
	return changes
}

// appDataMakesRows says a create-pages step adds rows to a database.
func appDataMakesRows(step appDataStep) bool {
	input := struct {
		Parent struct {
			DataSourceID string `json:"data_source_id"`
			DatabaseID   string `json:"database_id"`
		} `json:"parent"`
	}{}
	_ = json.Unmarshal(step.Args, &input)
	return input.Parent.DataSourceID != "" || input.Parent.DatabaseID != ""
}

func appDataChangesDestructive(changes []appDataChangeWire) bool {
	for _, change := range changes {
		if change.Destructive {
			return true
		}
	}
	return false
}

// appDataUpdateChanges are what installing a package would change in the
// app's data, named in a language when one is given. A workspace catches up
// from what it was made from (app_platform_data_catch_up.go): one made from
// the installed version gets what the installed version's data structure
// compared with the package's says, and one an earlier update didn't finish
// in gets what its own data compared with the package's says — a step that
// failed there is made, however the installed version wrote it. Those lines
// the installed version's comparison doesn't have are marked behind, and its
// own lines are left out when no workspace was made from it.
func appDataUpdateChanges(app core.App, installed storedAppInstall, packageHash string, manifest appManifest, language string) ([]appDataChangeWire, error) {
	_, current, err := loadAppPackage(app, installed.PackageHash)
	if err != nil {
		return nil, err
	}
	before, err := loadAppDataSteps(app, installed.PackageHash, current)
	if err != nil {
		return nil, err
	}
	steps, err := loadAppDataSteps(app, packageHash, manifest)
	if err != nil {
		return nil, err
	}
	byKey := map[string]appDataStep{}
	for _, step := range before {
		byKey[step.Key] = step
	}
	named := func(changes []appDataChangeWire, before map[string]appDataStep) []appDataChangeWire {
		if language == "" {
			return changes
		}
		return appDataChangesIn(changes, before, steps, language)
	}
	changes := appDataChanges(byKey, steps)
	behind, upToDate, err := appDataBehindStates(app, installed)
	if err != nil || len(behind) == 0 {
		return named(changes, byKey), err
	}
	known, seen, lines := map[string]bool{}, map[string]bool{}, []appDataChangeWire{}
	for _, change := range changes {
		known[appDataChangeLine(change)] = true
	}
	if upToDate > 0 {
		seen, lines = known, named(changes, byKey)
	}
	for _, state := range behind {
		// Its names are the installed version's for what it still has.
		madeBefore := map[string]appDataStep{}
		for key, step := range state.before {
			step.words = byKey[key].words
			madeBefore[key] = step
		}
		for _, change := range appDataChanges(state.before, steps) {
			// A step made before Leafen kept what steps were made from is
			// only recorded again.
			if _, recorded := state.results[change.Key]; recorded && change.Kind == appDataChangeAdd {
				continue
			}
			line := appDataChangeLine(change)
			if seen[line] {
				continue
			}
			change.Behind = !known[line]
			seen[line] = true
			lines = append(lines, named([]appDataChangeWire{change}, madeBefore)...)
		}
	}
	sort.SliceStable(lines, func(left, right int) bool { return lines[left].Destructive && !lines[right].Destructive })
	return lines, nil
}

// appDataBehindStates are what each of the app's workspaces an earlier
// update didn't finish in was made as, and how many workspaces were brought
// up to the installed version — the app details' "behind" (app_platform_data_failure.go).
func appDataBehindStates(app core.App, installed storedAppInstall) ([]appDataMadeState, int, error) {
	workspaces := []struct {
		Tenancy string `db:"tenancy_key"`
		Package string `db:"data_package"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[tenancy_key]], [[data_package]] FROM {{leafen_app_workspaces}}
		WHERE [[install_id]] = {:install} AND [[setup_state]] = {:done} ORDER BY [[tenancy_key]]`).
		Bind(dbx.Params{"install": installed.ID, "done": appDataSetupDone}).All(&workspaces); err != nil {
		return nil, 0, err
	}
	states, upToDate := []appDataMadeState{}, 0
	for _, workspace := range workspaces {
		if workspace.Package == installed.PackageHash {
			upToDate++
			continue
		}
		state, err := loadAppDataMadeState(app, installed.ID, workspace.Tenancy)
		if err != nil {
			return nil, 0, err
		}
		states = append(states, state)
	}
	return states, upToDate, nil
}

// appDataChangeLine is what a change does, to tell one workspace's lines
// from another's.
func appDataChangeLine(change appDataChangeWire) string {
	encoded, _ := json.Marshal([]any{change.Kind, change.Key, change.Tool, change.Added, change.Removed, change.Converted, change.Rows, change.Destructive})
	return string(encoded)
}

// catchUpAppDataEverywhere brings each of the app's workspaces up to the
// installed version right after an update; one that fails is kept for the
// administrators (app_platform_data_failure.go) and tries again when the app
// opens there.
func catchUpAppDataEverywhere(app core.App, install storedAppInstall) {
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return
	}
	workspaces, err := loadAppWorkspaces(app, install.ID)
	if err != nil {
		return
	}
	for _, workspace := range workspaces {
		if err := runAppDataSetup(app, install, manifest, workspace); err != nil {
			app.Logger().Warn("app data update failed", "install", install.ID, "tenancy", workspace.TenancyKey, "error", err.Error())
		}
	}
}

func setAppDataPackage(app core.App, workspace storedAppWorkspace, packageHash string) error {
	_, err := app.DB().Update(appWorkspacesTable, dbx.Params{"data_package": packageHash}, dbx.HashExp{"workspace_id": workspace.WorkspaceID}).Execute()
	return err
}
