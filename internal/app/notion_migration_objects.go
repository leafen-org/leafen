package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A property the plan turns into objects — a text, an option or people — becomes a
// relation to the plan's database: each name a row holds, the text, each
// option's or person's name, points at that database's row of that title,
// made when there is none, with an ID its name gives so a run again finds
// it. A name more than one row has is pointed at the row the plan's values
// name for it (from the name to the row's Notion page); else it isn't, and
// the name stays as text in a column beside the relation, the person told
// in the import's notes to settle it. The relation is made in the link
// phase, after every row.

// objectColumn is the plan's entry for a property it turns into objects.
func (run *notionMigrationRunPlan) objectColumn(propertySource string) (notionMigrationPlanProperty, bool) {
	if run == nil {
		return notionMigrationPlanProperty{}, false
	}
	databaseID, propertyID, found := strings.Cut(propertySource, ":property:")
	if !found {
		return notionMigrationPlanProperty{}, false
	}
	entry, planned := run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)]
	return entry, planned && entry.Action == "object"
}

// linkedLater says whether a row's property is made in the link phase:
// one the plan turns into objects.
func (run *notionMigrationRunPlan) linkedLater(propertySource string) bool {
	_, object := run.objectColumn(propertySource)
	return object
}

// rowPlace is how a row of its own database is made: nil, but for a plan
// turning properties into objects or giving options values.
func (run *notionMigrationRunPlan) rowPlace(app core.App, jobID string) *notionRowPlace {
	if !run.hasObjects() && !run.hasOptionValues() {
		return nil
	}
	return &notionRowPlace{Run: run, Skip: run.linkedLater, Value: func(propertySource string, value notionDatabaseRowValuePlan) (notionDatabaseRowValuePlan, error) {
		return run.mergedOptionValue(app, jobID, propertySource, value)
	}}
}

// objectDatabases lists, as JSON, the staged databases with a property the
// plan turns into objects, whose rows the link phase links.
func (run *notionMigrationRunPlan) objectDatabases(app core.App, jobID string) (string, error) {
	staged := []string{}
	if run != nil {
		for key, columns := range run.columns {
			for _, entry := range columns {
				if entry.Action != "object" {
					continue
				}
				id, err := run.stagedTarget(app, jobID, key)
				if err != nil {
					return "", err
				}
				if id != "" {
					staged = append(staged, id)
				}
				break
			}
		}
	}
	encoded, err := json.Marshal(staged)
	return string(encoded), err
}

func (run *notionMigrationRunPlan) hasObjects() bool {
	if run == nil {
		return false
	}
	for _, columns := range run.columns {
		for _, entry := range columns {
			if entry.Action == "object" {
				return true
			}
		}
	}
	return false
}

// notionObjectRowID is the ID of the row a name becomes in a data source: a
// row is a page, so a page's (derivedPageID). A row a run made before rows
// took page IDs has the 15 digits notionObjectOlderRowID gives, and a run
// again finds it by them (notionObjectMadeRowID).
func notionObjectRowID(dataSourceID, name string) string {
	return derivedPageID("notion-object", dataSourceID, name)
}

func notionObjectOlderRowID(dataSourceID, name string) string {
	digest := sha256.Sum256([]byte("leafen:notion-object\x00" + dataSourceID + "\x00" + name))
	return hex.EncodeToString(digest[:])[:15]
}

// notionObjectMadeRowID is the ID a name's row takes when no row has its
// title: the one an earlier run made for it, in the trash or retitled since,
// which takes its name again as it did, or else a new one's.
func notionObjectMadeRowID(app core.App, dataSourceID, name string) (string, error) {
	older := notionObjectOlderRowID(dataSourceID, name)
	var found int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]]={:id} AND [[data_source_id]]={:dataSourceId}
	`).Bind(dbx.Params{"id": older, "dataSourceId": dataSourceID}).Row(&found); err != nil {
		return "", err
	}
	if found > 0 {
		return older, nil
	}
	return notionObjectRowID(dataSourceID, name), nil
}

// objectTarget is the data source the plan's objects of a property go to:
// its database, or the one it merges into.
func (linker *notionImportLinker) objectTarget(entry notionMigrationPlanProperty) (string, error) {
	into := entry.Database
	if target, merged := linker.run.mergeTarget(into); merged {
		into = target
	}
	staged, err := linker.run.stagedTarget(linker.app, linker.job.ID, into)
	if err != nil || staged == "" {
		return "", err
	}
	return linker.lookup(staged, "data_source")
}

// objectColumn is the relation a property turned into objects is in the
// data source a row is in: made once, under the plan's name. A merged
// database's, or a split's in the target its rows go to, stands for the
// target's relation of that name to the same database, or another merged or
// split database's there, as a merged relation does
// (notion_migration_merge.go) — made for both when another's objects are it.
func (linker *notionImportLinker) objectColumn(source storedNotionSource, entry notionMigrationPlanProperty, key, dataSourceID string) (string, error) {
	if nativeID, err := linker.propertyID(key); err != nil || nativeID != "" {
		return nativeID, err
	}
	target, err := linker.objectTarget(entry)
	if err != nil || target == "" {
		return "", err
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = strings.TrimSpace(source.Title)
	}
	into, err := linker.objectInto(source, key, dataSourceID)
	if err != nil {
		return "", err
	}
	if into != "" {
		column, err := linker.run.mergeColumn(source.ParentSourceID, source)
		if err != nil {
			return "", err
		}
		matched, err := linker.run.targetColumn(linker.app, linker.job.ID, column, into, source.ParentSourceID,
			func(candidate storedNotionSource, key string, own bool) (string, error) {
				nativeID, err := linker.propertyID(key)
				if err != nil || nativeID != "" {
					return nativeID, err
				}
				if _, object := linker.run.objectColumn(candidate.SourceID); object {
					return linker.relationColumn(key, dataSourceID, name, target)
				}
				if !own {
					return "", nil
				}
				if err := linker.property(candidate); err != nil {
					return "", err
				}
				return linker.propertyID(candidate.SourceID)
			})
		if err != nil {
			return "", err
		}
		if matched != "" {
			linker.linked[key] = matched
			return matched, linker.mappings.insert(key, matched, "property")
		}
	}
	return linker.relationColumn(key, dataSourceID, name, target)
}

// objectInto is the target a property turned into objects has its column in
// beside other databases' — its merged database's, or the split's that
// sends rows to dataSourceID when key is its column there — "" for its own
// database's column.
func (linker *notionImportLinker) objectInto(source storedNotionSource, key, dataSourceID string) (string, error) {
	if into, merged := linker.run.mergeTarget(source.ParentSourceID); merged {
		if key != source.SourceID {
			return "", nil
		}
		return into, nil
	}
	if key != notionSplitColumnSource(source.SourceID, dataSourceID) {
		return "", nil
	}
	return linker.splitInto(source.ParentSourceID, dataSourceID)
}

// objectNames reads the names a row's value holds.
func (linker *notionImportLinker) objectNames(propertySource string, value notionDatabaseRowValuePlan) ([]string, error) {
	names := []string{}
	add := func(name string) {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	switch value.Type {
	case graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect:
		plan, err := linker.stagedProperty(propertySource)
		if err != nil || plan == nil {
			return nil, err
		}
		options := map[string]string{}
		list, _ := plan.Config["options"].([]any)
		for _, option := range list {
			fields, _ := option.(map[string]any)
			id, _ := fields["id"].(string)
			name, _ := fields["name"].(string)
			options[id] = name
		}
		ids := []any{value.Value}
		if many, isList := value.Value.([]any); isList {
			ids = many
		}
		for _, id := range ids {
			if text, ok := id.(string); ok {
				add(options[text])
			}
		}
	case graphValueTypePerson:
		people, _ := value.Value.([]any)
		for _, person := range people {
			fields, _ := person.(map[string]any)
			name, _ := fields["displayName"].(string)
			add(name)
		}
	default:
		if text, ok := value.Value.(string); ok {
			add(text)
		}
	}
	return names, nil
}

// objectRow is the row a name points at in a data source: the one row of
// that title, the one the plan's values choose, or one made for it; ""
// when several have the title and the plan chooses none.
func (linker *notionImportLinker) objectRow(dataSourceID, name string, chosen map[string]string) (string, error) {
	if page, ok := chosen[name]; ok {
		return linker.lookup(page, "row")
	}
	var title string
	if err := linker.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}} WHERE [[data_source_id]]={:dataSourceId} AND [[type]]='title' LIMIT 1
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&title); err != nil {
		return "", err
	}
	rows := []string{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[row]].[[id]] FROM {{leafen_database_rows}} [[row]]
		JOIN {{leafen_database_row_values}} [[value]] ON [[value]].[[row_id]]=[[row]].[[id]] AND [[value]].[[property_id]]={:title}
		WHERE [[row]].[[data_source_id]]={:dataSourceId} AND json_valid([[value]].[[value]])
			AND trim(json_extract([[value]].[[value]], '$'))={:name} AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		ORDER BY [[row]].[[id]] LIMIT 2
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "title": title, "name": name}).Column(&rows); err != nil {
		return "", err
	}
	switch len(rows) {
	case 1:
		return rows[0], nil
	case 2:
		return "", nil
	}
	pageID, err := linker.dataSourcePage(dataSourceID)
	if err != nil {
		return "", err
	}
	batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
	if err != nil {
		return "", err
	}
	rowID, err := notionObjectMadeRowID(linker.app, dataSourceID, name)
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(name)
	if err := applyNotionGraphOperationsWith(linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, linker.job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: rowID, DataSourceID: dataSourceID, Position: "z" + rowID,
			Values: map[string]graphRowValue{title: {Type: graphValueTypeTitle, Value: encoded}},
		}}}, notionGraphApplyReferences{},
	); err != nil {
		return "", err
	}
	return rowID, nil
}

// keptObjectNames keeps, beside the relation, the names of a row that
// point at no one row.
func (linker *notionImportLinker) keptObjectNames(key, dataSourceID, columnName string, names []string) (string, graphRowValue, error) {
	keptKey := key + ":kept"
	propertyID, err := linker.propertyID(keptKey)
	if err != nil {
		return "", graphRowValue{}, err
	}
	if propertyID == "" {
		// In the language the move's owner reads its report in (serverTextLanguage).
		suffix := notionMigrationKeptSuffixes[0]
		if language, err := serverTextLanguage(linker.app, linker.job.Owner, nil); err == nil && language == accountLanguageKo {
			suffix = notionMigrationKeptSuffixes[1]
		}
		pageID, err := linker.dataSourcePage(dataSourceID)
		if err != nil {
			return "", graphRowValue{}, err
		}
		batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
		if err != nil {
			return "", graphRowValue{}, err
		}
		propertyID = notionImportLinkedPropertyID(linker.job.ID, keptKey)
		if err := applyNotionGraphOperationsWith(linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, linker.job.Owner,
			[]graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
				ID: propertyID, DataSourceID: dataSourceID, Type: graphValueTypeRichText, Name: columnName + suffix,
				Position: "z" + propertyID, Config: json.RawMessage(`{}`),
			}}}, notionGraphApplyReferences{},
		); err != nil {
			return "", graphRowValue{}, err
		}
		linker.linked[keptKey] = propertyID
		if err := linker.mappings.insert(keptKey, propertyID, "property"); err != nil {
			return "", graphRowValue{}, err
		}
	}
	encoded, _ := json.Marshal(strings.Join(names, ", "))
	return propertyID, graphRowValue{Type: graphValueTypeRichText, Value: encoded}, nil
}

// recordNotionObjectAmbiguous notes, for the person to settle, a name more
// than one row of the plan's database has.
func recordNotionObjectAmbiguous(app core.App, jobID, propertySource, name string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_sources}}
			([[job_id]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]],
			 [[text]], [[blueprint]], [[discovery_status]], [[discovery_cursor]], [[discovery_position]], [[created]])
		SELECT [[job_id]], [[source_id]] || '#' || {:reason} || ':' || {:name}, 'issue', [[parent_source_id]], {:parentKind},
			[[position]], {:name}, {:reason}, 0, 'complete', '', 0, {:created}
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='property'
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sourceId": propertySource, "reason": notionImportIssueObjectAmbiguous, "name": name,
		"parentKind": notionImportPropertyIssueParentKind, "created": time.Now().UTC().UnixMilli(),
	}).Execute()
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// objectValues links a row's property turned into objects: the relation's
// values, and the names that point at no one row kept beside it. On a run
// again, either one the source no longer gives any of is emptied, as Notion
// holds it (notion_migration_rerun_emptied.go).
func (linker *notionImportLinker) objectValues(
	source storedNotionSource, entry notionMigrationPlanProperty, propertySource string, value notionDatabaseRowValuePlan,
	dataSourceID string, split bool, rowValues map[string]graphRowValue, externalRows map[string]graphEntityReference,
) error {
	key := propertySource
	if split {
		key = notionSplitColumnSource(propertySource, dataSourceID)
	}
	staged, found, err := notionImportStagedSource(linker.app, linker.job.ID, propertySource)
	if err != nil || !found {
		return err
	}
	columnID, err := linker.objectColumn(staged, entry, key, dataSourceID)
	if err != nil || columnID == "" {
		return err
	}
	target, err := linker.objectTarget(entry)
	if err != nil || target == "" {
		return err
	}
	names, err := linker.objectNames(propertySource, value)
	if err != nil {
		return err
	}
	chosen := map[string]string{}
	for _, choice := range entry.Values {
		chosen[strings.TrimSpace(choice.From)] = strings.TrimSpace(choice.To)
	}
	ids, kept := []string{}, []string{}
	seen := map[string]struct{}{}
	for _, name := range names {
		rowID, err := linker.objectRow(target, name, chosen)
		if err != nil {
			return err
		}
		if rowID == "" {
			kept = append(kept, name)
			if err := recordNotionObjectAmbiguous(linker.app, linker.job.ID, propertySource, name); err != nil {
				return err
			}
			continue
		}
		if _, linked := seen[rowID]; !linked {
			seen[rowID] = struct{}{}
			ids = append(ids, rowID)
		}
	}
	references, err := loadGraphReferences(linker.app, "leafen_database_rows", seen)
	if err != nil {
		return err
	}
	for id, reference := range references {
		externalRows[id] = reference
	}
	// Another property of the row may stand for the same column.
	if err := linker.relationValue(rowValues, columnID, ids); err != nil {
		return err
	}
	if len(kept) == 0 && linker.payload.MigrationRerunSince > 0 {
		keptID, err := linker.propertyID(key + ":kept")
		if _, written := rowValues[keptID]; err != nil || keptID == "" || written {
			return err
		}
		rowValues[keptID] = graphRowValue{Type: graphValueTypeRichText, Value: graphRowValueCleared}
	}
	if len(kept) > 0 {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = strings.TrimSpace(staged.Title)
		}
		keptID, keptValue, err := linker.keptObjectNames(key, dataSourceID, name, kept)
		if err != nil {
			return err
		}
		rowValues[keptID] = keptValue
	}
	return nil
}
