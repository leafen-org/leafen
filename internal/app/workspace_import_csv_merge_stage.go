package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	// workspaceCSVMergeOptionsKey marks a staged property that only adds
	// options; they join its options as they are when the import applies.
	workspaceCSVMergeOptionsKey = "csvMergeOptions"
	// workspaceCSVMergeShowKey marks a staged table that shows the import's
	// new properties after those it shows when the import applies.
	workspaceCSVMergeShowKey = "csvMergeShow"
)

// parseWorkspaceCSVMerge stages a CSV's rows for a database: the properties
// it makes, the options it adds and the tables that show its new properties,
// then a row for each CSV row after the header, or the header's too when the
// first row is kept as a row.
func parseWorkspaceCSVMerge(
	app core.App,
	ownerID, jobID string,
	admission workspaceImportAdmission,
) (workspaceImportItemSource, int, error) {
	records, err := readWorkspaceCSVMergeRecords(admission.Input.CSV)
	if err != nil {
		return nil, 0, err
	}
	dataSourceID := admission.MergeDataSourceID
	stored, err := loadWorkspaceCSVMergeProperties(app, dataSourceID)
	if err != nil {
		return nil, 0, err
	}
	created, err := workspaceCSVMergeNewProperties(app, dataSourceID, stored, admission.MergeNewProperties)
	if err != nil {
		return nil, 0, err
	}
	properties := make(map[string]workspaceCSVMergeProperty, len(stored)+len(created))
	for _, property := range append(slices.Clone(stored), created...) {
		properties[property.ID] = property
	}
	// The first row names the columns, and is a row too when it is kept.
	firstLine := 2
	if admission.Input.CSVKeepFirstRow {
		firstLine = 1
	}
	var workspaceID string
	if err := app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": admission.TargetPageID}).Row(&workspaceID); err != nil {
		return nil, 0, workspaceImportInputError()
	}
	mapped := make([]*workspaceCSVMergeColumn, len(admission.MergeColumns))
	columns := map[string]*workspaceCSVMergeColumn{}
	for index, raw := range admission.MergeColumns {
		propertyID := strings.TrimSpace(raw)
		if propertyID == "" {
			continue
		}
		property, found := properties[propertyID]
		_, writable := workspaceCSVMergeWritable[property.Type]
		if _, repeated := columns[propertyID]; !found || !writable || repeated {
			return nil, 0, workspaceImportInputError()
		}
		column, err := newWorkspaceCSVMergeColumn(app, ownerID, workspaceID, dataSourceID, len(records), property)
		if err != nil {
			return nil, 0, err
		}
		mapped[index], columns[propertyID] = column, column
	}
	if len(columns) == 0 {
		return nil, 0, workspaceImportInputError()
	}
	if firstLine == 2 {
		records = records[1:]
	}
	// A first pass over the rows finds the options and related rows they
	// name, and what refuses the merge; the rows are made again as they are
	// staged.
	if err := stageWorkspaceCSVMergeRows(app, dataSourceID, records, mapped, firstLine, nil); err != nil {
		return nil, 0, err
	}
	// The related rows a relation cell names that its database didn't have
	// come first, in their database's page.
	related := []workspaceImportStagedItem{}
	for _, column := range mapped {
		if column == nil || column.related == nil {
			continue
		}
		for _, row := range column.related.rows {
			item := stagedWorkspaceImportOperation(graphOperation{Kind: graphOperationKindUpsertRow, Row: &row})
			item.PageID = column.related.pageID
			related = append(related, item)
		}
	}
	items := []workspaceImportStagedItem{}
	for _, property := range created {
		column := columns[property.ID]
		if column == nil {
			return nil, 0, workspaceImportInputError()
		}
		config := map[string]any{}
		if err := json.Unmarshal([]byte(property.Config), &config); err != nil {
			return nil, 0, workspaceImportInputError()
		}
		if len(column.added) > 0 {
			if config, err = appendWorkspaceCSVMergeOptions(config, column.added); err != nil {
				return nil, 0, workspaceImportInputError()
			}
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, stagedWorkspaceImportOperation(graphOperation{
			Kind: graphOperationKindUpsertProperty,
			Property: &graphProperty{
				ID: property.ID, DataSourceID: dataSourceID, Type: property.Type,
				Name: property.Name, Position: property.Position, Config: encoded,
			},
		}))
	}
	for _, property := range stored {
		column := columns[property.ID]
		if column == nil || len(column.added) == 0 {
			continue
		}
		encoded, err := json.Marshal(map[string]any{workspaceCSVMergeOptionsKey: column.added})
		if err != nil {
			return nil, 0, err
		}
		items = append(items, stagedWorkspaceImportOperation(graphOperation{
			Kind: graphOperationKindUpsertProperty,
			Property: &graphProperty{
				ID: property.ID, DataSourceID: dataSourceID, Type: property.Type,
				Name: property.Name, Position: property.Position, Config: encoded,
			},
		}))
	}
	shown, err := stageWorkspaceCSVMergeTables(app, admission.TargetPageID, dataSourceID, created)
	if err != nil {
		return nil, 0, err
	}
	before := append(append(items, shown...), related...)
	return func(emit func(workspaceImportStagedItem) error) error {
		for _, item := range before {
			if err := emit(item); err != nil {
				return err
			}
		}
		// The import's rows take its own IDs, in order.
		next := 0
		return stageWorkspaceCSVMergeRows(app, dataSourceID, records, mapped, firstLine, func(item workspaceImportStagedItem) error {
			operation, row := *item.Operation, *item.Operation.Row
			row.ID = workspaceCSVMergeRowID(jobID, next)
			operation.Row = &row
			item.Operation = &operation
			next++
			return emit(item)
		})
	}, len(records), nil
}

func readWorkspaceCSVMergeRecords(value string) ([][]string, error) {
	reader := newWorkspaceImportCSVReader(value)
	records := [][]string{}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, workspaceImportInputError()
		}
		if workspaceImportCSVRowHasValue(record) {
			records = append(records, record)
		}
	}
	if len(records) < 2 {
		return nil, workspaceImportInputError()
	}
	return records, nil
}

// workspaceCSVMergeNewProperties are the properties the import makes for
// its columns, after the database's own.
func workspaceCSVMergeNewProperties(
	app core.App,
	dataSourceID string,
	stored []workspaceCSVMergeProperty,
	requested []workspaceCSVMergeNewProperty,
) ([]workspaceCSVMergeProperty, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	tail := ""
	for _, property := range stored {
		tail = max(tail, property.Position)
	}
	base, err := databasePositionAfter(tail)
	if err != nil || len(base)+4 > maxGraphPositionLength {
		return nil, workspaceImportInputError()
	}
	created := make([]workspaceCSVMergeProperty, 0, len(requested))
	seen := map[string]struct{}{}
	for index, request := range requested {
		if _, allowed := workspaceCSVMergeNewTypes[strings.TrimSpace(request.Type)]; !allowed {
			return nil, workspaceImportInputError()
		}
		config := request.Config
		if config == nil {
			config = map[string]any{}
		}
		encoded, err := json.Marshal(config)
		property := graphProperty{
			ID: request.ID, DataSourceID: dataSourceID, Type: request.Type, Name: request.Name,
			Position: fmt.Sprintf("%s%04d", base, index), Config: encoded,
		}
		if err != nil || normalizeGraphProperty(&property) != nil {
			return nil, workspaceImportInputError()
		}
		if _, repeated := seen[property.ID]; repeated {
			return nil, workspaceImportInputError()
		}
		seen[property.ID] = struct{}{}
		var exists int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": property.ID}).Row(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			return nil, workspaceImportInputError()
		}
		created = append(created, workspaceCSVMergeProperty{
			ID: property.ID, Type: property.Type, Name: property.Name,
			Position: property.Position, Config: string(property.Config),
		})
	}
	return created, nil
}

// stageWorkspaceCSVMergeTables stages the database's tables that list the
// properties they show, which show its new properties too, as Notion's
// tables show a property the import adds.
func stageWorkspaceCSVMergeTables(
	app core.App,
	pageID, dataSourceID string,
	created []workspaceCSVMergeProperty,
) ([]workspaceImportStagedItem, error) {
	if len(created) == 0 {
		return nil, nil
	}
	ids := make([]string, len(created))
	for index, property := range created {
		ids[index] = property.ID
	}
	views := []struct {
		ID          string `db:"id"`
		ContainerID string `db:"container_id"`
		Name        string `db:"name"`
		Position    string `db:"position"`
		Config      string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[container_id]], [[name]], [[position]], [[config]] FROM {{leafen_database_views}}
		WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId} AND [[type]] = {:table}
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": pageID, "dataSourceId": dataSourceID, "table": graphViewTypeTable}).All(&views); err != nil {
		return nil, err
	}
	items := []workspaceImportStagedItem{}
	for _, view := range views {
		config := map[string]any{}
		if json.Unmarshal([]byte(view.Config), &config) != nil {
			continue
		}
		if _, lists := config["visibleProperties"].([]any); !lists {
			continue
		}
		encoded, err := json.Marshal(map[string]any{workspaceCSVMergeShowKey: ids})
		if err != nil {
			return nil, err
		}
		items = append(items, stagedWorkspaceImportOperation(graphOperation{
			Kind: graphOperationKindUpsertView,
			View: &graphDatabaseView{
				ID: view.ID, DataSourceID: dataSourceID, ContainerID: view.ContainerID, Type: graphViewTypeTable,
				Name: view.Name, Position: view.Position, Config: encoded,
			},
		}))
	}
	return items, nil
}

// stageWorkspaceCSVMergeRows stages a row for each CSV row, after the
// database's last row in the CSV's order.
func stageWorkspaceCSVMergeRows(
	app core.App,
	dataSourceID string,
	records [][]string,
	mapped []*workspaceCSVMergeColumn,
	firstLine int,
	emit func(workspaceImportStagedItem) error,
) error {
	var tail string
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[row]].[[position]]), '') FROM {{leafen_database_rows}} AS [[row]] WHERE ` + databaseSourceRowsSQL("[[row]]", "{:dataSourceId}") + `
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&tail); err != nil {
		return err
	}
	base, err := databasePositionAfter(tail)
	if err != nil || len(base)+8 > maxGraphPositionLength {
		return workspaceImportInputError()
	}
	for index, cells := range records {
		values := map[string]graphRowValue{}
		for column, target := range mapped {
			if target == nil || column >= len(cells) || strings.TrimSpace(cells[column]) == "" {
				continue
			}
			if value, ok := target.value(cells[column]); ok {
				values[target.property.ID] = graphRowValue{Type: target.property.Type, Value: value}
			}
			// A new option longer than an option's name may be refuses the
			// merge, as Notion's fails (seen 2026-10-14).
			if target.tooLong {
				return workspaceImportInvalid(fmt.Sprintf("line %d: a new option for %q is longer than %d characters",
					firstLine+index, target.property.Name, maxDatabaseRowPageNewOptionNameRune))
			}
		}
		row := &graphDatabaseRow{
			ID: fmt.Sprintf("csv-merge-row-%d", index), DataSourceID: dataSourceID,
			Position: fmt.Sprintf("%s%08d", base, index), Values: values,
		}
		if err := normalizeGraphRow(row, nil); err != nil {
			return workspaceImportInputError()
		}
		if emit != nil {
			if err := emit(stagedWorkspaceImportOperation(graphOperation{Kind: graphOperationKindUpsertRow, Row: row})); err != nil {
				return err
			}
		}
	}
	return nil
}

type workspaceCSVMergeColumn struct {
	property workspaceCSVMergeProperty
	options  []notionDatabaseRowPageOption
	added    []notionDatabaseRowPageOption
	// titles are a relation's related rows by title, the first of each.
	titles map[string]string
	// people are a person property's workspace members by email and by a
	// name only one of them has.
	people map[string]string
	// related is where a relation cell's titles its database doesn't have
	// go, when the importer may add to it.
	related *workspaceCSVMergeRelated
	// tooLong marks a cell naming a new option longer than an option's name
	// may be.
	tooLong bool
}

// workspaceCSVMergeRelated is a related database a merge adds rows to: a
// relation cell's title its database doesn't have becomes a new row of it
// with that title, as Notion's merge makes one (seen 2026-10-14).
type workspaceCSVMergeRelated struct {
	pageID, dataSourceID, titleID, base string
	next                                int
	rows                                []graphDatabaseRow
}

func newWorkspaceCSVMergeColumn(
	app core.App,
	ownerID, workspaceID, dataSourceID string,
	records int,
	property workspaceCSVMergeProperty,
) (*workspaceCSVMergeColumn, error) {
	column := &workspaceCSVMergeColumn{property: property}
	switch property.Type {
	case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
		options, err := notionDatabaseRowPageOptions(storedNotionDatabaseRowPageProperty{Config: property.Config})
		if err != nil {
			return nil, workspaceImportInputError()
		}
		column.options = options
	case graphValueTypeRelation:
		target := graphRelationTarget(property.Type, json.RawMessage(property.Config))
		titles, err := workspaceCSVMergeRelatedTitles(app, ownerID, target)
		if err != nil {
			return nil, err
		}
		column.titles = titles
		if column.related, err = workspaceCSVMergeRelatedDatabase(app, ownerID, workspaceID, target); err != nil {
			return nil, err
		}
		// A database related to itself gets its new rows after the merge's.
		if column.related != nil && target == dataSourceID {
			column.related.next = records
		}
	case graphValueTypePerson:
		people, err := workspaceCSVMergePeople(app, property.ID)
		if err != nil {
			return nil, err
		}
		column.people = people
	}
	return column, nil
}

// workspaceCSVMergePeople are the members of a property's workspace by
// lowercased email, and by lowercased name where only one has it, as a
// person cell names them. Notion leaves a cell naming no member empty (seen
// 2026-10-01).
func workspaceCSVMergePeople(app core.App, propertyID string) (map[string]string, error) {
	members := []struct {
		ID    string `db:"id"`
		Name  string `db:"name"`
		Email string `db:"email"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[user]].[[id]], [[user]].[[name]], [[user]].[[email]]
		FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]]=[[property]].[[workspace_id]]
		JOIN {{leafen_organization_memberships}} AS [[organization_member]]
			ON [[organization_member]].[[organization]]=[[workspace]].[[organization]] AND [[organization_member]].[[status]]='active'
		JOIN {{users}} AS [[user]] ON [[user]].[[id]]=[[organization_member]].[[user]]
		WHERE [[property]].[[id]]={:propertyId} AND ([[user]].[[id]]=[[workspace]].[[workspace_owner]] OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} AS [[member]] WHERE [[member]].[[workspace_id]]=[[workspace]].[[id]]
			AND [[member]].[[user_id]]=[[user]].[[id]] AND [[member]].[[role]]='member'))
	`).Bind(dbx.Params{"propertyId": propertyID}).All(&members); err != nil {
		return nil, err
	}
	people, names := map[string]string{}, map[string][]string{}
	for _, member := range members {
		if email := strings.ToLower(strings.TrimSpace(member.Email)); email != "" {
			people[email] = member.ID
		}
		if name := strings.ToLower(strings.TrimSpace(member.Name)); name != "" {
			names[name] = append(names[name], member.ID)
		}
	}
	for name, ids := range names {
		if _, email := people[name]; !email && len(ids) == 1 {
			people[name] = ids[0]
		}
	}
	return people, nil
}

// workspaceCSVMergeRelatedTitles are the rows of a relation's database by
// title, when the importer can read them; none otherwise.
func workspaceCSVMergeRelatedTitles(app core.App, ownerID, dataSourceID string) (map[string]string, error) {
	titles := map[string]string{}
	var pageID string
	if dataSourceID == "" || app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&pageID) != nil {
		return titles, nil
	}
	if _, err := authorizePageBody(app, ownerID, pageID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return titles, nil
		}
		return nil, err
	}
	rows := []struct {
		ID    string `db:"id"`
		Value string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]] AS [[id]], [[cell]].[[value]] AS [[value]] FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_database_row_values}} AS [[cell]]
			ON [[cell]].[[row_id]] = [[row]].[[id]] AND [[cell]].[[type]] = {:title}
		WHERE [[row]].[[data_source_id]] = {:dataSourceId} AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		ORDER BY [[row]].[[position]], [[row]].[[id]]
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "title": graphValueTypeTitle}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		var title string
		if json.Unmarshal([]byte(row.Value), &title) != nil || strings.TrimSpace(title) == "" {
			continue
		}
		if _, known := titles[strings.TrimSpace(title)]; !known {
			titles[strings.TrimSpace(title)] = row.ID
		}
	}
	return titles, nil
}

// workspaceCSVMergeRelatedDatabase is a relation's database when the
// importer may add rows to it in the merge's workspace; none otherwise.
func workspaceCSVMergeRelatedDatabase(app core.App, ownerID, workspaceID, dataSourceID string) (*workspaceCSVMergeRelated, error) {
	related := &workspaceCSVMergeRelated{dataSourceID: dataSourceID}
	if dataSourceID == "" || app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&related.pageID) != nil {
		return nil, nil
	}
	authorization, err := authorizePageBody(app, ownerID, related.pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || (err == nil && authorization.Workspace.WorkspaceID != workspaceID) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[type]] = {:title} AND `+databasePropertyNotTrashedSQL("[[id]]")+`
		LIMIT 1
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "title": graphValueTypeTitle}).Row(&related.titleID) != nil {
		return nil, nil
	}
	var tail string
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[row]].[[position]]), '') FROM {{leafen_database_rows}} AS [[row]] WHERE ` + databaseSourceRowsSQL("[[row]]", "{:dataSourceId}") + `
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&tail); err != nil {
		return nil, err
	}
	if related.base, err = databasePositionAfter(tail); err != nil || len(related.base)+8 > maxGraphPositionLength {
		return nil, nil
	}
	return related, nil
}

// relatedRow is a new row of the related database with the title a cell
// names, made once however many cells name it.
func (column *workspaceCSVMergeColumn) relatedRow(name string) (string, bool) {
	related := column.related
	if related == nil || name == "" {
		return "", false
	}
	title, err := json.Marshal(name)
	if err != nil {
		return "", false
	}
	row := graphDatabaseRow{
		ID: security.RandomString(15), DataSourceID: related.dataSourceID,
		Position: fmt.Sprintf("%s%08d", related.base, related.next),
		Values:   map[string]graphRowValue{related.titleID: {Type: graphValueTypeTitle, Value: title}},
	}
	if normalizeGraphRow(&row, nil) != nil {
		return "", false
	}
	related.next++
	related.rows = append(related.rows, row)
	column.titles[name] = row.ID
	return row.ID, true
}

// value is a CSV cell as the column's property holds it.
func (column *workspaceCSVMergeColumn) value(text string) (json.RawMessage, bool) {
	var parsed any
	switch column.property.Type {
	case graphValueTypeSelect, graphValueTypeStatus:
		id, ok := column.option(strings.TrimSpace(text))
		if !ok {
			return nil, false
		}
		parsed = id
	case graphValueTypeMultiSelect, graphValueTypeRelation, graphValueTypePerson:
		ids := []string{}
		for _, part := range column.listedNames(text) {
			id, ok := "", false
			switch column.property.Type {
			case graphValueTypeRelation:
				name := workspaceCSVMergeRelationName(part)
				if id, ok = column.titles[name]; !ok {
					id, ok = column.relatedRow(name)
				}
			case graphValueTypePerson:
				id, ok = column.people[strings.ToLower(strings.TrimSpace(part))]
			default:
				id, ok = column.option(strings.TrimSpace(part))
			}
			// Every name a cell lists, however many.
			if ok && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, false
		}
		parsed = ids
	case graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		parsed = strings.TrimSpace(text)
	default:
		return workspaceImportCSVValue(column.property.Type, text)
	}
	encoded, err := json.Marshal(parsed)
	return encoded, err == nil
}

// workspaceCSVMergeRelationName is the title a relation cell's part names;
// Notion's export follows each with its link in parentheses.
func workspaceCSVMergeRelationName(part string) string {
	name := strings.TrimSpace(part)
	if open := strings.LastIndex(name, " (http"); open > 0 && strings.HasSuffix(name, ")") {
		name = strings.TrimSpace(name[:open])
	}
	return name
}

// option is the option a name names: one the property has, or a new one —
// a status's in its first group — in the default color, as Notion's merge
// adds it (seen 2026-10-14), however many the property has already (100 is
// the Notion API's limit for one request, not a property's).
func (column *workspaceCSVMergeColumn) option(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	for _, option := range column.options {
		if option.Name == name {
			return option.ID, true
		}
	}
	for _, option := range column.options {
		if strings.EqualFold(option.Name, name) {
			return option.ID, true
		}
	}
	if utf8.RuneCountInString(name) > maxDatabaseRowPageNewOptionNameRune {
		column.tooLong = true
		return "", false
	}
	option := notionDatabaseRowPageOption{ID: security.RandomString(15), Name: name, Color: "default"}
	column.options = append(column.options, option)
	column.added = append(column.added, option)
	return option.ID, true
}
