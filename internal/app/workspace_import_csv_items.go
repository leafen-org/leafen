package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/tools/security"
)

// workspaceImportZipViewName is the view of a database a ZIP's import makes
// — a CSV's and the log's unused attachments — as Notion's, whatever the
// person's language (seen 2026-11-12 in a Korean screen).
const workspaceImportZipViewName = "Show All"

// buildWorkspaceImportCSVItems gives a CSV's database — its table, its
// properties with the options its rows name, its view and its block — then
// its rows, one at a time: a first pass over the rows finds the options, a
// second makes each row as it is staged. A column's format is a date's chosen
// format or a number's ("percent"); a column chosen in the CSV window keeps
// its cells as that window does (workspaceImportCSVURL). Its data source
// takes dataSourceID when it is given (workspaceImportReservedDataSourceID).
func buildWorkspaceImportCSVItems(
	title, locale string, headers, types, formats []string, chosen bool, rows [][]string, reserved []workspaceImportReservedRow,
	dataSourceID string,
) (workspaceImportItemSource, error) {
	rows, rowIDs := workspaceImportCSVRowIDs(types, rows, reserved)
	if dataSourceID == "" {
		dataSourceID = security.RandomString(15)
	}
	viewID := security.RandomString(15)
	propertyIDs := make([]string, len(headers))
	columns := make([]*workspaceCSVMergeColumn, len(headers))
	for index := range headers {
		if types[index] == "" {
			continue
		}
		propertyIDs[index] = security.RandomString(15)
		columns[index] = &workspaceCSVMergeColumn{property: workspaceCSVMergeProperty{
			ID: propertyIDs[index], Type: types[index], Config: "{}",
		}}
	}
	rowItem := func(index int, cells []string) (workspaceImportStagedItem, error) {
		values := map[string]graphRowValue{}
		for column, property := range columns {
			value := ""
			if column < len(cells) {
				value = cells[column]
			}
			if property == nil || (types[column] != graphValueTypeTitle && strings.TrimSpace(value) == "") {
				continue
			}
			parsed, ok := property.value(value)
			var spans []graphTextSpan
			switch {
			case types[column] == graphValueTypeNumber && formats[column] == "percent":
				parsed, ok = workspaceImportCSVPercentValue(value)
			case types[column] == graphValueTypeURL && chosen:
				parsed, _ = json.Marshal(workspaceImportCSVURL(value))
			case types[column] == graphValueTypeTitle || types[column] == graphValueTypeRichText:
				// An export's cell mentioning a page the import brings
				// (workspace_import_csv_mentions.go).
				if plain, mentioned, found := workspaceImportCSVTextValue(types[column], value); found {
					parsed, _ = json.Marshal(plain)
					spans, ok = mentioned, true
				}
			}
			if ok {
				values[propertyIDs[column]] = graphRowValue{Type: types[column], Value: parsed, Spans: spans}
			}
		}
		item := stagedWorkspaceImportOperation(graphOperation{
			Kind: graphOperationKindUpsertRow,
			Row: &graphDatabaseRow{
				ID: rowIDs[index], DataSourceID: dataSourceID,
				Position: workspaceImportPosition(index), Values: values,
			},
		})
		return item, normalizeWorkspaceImportCSVItems([]workspaceImportStagedItem{item})
	}
	// The options the rows name, before their properties are written.
	for index, cells := range rows {
		if _, err := rowItem(index, cells); err != nil {
			return nil, err
		}
	}
	items := make([]workspaceImportStagedItem, 0, 3+len(headers))
	items = append(items, stagedWorkspaceImportOperation(graphOperation{
		Kind:       graphOperationKindUpsertDataSource,
		DataSource: &graphDataSource{ID: dataSourceID, Title: title},
	}))
	shown := []string{}
	for _, index := range workspaceImportCSVColumnOrder(types, chosen) {
		header := headers[index]
		if columns[index] == nil {
			continue
		}
		config := map[string]any{}
		switch {
		case types[index] == graphValueTypeNumber && formats[index] != "":
			config["numberFormat"] = formats[index]
		case types[index] == graphValueTypeNumber:
			config["numberFormat"] = "number"
		case types[index] == graphValueTypeDate && formats[index] != "":
			config["dateFormat"] = formats[index]
		}
		if len(columns[index].added) > 0 {
			var err error
			if config, err = appendWorkspaceCSVMergeOptions(config, columns[index].added); err != nil {
				return nil, workspaceImportInputError()
			}
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		items = append(items, stagedWorkspaceImportOperation(graphOperation{
			Kind: graphOperationKindUpsertProperty,
			Property: &graphProperty{
				ID: propertyIDs[index], DataSourceID: dataSourceID, Type: types[index],
				Name: header, Position: workspaceImportPosition(len(shown)), Config: encoded,
			},
		}))
		shown = append(shown, propertyIDs[index])
	}
	// The CSV window's table is named in the person's language as its own
	// name, as Notion's ("표" in its name field and through its API, seen
	// 2026-11-12 and 2026-11-17); one a ZIP brings is "Show All" in any
	// language.
	viewName := workspaceImportZipViewName
	if chosen {
		viewName = "Table"
		if locale == workspaceImportLocaleKorean {
			viewName = "표"
		}
	}
	// Every column of an imported CSV starts at one width, as Notion's do
	// (seen 2026-11-04 in the audit's side by side).
	widths := make(map[string]int, len(shown))
	for _, propertyID := range shown {
		widths[propertyID] = workspaceImportCSVColumnWidth
	}
	// The table places the rows in the CSV's order (importViewBase).
	settings := importViewBase(rowIDs)
	titleName := ""
	if title := slices.Index(types, graphValueTypeTitle); title >= 0 {
		titleName = headers[title]
	}
	viewOrder := workspaceImportCSVViewOrder(shown, titleName, chosen)
	settings["propertyOrder"], settings["visibleProperties"], settings["propertyWidths"] = viewOrder, viewOrder, widths
	config, _ := json.Marshal(settings)
	items = append(items, stagedWorkspaceImportOperation(graphOperation{
		Kind: graphOperationKindUpsertView,
		View: &graphDatabaseView{
			ID: viewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
			Name: viewName, Position: workspaceImportPosition(0), Config: config,
		},
	}))
	// The page is the database, as Notion's CSV import makes a full-page
	// database (seen 2026-10-01): its body is the database's block.
	content, _ := json.Marshal(map[string]any{"dataSourceId": dataSourceID, "viewId": viewID})
	items = append(items, stagedWorkspaceImportOperation(graphOperation{
		Kind: graphOperationKindUpsertBlock,
		Block: &graphBlock{
			ID: security.RandomString(15), Position: "p0000000000", Type: graphBlockTypeInlineDatabase, Content: content,
		},
	}))
	if err := normalizeWorkspaceImportCSVItems(items); err != nil {
		return nil, err
	}
	return func(emit func(workspaceImportStagedItem) error) error {
		for _, item := range items {
			if err := emit(item); err != nil {
				return err
			}
		}
		for index, cells := range rows {
			item, err := rowItem(index, cells)
			if err != nil {
				return err
			}
			if err := emit(item); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

// workspaceImportCSVRowIDs gives each row its ID. An export brought back
// (Notion's Markdown & CSV, and Leafen's) has a page for each of a database's
// rows beside its CSV, whose body is the row's: the row of each page's title,
// in order, takes the ID the batch gave that page. A page no row has the
// title of is an untitled row's, its file named "Untitled" in the export's
// language ("제목 없음", seen 2026-10-09), or else becomes a row of its own,
// so no page's body is lost.
func workspaceImportCSVRowIDs(types []string, rows [][]string, reserved []workspaceImportReservedRow) ([][]string, []string) {
	titleColumn := slices.Index(types, graphValueTypeTitle)
	if titleColumn < 0 {
		reserved = nil
	}
	waiting := map[string][]string{}
	for _, row := range reserved {
		title := strings.TrimSpace(row.Title)
		waiting[title] = append(waiting[title], row.PageID)
	}
	// A title mentioning a page names its row's page so
	// (workspaceImportCSVTitleName).
	ids := make([]string, len(rows), len(rows)+len(reserved))
	untitled := []int{}
	for index, cells := range rows {
		title := ""
		if titleColumn >= 0 && titleColumn < len(cells) {
			title = workspaceImportCSVTitleName(cells[titleColumn])
		}
		if pages := waiting[title]; len(pages) > 0 {
			ids[index], waiting[title] = pages[0], pages[1:]
		} else if title == "" {
			untitled = append(untitled, index)
		}
	}
	for _, row := range reserved {
		title := strings.TrimSpace(row.Title)
		pages := waiting[title]
		if len(pages) == 0 || pages[0] != row.PageID {
			continue
		}
		waiting[title] = pages[1:]
		if len(untitled) > 0 {
			ids[untitled[0]], untitled = row.PageID, untitled[1:]
			continue
		}
		cells := make([]string, len(types))
		cells[titleColumn] = title
		rows = append(rows, cells)
		ids = append(ids, row.PageID)
	}
	for index := range ids {
		if ids[index] == "" {
			ids[index] = newPageID()
		}
	}
	return rows, ids
}

func workspaceImportCSVValue(valueType, value string) (json.RawMessage, bool) {
	var parsed any = value
	switch valueType {
	// A title or text longer than its cell holds keeps its first characters.
	case graphValueTypeTitle:
		parsed = graphTextWithin(strings.TrimSpace(value), maxGraphTitleRunes)
	case graphValueTypeRichText:
		parsed = graphTextWithin(value, maxGraphTextCharacters)
	case graphValueTypeNumber:
		var ok bool
		parsed, ok = parseWorkspaceImportCSVNumber(value)
		if !ok {
			return nil, false
		}
	case graphValueTypeCheckbox:
		var ok bool
		parsed, ok = parseWorkspaceImportCSVBoolean(value)
		if !ok {
			return nil, false
		}
	case graphValueTypeDate:
		// A day with its time when it has one, else the day it names.
		start, ok := workspaceImportCSVDateTime(value)
		if !ok {
			start, ok = parseWorkspaceImportCSVDate(value)
		}
		if !ok {
			return nil, false
		}
		parsed = map[string]any{"start": start}
	}
	encoded, err := json.Marshal(parsed)
	return encoded, err == nil
}

func normalizeWorkspaceImportCSVItems(items []workspaceImportStagedItem) error {
	for _, item := range items {
		if item.Operation == nil {
			return workspaceImportInputError()
		}
		var err error
		switch item.Operation.Kind {
		case graphOperationKindUpsertDataSource:
			err = normalizeGraphDataSource(item.Operation.DataSource)
		case graphOperationKindUpsertProperty:
			err = normalizeGraphProperty(item.Operation.Property)
		case graphOperationKindUpsertView:
			err = normalizeGraphView(item.Operation.View)
		case graphOperationKindUpsertBlock:
			err = normalizeGraphBlock(item.Operation.Block)
		case graphOperationKindUpsertRow:
			err = normalizeGraphRow(item.Operation.Row, nil)
		default:
			err = workspaceImportInputError()
		}
		if err != nil {
			return workspaceImportInputError()
		}
	}
	return nil
}

// workspaceImportCSVColumnOrder is the order of a CSV's database's
// properties, and of its table's columns, as Notion's imports make them (seen
// 2026-11-12). A CSV set up in the CSV window (chosen) puts its title first
// and the rest in the CSV's order; a CSV in a ZIP keeps the CSV's order, its
// title where it stands ("Count,Name,Zeta" shows Count, Name, Zeta).
func workspaceImportCSVColumnOrder(types []string, chosen bool) []int {
	title := -1
	if chosen {
		title = slices.Index(types, graphValueTypeTitle)
	}
	order := make([]int, 0, len(types))
	if title >= 0 {
		order = append(order, title)
	}
	for index := range types {
		if index != title {
			order = append(order, index)
		}
	}
	return order
}

// workspaceImportCSVViewOrder is the order its table shows them in: the
// properties' own — but the window's title named "Name", the name Notion's new
// database gives its title, comes after the rest, as Notion's CSV window
// makes it (seen 2026-11-12: "Name,Count" shows Count, Name; "이름,Count",
// "Item,Count" and a ZIP's "Name,Count" show their title first).
func workspaceImportCSVViewOrder(shown []string, titleName string, chosen bool) []string {
	if !chosen || titleName != "Name" || len(shown) == 0 {
		return shown
	}
	return append(slices.Clone(shown[1:]), shown[0])
}

func stagedWorkspaceImportOperation(operation graphOperation) workspaceImportStagedItem {
	return workspaceImportStagedItem{Operation: &operation}
}
