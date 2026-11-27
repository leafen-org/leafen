package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's run matches what the reader read in the person's Notion
// (notion_migration_formats_read.go) to what it made, as its step before the
// ledger and the report: each page's and row's full width, small text and
// font as its "..." menu sets them; each database's page layout — its
// properties in Notion's order, each shown, hidden or hidden when empty, as
// the row page's "속성 표시 여부" sets them; each view's groups in Notion's
// order with the hidden ones hidden, its wrap, and its rows in the order
// people put them (by Notion's row ids, so rows of one title keep theirs).
// What it matched it marks, so the list of what is left
// (notion_migration_formats_left.go) and the report name only the rest.

const notionMigrationStepFormats = "formats"

// applyNotionMigrationFormatsWhenDone matches a reading that came in after
// the migration's run was done, with that run's job, and writes its report
// again.
func applyNotionMigrationFormatsWhenDone(app core.App, migrationID string) error {
	stored := struct {
		RunState    string `db:"run_state"`
		ImportJobID string `db:"import_job_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[run_state]],[[import_job_id]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).One(&stored); err != nil {
		return err
	}
	if stored.RunState != notionMigrationRunDone || stored.ImportJobID == "" {
		return nil
	}
	job, err := loadStoredJob(app, stored.ImportJobID, "")
	if err != nil {
		return err
	}
	payload := notionImportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	if err := app.RunInTransaction(func(txApp core.App) error {
		return applyNotionMigrationFormats(txApp, job, payload)
	}); err != nil {
		return err
	}
	return rewriteNotionMigrationReportAfterReading(app, migrationID, job, payload)
}

// notionMigrationFormatsMatcher holds what one matching needs of the run.
type notionMigrationFormatsMatcher struct {
	app     core.App
	job     storedJob
	payload notionImportJobPayload
	batches map[string]*structuralMutationBatch
	// The forms whose answering the reading set (notion_migration_formats_form.go).
	forms []notionMigrationFormOpen
	// The rows' orders the reading kept once, as read (notion_migration_formats_row_orders.go).
	orders map[string][]string
}

// applyNotionMigrationFormats matches the migration's reading, once a reading.
func applyNotionMigrationFormats(app core.App, job storedJob, payload notionImportJobPayload) error {
	reader, found, err := loadNotionMigrationFormatsReader(app, payload.MigrationID)
	if err != nil || !found || reader.ReadAt == 0 {
		return err
	}
	if reader.AppliedAt >= reader.ReadAt {
		return settleNotionMigrationReadRowOrders(app, job.ID, payload.MigrationID)
	}
	matcher := &notionMigrationFormatsMatcher{app: app, job: job, payload: payload, batches: map[string]*structuralMutationBatch{}}
	items := []struct {
		Kind     string `db:"kind"`
		NotionID string `db:"notion_id"`
		Value    string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[kind]],[[notion_id]],[[value]] FROM {{leafen_notion_migration_formats}}
		WHERE [[migration_id]]={:id} AND [[kind]]!='reader' ORDER BY [[kind]],[[notion_id]]`).
		Bind(dbx.Params{"id": payload.MigrationID}).All(&items); err != nil {
		return err
	}
	for _, item := range items {
		var value any
		switch item.Kind {
		case "page":
			page := notionMigrationPageFormat{}
			if err := json.Unmarshal([]byte(item.Value), &page); err != nil {
				return err
			}
			if page.Matched, err = matcher.page(item.NotionID, page); err != nil {
				return err
			}
			value = page
		case "database":
			database := notionMigrationDatabaseFormat{}
			if err := json.Unmarshal([]byte(item.Value), &database); err != nil {
				return err
			}
			if database.Matched, err = matcher.database(item.NotionID, database); err != nil {
				return err
			}
			// And its block's own title (notion_migration_formats_database_title.go).
			if titled, err := matcher.databaseTitle(item.NotionID, database.Title); err != nil || !titled {
				database.Matched = false
				if err != nil {
					return err
				}
			}
			value = database
		case "view":
			view := notionMigrationViewFormat{}
			if err := json.Unmarshal([]byte(item.Value), &view); err != nil {
				return err
			}
			if err := matcher.view(item.NotionID, &view); err != nil {
				return err
			}
			value = view
		default:
			continue
		}
		if err := storeNotionMigrationFormat(app, payload.MigrationID, job.WorkspaceID, item.Kind, item.NotionID, value); err != nil {
			return err
		}
	}
	if err := saveStructuralMutationBatches(app, job.Owner, matcher.batches); err != nil {
		return err
	}
	if err := matcher.openForms(); err != nil {
		return err
	}
	// Notion's API not giving the rows' order is no longer so where it matched.
	if err := settleNotionMigrationReadRowOrders(app, job.ID, payload.MigrationID); err != nil {
		return err
	}
	reader.AppliedAt = reader.ReadAt
	return storeNotionMigrationFormat(app, payload.MigrationID, job.WorkspaceID, "reader", "", reader)
}

func (matcher *notionMigrationFormatsMatcher) mapped(sourceID, kind string) (string, error) {
	return notionImportMappedID(matcher.app, matcher.job.ID, sourceID, kind)
}

// page gives a page or a row what Notion shows of it; a database's own page
// is the one its block opens.
func (matcher *notionMigrationFormatsMatcher) page(notionID string, format notionMigrationPageFormat) (bool, error) {
	font, smallText, fullWidth := format.Font, format.SmallText, format.FullWidth
	if rowID, err := matcher.mapped(notionID, "row"); err != nil || rowID != "" {
		if err != nil {
			return false, err
		}
		err := setDatabaseRowPageStyle(matcher.app, rowID, matcher.job.WorkspaceID, "", &font, &smallText, &fullWidth)
		return err == nil, ignoreNotionMigrationFormatConflict(err)
	}
	pageID, err := matcher.mapped(notionID, "page")
	if err == nil && pageID == "" {
		pageID, err = matcher.mapped("notion-database-root-page:"+notionID, "page")
	}
	if err == nil && pageID == "" {
		return matcher.template(notionID, format)
	}
	if err != nil {
		return false, err
	}
	page, err := matcher.app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return false, nil
	}
	if page.GetString("font") == font && page.GetBool("small_text") == smallText && page.GetBool("full_width") == fullWidth {
		return true, nil
	}
	metadata := pageMetadataInput{
		Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: page.GetBool("in_trash"),
		Font: &font, SmallText: &smallText, FullWidth: &fullWidth,
		MutationID: "notion-migration-formats:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
	}
	_, status, err := applyPageMetadataTransaction(matcher.app, matcher.job.Owner, pageID, metadata, pageMetadataRequestHash(pageID, metadata))
	if err != nil {
		return false, ignoreNotionMigrationFormatConflict(err)
	}
	return status == pageMetadataApplied, nil
}

// ignoreNotionMigrationFormatConflict lets a page someone changed or took
// away since stay as it is, unmatched.
func ignoreNotionMigrationFormatConflict(err error) error {
	if err == nil || errors.Is(err, errDatabaseRowPageConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	var refused *graphRequestError
	if errors.As(err, &refused) {
		return nil
	}
	return err
}

// properties are the database's imported properties by their Notion ids.
func (matcher *notionMigrationFormatsMatcher) properties(databaseID string) (map[string]string, error) {
	rows := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
	}{}
	prefix := databaseID + ":property:"
	if err := matcher.app.DB().NewQuery(`SELECT [[source_id]],[[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND substr([[source_id]], 1, length({:prefix}))={:prefix}`).
		Bind(dbx.Params{"jobId": matcher.job.ID, "prefix": prefix}).All(&rows); err != nil {
		return nil, err
	}
	properties := map[string]string{}
	for _, row := range rows {
		notionID := strings.TrimPrefix(row.SourceID, prefix)
		if strings.Contains(notionID, ":") {
			continue
		}
		properties[notionImportPropertyKey(notionID)] = row.NativeID
	}
	return properties, nil
}

// database lays out a database's pages as Notion does: the property group
// in Notion's order, the ones it doesn't list after in theirs, and each
// property shown, hidden or hidden when empty.
func (matcher *notionMigrationFormatsMatcher) database(notionID string, format notionMigrationDatabaseFormat) (bool, error) {
	dataSourceID, err := matcher.mapped(notionID, "data_source")
	if err != nil || dataSourceID == "" {
		return false, err
	}
	pageID, err := notionMigrationDataSourcePage(matcher.app, dataSourceID)
	if err != nil || pageID == "" {
		return false, err
	}
	byNotion, err := matcher.properties(notionID)
	if err != nil {
		return false, err
	}
	stored, err := loadDatabasePropertyRecords(matcher.app, pageID)
	if err != nil {
		return false, err
	}
	ordered := []string{}
	for _, notionProperty := range format.Order {
		id := byNotion[notionImportPropertyKey(notionProperty)]
		if property, found := stored[id]; found && property.DataSourceID == dataSourceID && property.Type != graphValueTypeTitle && !slices.Contains(ordered, id) {
			ordered = append(ordered, id)
		}
	}
	visibility := map[string]string{}
	for notionProperty, shown := range format.Visibility {
		if id := byNotion[notionImportPropertyKey(notionProperty)]; id != "" && oneOf(shown, "show", "hide", "hide_if_empty") {
			visibility[id] = shown
		}
	}
	current, err := loadDatabasePageLayout(matcher.app, dataSourceID)
	if err != nil {
		return false, err
	}
	layout := defaultDatabasePageLayout()
	if current.Layout != nil {
		layout = *current.Layout
	}
	if len(ordered) > 0 {
		group := []databasePageLayoutGroupItem{}
		for _, id := range ordered {
			group = append(group, databasePageLayoutGroupItem{Kind: "property", PropertyID: id})
		}
		for _, item := range layout.PropertyGroup {
			if item.Kind == "property" && !slices.Contains(ordered, item.PropertyID) {
				group = append(group, item)
			}
		}
		layout.PropertyGroup = group
		raw, err := json.Marshal(layout)
		if err != nil {
			return false, err
		}
		parsed, err := parseDatabasePageLayout(matcher.app, raw, pageID, dataSourceID, matcher.job.WorkspaceID)
		if err != nil {
			return false, nil
		}
		encoded, err := json.Marshal(parsed)
		if err != nil {
			return false, err
		}
		if err := storeDatabasePageLayout(matcher.app, dataSourceID, matcher.job.WorkspaceID, encoded, current.Revision+1,
			matcher.job.Owner, time.Now().UTC().UnixMilli()); err != nil {
			return false, err
		}
	}
	operations := []graphOperation{}
	for id, property := range stored {
		if property.DataSourceID != dataSourceID || property.Type == graphValueTypeTitle {
			continue
		}
		want := visibility[id]
		if want == "" {
			want = "show"
		}
		config := maps.Clone(property.Config)
		if config == nil {
			config = map[string]any{}
		}
		if mcpPropertyVisibility(config) == want {
			continue
		}
		config["hideInPagePanel"], config["hideWhenEmpty"] = want == "hide", want == "hide_if_empty"
		encoded, err := json.Marshal(config)
		if err != nil {
			return false, err
		}
		next := graphProperty{ID: id, DataSourceID: dataSourceID, Type: property.Type, Name: property.Name, Position: property.Position, Config: encoded}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &next})
	}
	if len(operations) == 0 {
		return true, nil
	}
	slices.SortFunc(operations, func(a, b graphOperation) int { return strings.Compare(a.Property.ID, b.Property.ID) })
	if err := matcher.apply(pageID, operations); err != nil {
		return false, ignoreNotionMigrationFormatConflict(err)
	}
	return true, nil
}

func (matcher *notionMigrationFormatsMatcher) apply(pageID string, operations []graphOperation) error {
	batch, err := loadStructuralMutationBatch(matcher.app, pageID, matcher.batches)
	if err != nil {
		return err
	}
	references, err := matcher.linkedViewReferences(pageID, operations)
	if err != nil {
		return err
	}
	return applyNotionGraphOperationsWith(matcher.app, batch, matcher.payload.AuthorityOwnerID, matcher.job.WorkspaceID,
		pageID, matcher.job.Owner, operations, references)
}

// view gives a view Notion's groups, wrap and rows' order, and a form its
// questions.
func (matcher *notionMigrationFormatsMatcher) view(notionID string, format *notionMigrationViewFormat) error {
	format.Left, format.Matched, format.Unsaved = nil, false, false
	viewID, err := matcher.mapped(notionImportViewSourceID(format.Database, notionID), "view")
	if err != nil || viewID == "" {
		return err
	}
	var pageID string
	if err := matcher.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": viewID}).Row(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	view, found, err := loadDatabaseViewRecord(matcher.app, pageID, viewID)
	if err != nil || !found {
		return err
	}
	config := maps.Clone(view.Config)
	if config == nil {
		config = map[string]any{}
	}
	if format.Groups != nil {
		if !matcher.groups(matcher.viewDatabase(format.Database, view.DataSourceID), view, format.Groups, config) {
			format.Left = append(format.Left, "groups")
		}
	}
	if format.Groups != nil && format.HideEmpty != nil {
		config["hideEmptyGroups"] = *format.HideEmpty
	}
	if format.Colors != nil && view.Type == graphViewTypeBoard {
		delete(config, "boardColumnColors")
		if !*format.Colors {
			config["boardColumnColors"] = false
		}
	}
	if format.Wrap != nil {
		config["wrap"] = *format.Wrap
	}
	opened := len(matcher.forms)
	if format.Form != nil && view.Type == graphViewTypeForm {
		format.Left = append(format.Left, matcher.form(format.Database, pageID, viewID, view, *format.Form, config)...)
	}
	if err := matcher.viewDefaultTemplate(*format, config); err != nil {
		return err
	}
	if err := matcher.viewRows(view.Type, format, config); err != nil {
		return err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	next := &graphDatabaseView{ID: viewID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
		Name: view.Name, Position: view.Position, Config: encoded}
	if err := matcher.apply(pageID, []graphOperation{{Kind: graphOperationKindUpsertView, View: next}}); err != nil {
		if ignored := ignoreNotionMigrationFormatConflict(err); ignored != nil {
			return ignored
		}
		// Nothing read of it came, a form's link not opened either.
		matcher.forms = matcher.forms[:opened]
		format.Left, format.Unsaved = notionMigrationViewUnsavedLeft(*format), true
		return nil
	}
	format.Matched = len(format.Left) == 0
	return nil
}

// groups orders a view's groups as Notion's and hides those Notion hides:
// an option's or status's group by its name in Notion, the group for no
// value by its own key. It reports whether every group found its own.
func (matcher *notionMigrationFormatsMatcher) groups(
	databaseID string, view databaseViewAuthority, groups []notionMigrationGroupFormat, config map[string]any,
) bool {
	groupBy, _ := config["groupBy"].(string)
	if groupBy == "" || len(groups) == 0 {
		return len(groups) == 0
	}
	byNotion, err := matcher.properties(databaseID)
	if err != nil || byNotion[notionImportPropertyKey(groups[0].Property)] != groupBy {
		return false
	}
	var configText string
	if err := matcher.app.DB().NewQuery(`SELECT [[config]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": groupBy}).Row(&configText); err != nil {
		return false
	}
	property := struct {
		Options []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"options"`
	}{}
	if json.Unmarshal([]byte(configText), &property) != nil {
		return false
	}
	notionNames, _ := notionStagedOptionNames(matcher.app, matcher.job.ID, notionPropertySourceID(databaseID, groups[0].Property))
	keyOf := map[string]string{}
	for _, option := range property.Options {
		keyOf[option.Name], keyOf[option.ID] = option.ID, option.ID
		if name := notionNames[option.ID]; name != "" {
			keyOf[name] = option.ID
		}
	}
	none := mcpBoardNoValueGroup
	if view.Type != graphViewTypeBoard {
		none = mcpTableNoValueGroup
	}
	order, hidden, all := []string{}, []string{}, true
	for _, group := range groups {
		key := none
		if group.Value != nil {
			name, _ := group.Value.(string)
			if key = keyOf[name]; key == "" {
				all = false
				continue
			}
		}
		if slices.Contains(order, key) {
			continue
		}
		order = append(order, key)
		if group.Hidden {
			hidden = append(hidden, key)
		}
	}
	// The groups Notion doesn't list keep the order they had, after.
	for _, raw := range mcpAnySlice(config["groupOrder"]) {
		if key, ok := raw.(string); ok && !slices.Contains(order, key) {
			order = append(order, key)
		}
	}
	config["groupOrder"], config["groupSort"], config["hiddenGroups"] = mcpAnyStrings(order), "manual", mcpAnyStrings(hidden)
	return all
}
