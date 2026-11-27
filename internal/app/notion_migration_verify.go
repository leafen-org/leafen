package app

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's run ends by checking what it made against what it read from
// Notion (2026-11-16: no step compared them, so a real move's 13 view
// differences and 11 lost row orders came to light only when the person
// pointed at them, and nobody looked at the views inside row pages). The
// check takes each database's rows and templates, the cells Notion holds
// empty (notion_migration_verify_cells.go), the formulas' values
// (notion_migration_verify_formulas.go), the columns a split's targets
// have for its database (notion_migration_verify_split_columns.go), and
// each view — a database's own and every linked one, in pages and row pages — with its
// shown columns and their order, widths, sorts, filter and groups, Notion's
// side as the run read it and Leafen's as it is now. What differs is listed
// with why: what the plan left out, what an import issue already names, what
// a person changed meanwhile, or nothing known. What Notion's API doesn't
// give (a page's width, a database's page layout, a board's columns, a
// view's hand order, a form's questions) is what it can't compare: it is
// counted apart, as formats left.

// notionMigrationVerifyShown is how many differences a check keeps listed;
// it counts all of them by group (notion_migration_verify_groups.go).
const notionMigrationVerifyShown = 200

// Why a difference stands, beside the import issue codes a view's own issue
// names.
const (
	notionMigrationVerifyTrashed     = "changed_meanwhile"
	notionMigrationVerifyNotMade     = "not_made"
	notionMigrationVerifyNotBrought  = "property_not_brought"
	notionMigrationVerifyUnexplained = "unexplained"
)

type notionMigrationVerifier struct {
	app                core.App
	jobID, migrationID string
	// The databases the plan merges into another, and the relations it
	// gathers into another of their database.
	merged map[string]bool
	into   map[string]map[string]string
	// The data sources the run made, by their staged key, and the
	// databases' titles.
	sources    map[string]string
	titles     map[string]string
	issues     map[string][]string
	converters map[string]*notionImportViewConverter
	verify     notionMigrationVerify
	remaining  []notionMigrationVerifyDifference
	// The person's, to name a view left unnamed by its layout.
	language string
}

// checkNotionMigration compares a run's import with what it read from
// Notion.
func checkNotionMigration(app core.App, jobID, migrationID string, plan notionMigrationPlan) (notionMigrationVerify, error) {
	v := &notionMigrationVerifier{
		app: app, jobID: jobID, migrationID: migrationID, merged: map[string]bool{}, into: map[string]map[string]string{},
		sources: map[string]string{}, titles: map[string]string{}, issues: map[string][]string{},
		converters: map[string]*notionImportViewConverter{},
	}
	owner := ""
	_ = app.DB().NewQuery(`SELECT [[owner]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": migrationID}).Row(&owner)
	v.language, _ = serverTextLanguage(app, owner, nil)
	for _, database := range plan.Databases {
		key := notionMigrationSourceKey(database.SourceID)
		if database.Action == "merge" {
			v.merged[key] = true
		}
		gather := func(properties []notionMigrationPlanProperty) {
			for _, property := range properties {
				if into := strings.TrimSpace(property.Into); into != "" {
					if v.into[key] == nil {
						v.into[key] = map[string]string{}
					}
					v.into[key][notionImportPropertyKey(property.SourceID)] = into
				}
			}
		}
		gather(database.Properties)
		for _, source := range database.DataSources {
			gather(source.Properties)
		}
	}
	steps := []func() error{v.load, v.rows, v.relations, v.cells, v.formulas, v.splitColumns, v.views}
	for _, step := range steps {
		if err := step(); err != nil {
			return notionMigrationVerify{}, err
		}
	}
	unchecked, err := notionMigrationFormatsLeftKinds(app, jobID)
	if err != nil {
		return notionMigrationVerify{}, err
	}
	v.verify.Unchecked = append([]notionMigrationFormatsLeftKind{}, unchecked...)
	v.verify.RemainingTotal = len(v.remaining)
	v.verify.RemainingGroups = notionMigrationVerifyGroupCounts(v.remaining)
	v.verify.Remaining = notionMigrationVerifyKept(v.remaining)
	v.verify.CheckedAt = time.Now().UTC().UnixMilli()
	return v.verify, nil
}

// notionMigrationVerifyDatabase is the database a staged key names, its
// data source's own part left aside.
func notionMigrationVerifyDatabase(key string) string {
	database, _, _ := strings.Cut(key, notionImportDataSourceKeyMarker)
	return notionMigrationSourceKey(database)
}

func (v *notionMigrationVerifier) load() error {
	made := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source_id]], [[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='data_source'
	`).Bind(dbx.Params{"jobId": v.jobID}).All(&made); err != nil {
		return err
	}
	for _, source := range made {
		v.sources[source.SourceID] = source.NativeID
	}
	titles := []struct {
		SourceID string `db:"source_id"`
		Title    string `db:"title"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source_id]], [[title]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='database'
	`).Bind(dbx.Params{"jobId": v.jobID}).All(&titles); err != nil {
		return err
	}
	for _, database := range titles {
		v.titles[notionMigrationSourceKey(database.SourceID)] = database.Title
	}
	issues := []struct {
		SourceID string `db:"source_id"`
		Reason   string `db:"text"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source_id]], [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='issue' AND [[parent_kind]]={:view} AND instr([[source_id]], ':view:')>0
		ORDER BY [[source_id]]
	`).Bind(dbx.Params{"jobId": v.jobID, "view": notionImportViewIssueParentKind}).All(&issues); err != nil {
		return err
	}
	for _, issue := range issues {
		view, _, _ := strings.Cut(issue.SourceID, "#")
		v.issues[view] = append(v.issues[view], issue.Reason)
	}
	return nil
}

func (v *notionMigrationVerifier) differ(difference notionMigrationVerifyDifference) {
	v.remaining = append(v.remaining, difference)
}

// notionMigrationVerifyView is a staged view with the Leafen view it became.
type notionMigrationVerifyView struct {
	SourceID     string `db:"source_id"`
	Database     string `db:"parent_source_id"`
	Title        string `db:"title"`
	Text         string `db:"text"`
	NativeID     string `db:"native_id"`
	Type         string `db:"type"`
	Config       string `db:"config"`
	DataSourceID string `db:"data_source_id"`
	SourcePage   string `db:"source_page"`
	Holder       string `db:"holder"`
	HolderKind   string `db:"holder_kind"`
	SourceTitle  string `db:"source_title"`
}

func (v *notionMigrationVerifier) views() error {
	views := []notionMigrationVerifyView{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source]].[[source_id]], [[source]].[[parent_source_id]], [[source]].[[title]], [[source]].[[text]],
			COALESCE([[mapping]].[[native_id]], '') AS [[native_id]], COALESCE([[view]].[[type]], '') AS [[type]],
			COALESCE([[view]].[[config]], '') AS [[config]], COALESCE([[view]].[[data_source_id]], '') AS [[data_source_id]],
			COALESCE([[data_source]].[[page_id]], '') AS [[source_page]], COALESCE([[holder]].[[title]], '') AS [[holder]],
			COALESCE([[holder]].[[kind]], '') AS [[holder_kind]], COALESCE([[data_source]].[[title]], '') AS [[source_title]]
		FROM {{leafen_notion_import_sources}} AS [[source]]
		LEFT JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]]=[[source]].[[job_id]]
			AND [[mapping]].[[source_id]]=[[source]].[[source_id]] AND [[mapping]].[[kind]]='view'
		LEFT JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]]=[[mapping]].[[native_id]]
		LEFT JOIN {{leafen_data_sources}} AS [[data_source]] ON [[data_source]].[[id]]=[[view]].[[data_source_id]]
		LEFT JOIN {{leafen_pages}} AS [[holder]] ON [[holder]].[[id]]=[[view]].[[page_id]]
		WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='view'
		ORDER BY [[source]].[[sequence]]
	`).Bind(dbx.Params{"jobId": v.jobID}).All(&views); err != nil {
		return err
	}
	for _, view := range views {
		plan := notionImportViewPlan{}
		if json.Unmarshal([]byte(view.Text), &plan) != nil {
			continue
		}
		key := view.Database
		if plan.DataSourceKey != "" {
			key = plan.DataSourceKey
		}
		// A merged database's views aren't made; one of a database the run
		// didn't make has nothing to show.
		if _, made := v.sources[key]; !made || v.merged[notionMigrationVerifyDatabase(key)] {
			continue
		}
		kind := notionMigrationVerifyKindView
		if plan.LinkedBlock != "" {
			kind, v.verify.LinkedViews = notionMigrationVerifyKindLinkedView, v.verify.LinkedViews+1
		} else {
			v.verify.Views++
		}
		_, viewID, _ := strings.Cut(view.SourceID, ":view:")
		// Named where it is, as the report folds it (notion_migration_formats_left_place.go).
		place := cmpOr(notionMigrationFormatsLeftPlace("view", view.Holder, view.HolderKind, view.SourceTitle), v.titles[key])
		own, _ := notionImportViewOwnName(plan.Name)
		view.Title = notionMigrationViewNamed(v.language, place, own, plan.Type)
		noteNotionMigrationLive(v.migrationID, notionMigrationStageVerifying, "database", place)
		difference := notionMigrationVerifyDifference{Kind: kind, NotionID: viewID, ID: view.NativeID, Title: view.Title}
		if view.NativeID == "" || view.Config == "" {
			difference.Setting, difference.Notion, difference.Leafen = notionMigrationVerifySettingMissing, plan.Type, ""
			difference.Reason = v.reason(view.SourceID, notionMigrationVerifyNotMade)
			v.differ(difference)
			continue
		}
		converter, err := v.converter(key, view.DataSourceID, view.SourcePage)
		if err != nil {
			return err
		}
		for _, found := range compareNotionMigrationView(converter, v.into[notionMigrationVerifyDatabase(key)], plan, view) {
			found.Kind, found.NotionID, found.ID, found.Title = kind, viewID, view.NativeID, view.Title
			if found.Reason == "" {
				found.Reason = v.reason(view.SourceID, notionMigrationVerifyUnexplained)
			}
			found.Fix = notionMigrationVerifyFixes[found.Setting]
			v.differ(found)
		}
	}
	return nil
}

// reason is why a view differs: the issue the import recorded for it, or
// what else is known.
func (v *notionMigrationVerifier) reason(viewSourceID, otherwise string) string {
	if issues := v.issues[viewSourceID]; len(issues) > 0 {
		return strings.Join(issues, ",")
	}
	return otherwise
}

// converter points a view's Notion properties at the Leafen ones as the run
// did (notion_import_view_links.go), knowing which the data source had.
func (v *notionMigrationVerifier) converter(key, dataSourceID, pageID string) (*notionImportViewConverter, error) {
	if converter, found := v.converters[dataSourceID]; found {
		return converter, nil
	}
	converter, err := newNotionImportViewConverter(v.app, dataSourceID, pageID, func(string) (string, bool) { return "", false })
	if err != nil {
		return nil, err
	}
	staged := []struct {
		SourceID string `db:"source_id"`
		Title    string `db:"title"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source_id]], [[title]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]] IN ('property','issue') AND [[parent_source_id]]={:key}
			AND instr([[source_id]], ':property:')>0
	`).Bind(dbx.Params{"jobId": v.jobID, "key": key}).All(&staged); err != nil {
		return nil, err
	}
	converter.sourceProperties = make(map[string]bool, 2*len(staged))
	for _, property := range staged {
		_, notionID, _ := strings.Cut(property.SourceID, ":property:")
		notionID, _, _ = strings.Cut(notionID, "#")
		converter.sourceProperties[notionImportPropertyKey(notionID)] = true
		converter.sourceProperties[notionImportPropertyKey(property.Title)] = true
	}
	v.converters[dataSourceID] = converter
	return converter, nil
}

// notionMigrationVerifyFixes say how the agent sets a view's setting as
// Notion has it.
var notionMigrationVerifyFixes = map[string]string{
	notionMigrationVerifySettingColumns: "notion-update-view: show, hide and order its properties as in Notion",
	notionMigrationVerifySettingWidths:  "notion-update-view: give its columns Notion's widths",
	notionMigrationVerifySettingSorts:   "notion-update-view: SORT BY as in Notion",
	notionMigrationVerifySettingFilter:  "notion-update-view: FILTER as in Notion",
	notionMigrationVerifySettingGroup:   "notion-update-view: GROUP BY as in Notion",
}

// compareNotionMigrationView lists where a Leafen view differs from the
// Notion view it came from. A property Notion names that didn't come counts
// as a difference of its own, why given; one Notion names that its database
// no longer had is none — Notion's own view shows nothing for it.
func compareNotionMigrationView(
	converter *notionImportViewConverter, into map[string]string, plan notionImportViewPlan, view notionMigrationVerifyView,
) []notionMigrationVerifyDifference {
	config := map[string]any{}
	if json.Unmarshal([]byte(view.Config), &config) != nil {
		return nil
	}
	resolve := func(reference any) (string, bool, bool) {
		if property, found := converter.property(reference); found {
			return property.ID, true, true
		}
		text, _ := reference.(string)
		if target := into[notionImportPropertyKey(text)]; target != "" {
			if property, found := converter.property(target); found {
				return property.ID, true, true
			}
		}
		return "", false, converter.inSource(reference)
	}
	// A property gone from the data source that a view still names shows
	// nothing there, as in Notion.
	live := func(id string) bool {
		_, found := converter.scope.byID[id]
		return id != "" && found
	}
	differences := []notionMigrationVerifyDifference{}
	add := func(setting, notion, leafen string, lost bool) {
		difference := notionMigrationVerifyDifference{Setting: setting, Notion: notion, Leafen: leafen}
		if lost {
			difference.Reason = notionMigrationVerifyNotBrought
		}
		differences = append(differences, difference)
	}
	settings := map[string]any{}
	_ = json.Unmarshal(plan.Configuration, &settings)

	// The shown columns in their order, and their widths.
	if items, listed := settings["properties"].([]any); listed && !slices.Contains([]string{graphViewTypeForm, graphViewTypeChart, graphViewTypeDashboard, graphViewTypeMap}, plan.Type) {
		shown, widths, lost, lostCount := []string{}, map[string]float64{}, false, 0
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			reference := item["property_id"]
			if reference == nil {
				reference = item["property_name"]
			}
			id, found, inSource := resolve(reference)
			if item["visible"] == false {
				continue
			}
			if !found {
				if inSource {
					lost, lostCount = true, lostCount+1
				}
				continue
			}
			if !slices.Contains(shown, id) {
				shown = append(shown, id)
				if width, ok := item["width"].(float64); ok && width > 0 {
					widths[id] = width
				}
			}
		}
		hidden := notionViewStringSet(config["hiddenProperties"])
		order, _ := config["propertyOrder"].([]any)
		leafen := []string{}
		for _, raw := range order {
			if id, _ := raw.(string); live(id) {
				if _, isHidden := hidden[id]; !isHidden {
					leafen = append(leafen, id)
				}
			}
		}
		if len(order) > 0 && (!slices.Equal(shown, leafen) || lost) {
			add(notionMigrationVerifySettingColumns, fmt.Sprint(len(shown)+lostCount), fmt.Sprint(len(leafen)), lost && slices.Equal(shown, leafen))
		}
		given, _ := config["propertyWidths"].(map[string]any)
		off := 0
		for id, width := range widths {
			if got, ok := given[id].(float64); !ok || math.Abs(got-width) > 0.5 {
				off++
			}
		}
		if off > 0 {
			add(notionMigrationVerifySettingWidths, fmt.Sprint(len(widths)), fmt.Sprint(len(widths)-off), false)
		}
	}

	// The sorts by property, in their order; Leafen keeps no sort by a
	// timestamp, which the import reports.
	sorts := []struct {
		Property  *string `json:"property"`
		Direction string  `json:"direction"`
	}{}
	if json.Unmarshal(plan.Sorts, &sorts) == nil {
		notion, lost := []string{}, false
		for _, sort := range sorts {
			if sort.Property == nil {
				continue
			}
			id, found, inSource := resolve(*sort.Property)
			if !found {
				lost = lost || inSource
				continue
			}
			notion = append(notion, id+":"+storedViewSortDirection(sort.Direction))
		}
		leafen := []string{}
		stored, _ := config["sorts"].([]any)
		for _, raw := range stored {
			sort, _ := raw.(map[string]any)
			id, _ := sort["propertyId"].(string)
			direction, _ := sort["direction"].(string)
			if live(id) {
				leafen = append(leafen, id+":"+storedViewSortDirection(direction))
			}
		}
		if !slices.Equal(notion, leafen) || lost {
			add(notionMigrationVerifySettingSorts, fmt.Sprint(len(sorts)), fmt.Sprint(len(leafen)), lost && slices.Equal(notion, leafen))
		}
	}

	// The filter's conditions, counted.
	if len(plan.Filter) > 0 {
		filter := any(nil)
		if json.Unmarshal(plan.Filter, &filter) == nil {
			notion, leafen := notionMigrationFilterConditions(filter), notionMigrationNativeFilterConditions(config["filterGroup"], live)
			if notion != leafen {
				add(notionMigrationVerifySettingFilter, fmt.Sprint(notion), fmt.Sprint(leafen), false)
			}
		}
	}

	// The group of a view that takes one (a list's, gallery's and timeline's too) and, on a board, the sub-group: their property.
	for official, native := range map[string]string{"group_by": "groupBy", "sub_group_by": "subGroupBy"} {
		group, _ := settings[official].(map[string]any)
		if group == nil || !notionViewTakesField(plan.Type, official) {
			continue
		}
		id, found, inSource := resolve(group["property_id"])
		stored, _ := config[native].(string)
		if !found && !inSource {
			continue
		}
		if !found || id != stored {
			add(notionMigrationVerifySettingGroup, converter.scope.byID[id].Name, converter.scope.byID[stored].Name, !found)
		}
	}
	return differences
}

// notionMigrationFilterConditions counts a Notion filter's conditions, in
// its groups too.
func notionMigrationFilterConditions(filter any) int {
	switch typed := filter.(type) {
	case []any:
		count := 0
		for _, item := range typed {
			count += notionMigrationFilterConditions(item)
		}
		return count
	case map[string]any:
		for _, conjunction := range []string{"and", "or"} {
			if terms, grouped := typed[conjunction]; grouped {
				return notionMigrationFilterConditions(terms)
			}
		}
		if typed["property"] != nil || typed["timestamp"] != nil {
			return 1
		}
	}
	return 0
}

// notionMigrationNativeFilterConditions counts a Leafen view's filter
// conditions on properties its data source has, in its groups too.
func notionMigrationNativeFilterConditions(group any, live func(string) bool) int {
	fields, _ := group.(map[string]any)
	filters, _ := fields["filters"].([]any)
	count := 0
	for _, raw := range filters {
		filter, _ := raw.(map[string]any)
		if id, _ := filter["propertyId"].(string); live(id) {
			count++
		}
	}
	groups, _ := fields["groups"].([]any)
	for _, nested := range groups {
		count += notionMigrationNativeFilterConditions(nested, live)
	}
	return count
}
