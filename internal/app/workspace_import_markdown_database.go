package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/yuin/goldmark/ast"
)

// A converted document's table holding a picture is an inline database at
// its place, as Notion's HTML import makes one (seen 2026-11-13, 24
// synthetic tables; htmlImportImageTables.ts writes it): its first cell's
// mark is [](leafen-table:database,…,title=<column>), its last column holds
// each row's pictures and the others its words. With "header-row" its
// first row names the columns — a nameless one by its number, one named
// again with " 1", " 2" … after it — and the pictures' column is "파일"
// (Files), or "파일 1" when a column has that name; without it every row is
// a row, the columns named by their numbers, the pictures' too. The title
// is the column the mark names, the others text; every row is a row, an
// empty one too, its cells keep their formatting, and its pictures are its
// files, an uploaded one (leafen-file:) the workspace's, any other the
// address written, named by it. Its table is "Show All", in the table's
// order, as a ZIP's database's (workspaceImportZipViewName).

// workspaceImportMarkdownDatabase is what an inline database brings besides
// its block: its data source, properties and view, staged before the
// block, and its rows, staged after it.
type workspaceImportMarkdownDatabase struct {
	setup, rows []graphOperation
}

// workspaceImportDatabaseColumnName is a column's name by its number, as
// Notion's HTML import names a database's columns without a header row
// (seen 2026-11-13: "1열", "2열" …; "Column 1" … in English).
func workspaceImportDatabaseColumnName(locale string, number int) string {
	if locale == workspaceImportLocaleKorean {
		return strconv.Itoa(number) + "열"
	}
	return "Column " + strconv.Itoa(number)
}

func workspaceImportDatabaseFilesName(locale string) string {
	if locale == workspaceImportLocaleKorean {
		return "파일"
	}
	return "Files"
}

// workspaceImportDatabaseNames are the columns' names, each its own: a name
// taken already gets " 1", then " 2" …, as Notion's HTML import names a
// second "같음" "같음 1" (seen 2026-11-13).
func workspaceImportDatabaseNames(names []string) []string {
	used := map[string]bool{}
	unique := make([]string, len(names))
	for index, name := range names {
		if utf8.RuneCountInString(name) > maxGraphIdentifierLength {
			name = string([]rune(name)[:maxGraphIdentifierLength])
		}
		base := name
		for number := 1; used[name]; number++ {
			name = base + " " + strconv.Itoa(number)
		}
		used[name], unique[index] = true, name
	}
	return unique
}

// database reads a table its mark makes an inline database into the
// database's block, its data source, properties, view and rows riding on it.
func (reader *workspaceImportMarkdownReader) database(rows [][]ast.Node, marks workspaceImportTableMarks, width, indent int) bool {
	// As many columns as a table can hold (table).
	if width > maxGraphTableWidth {
		return false
	}
	texts := width - 1
	title := min(max(marks.title, 0), texts-1)
	names := make([]string, width)
	body := rows
	for column := range names {
		if marks.row && len(rows) > 0 && column < len(rows[0]) && column < texts {
			names[column] = strings.TrimSpace(reader.plain(rows[0][column]))
		}
		if names[column] == "" {
			names[column] = workspaceImportDatabaseColumnName(reader.locale, column+1)
		}
	}
	if marks.row {
		names[texts] = workspaceImportDatabaseFilesName(reader.locale)
		if len(body) > 0 {
			body = body[1:]
		}
	}
	names = workspaceImportDatabaseNames(names)
	dataSourceID, viewID := security.RandomString(15), security.RandomString(15)
	database := &workspaceImportMarkdownDatabase{setup: []graphOperation{{
		Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: dataSourceID},
	}}}
	propertyIDs := make([]string, width)
	types := make([]string, width)
	for column := range propertyIDs {
		propertyIDs[column], types[column] = security.RandomString(15), graphValueTypeRichText
		switch column {
		case title:
			types[column] = graphValueTypeTitle
		case texts:
			types[column] = graphValueTypeFiles
		}
		database.setup = append(database.setup, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: propertyIDs[column], DataSourceID: dataSourceID, Type: types[column], Name: names[column],
			Position: workspaceImportPosition(column), Config: json.RawMessage(`{}`),
		}})
	}
	rowIDs := []string{}
	for _, cells := range body {
		values := map[string]graphRowValue{}
		for column := range width {
			var cell ast.Node
			if column < len(cells) {
				cell = cells[column]
			}
			if value, ok := reader.databaseValue(cell, types[column]); ok {
				values[propertyIDs[column]] = value
			}
		}
		rowIDs = append(rowIDs, security.RandomString(15))
		database.rows = append(database.rows, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: rowIDs[len(rowIDs)-1], DataSourceID: dataSourceID, Position: workspaceImportPosition(len(database.rows)), Values: values,
		}})
	}
	// The table shows its columns in the table's order and its rows in the
	// document's (importViewBase).
	settings := importViewBase(rowIDs)
	settings["propertyOrder"] = propertyIDs
	config, _ := json.Marshal(settings)
	database.setup = append(database.setup, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
		ID: viewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
		Name: workspaceImportZipViewName, Position: workspaceImportPosition(0), Config: config,
	}})
	content, _ := json.Marshal(map[string]string{"dataSourceId": dataSourceID, "viewId": viewID})
	staged := make([]workspaceImportStagedItem, 0, len(database.setup)+len(database.rows)+1)
	for _, operation := range append(append([]graphOperation{}, database.setup...), database.rows...) {
		staged = append(staged, stagedWorkspaceImportOperation(operation))
	}
	if normalizeWorkspaceImportCSVItems(staged) != nil {
		return false
	}
	reader.list.blocks = append(reader.list.blocks, workspaceImportParsedBlock{
		Type: graphBlockTypeInlineDatabase, Content: content, Indent: indent, Database: database,
	})
	return true
}

// databaseValue is a cell's value in its column: its words, formatted, in a
// title or text column — the title's always, if empty — and its pictures in
// the files column; none for a text or files cell holding nothing.
func (reader *workspaceImportMarkdownReader) databaseValue(cell ast.Node, valueType string) (graphRowValue, bool) {
	if valueType == graphValueTypeFiles {
		items := reader.databaseFiles(cell)
		if len(items) == 0 {
			return graphRowValue{}, false
		}
		encoded, _ := json.Marshal(items)
		return graphRowValue{Type: valueType, Value: encoded}, true
	}
	plain, spans := "", []graphTextSpan(nil)
	if cell != nil {
		plain, spans = reader.inline(cell)
	}
	if plain == "" && valueType != graphValueTypeTitle {
		return graphRowValue{}, false
	}
	bound := maxGraphTextCharacters
	if valueType == graphValueTypeTitle {
		bound = maxGraphTitleRunes
	}
	// Words past what a cell holds keep their first characters, plain.
	if within := graphTextWithin(plain, bound); within != plain || len(spans) > workspaceImportMaxBlockSpans {
		plain, spans = within, nil
	}
	encoded, _ := json.Marshal(plain)
	return graphRowValue{Type: valueType, Value: encoded, Spans: spans}, true
}

// databaseFiles are the pictures a files cell holds, in order: an uploaded
// one by its file, named by its words or else "image", any other as the
// address it is written at, named by its words or else by that address.
func (reader *workspaceImportMarkdownReader) databaseFiles(cell ast.Node) []graphFileValueItem {
	items := []graphFileValueItem{}
	if cell == nil {
		return items
	}
	_ = ast.Walk(cell, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		image, ok := node.(*ast.Image)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		address := strings.TrimSpace(reader.text(image.Destination, false))
		name := strings.TrimSpace(reader.plain(image))
		if fileID, uploaded := strings.CutPrefix(address, workspaceImportFileReference); uploaded {
			if workspaceImportReferenceIDPattern.MatchString(fileID) {
				items = append(items, graphFileValueItem{
					FileID: fileID, Name: graphTextWithin(firstNonempty(name, "image"), maxGraphExternalFileNameRunes),
					ContentType: "application/octet-stream", SHA256: strings.Repeat("0", 64),
				})
			}
			return ast.WalkSkipChildren, nil
		}
		if validGraphStoredFileAddress(address) {
			items = append(items, graphFileValueItem{
				Source: graphExternalFileSource, URL: address,
				Name: graphTextWithin(firstNonempty(name, address), maxGraphExternalFileNameRunes),
			})
		}
		return ast.WalkSkipChildren, nil
	})
	return items
}

// workspaceImportMarkdownDatabaseOperation reports whether a staged Markdown
// item's operation is one an inline database brings beside its block.
func workspaceImportMarkdownDatabaseOperation(operation *graphOperation) bool {
	if operation == nil {
		return false
	}
	switch operation.Kind {
	case graphOperationKindUpsertDataSource:
		return operation.DataSource != nil
	case graphOperationKindUpsertProperty:
		return operation.Property != nil
	case graphOperationKindUpsertView:
		return operation.View != nil
	case graphOperationKindUpsertRow:
		return operation.Row != nil
	}
	return false
}
