package app

import (
	"encoding/csv"
	"errors"
	"io"
	"strconv"
	"strings"
)

// workspaceImportCSVColumnTypes are the types a new database's CSV column
// takes, as Notion's "CSV 열 설정" lists them (seen 2026-10-01); "" leaves
// the column out.
var workspaceImportCSVColumnTypes = map[string]bool{
	"": true, graphValueTypeTitle: true, graphValueTypeRichText: true, graphValueTypeNumber: true,
	graphValueTypeSelect: true, graphValueTypeMultiSelect: true, graphValueTypeDate: true,
	graphValueTypeCheckbox: true, graphValueTypeURL: true, graphValueTypeEmail: true, graphValueTypePhone: true,
}

// workspaceImportCSVDateFormats are the formats a date column may name after
// its type ("date:month_day_year"), as the "날짜" submenu of Notion's "CSV 열
// 설정" lists them (seen 2026-10-01); the format is the new property's.
var workspaceImportCSVDateFormats = map[string]bool{
	"full": true, "short": true, "month_day_year": true, "day_month_year": true, "year_month_day": true,
}

// workspaceImportCSVColumnChoice splits a chosen column type from the date
// format it names, if any.
func workspaceImportCSVColumnChoice(choice string) (string, string) {
	columnType, format, _ := strings.Cut(choice, ":")
	return columnType, format
}

// validWorkspaceImportCSVColumnTypes accepts no choice, or a type for each
// column with exactly one title.
func validWorkspaceImportCSVColumnTypes(types []string) bool {
	if len(types) == 0 {
		return true
	}
	titles := 0
	for _, choice := range types {
		columnType, format := workspaceImportCSVColumnChoice(choice)
		if !workspaceImportCSVColumnTypes[columnType] || strings.Contains(choice, ":") &&
			(columnType != graphValueTypeDate || !workspaceImportCSVDateFormats[format]) {
			return false
		}
		if columnType == graphValueTypeTitle {
			titles++
		}
	}
	return len(types) <= maxWorkspaceCSVMergeColumns && titles == 1
}

func trimWorkspaceImportCSVColumnTypes(types []string) {
	for index, columnType := range types {
		types[index] = strings.TrimSpace(columnType)
	}
}

// workspaceImportCSVHashParts are what a request's column choices add to
// the request it is known by.
func workspaceImportCSVHashParts(input workspaceImportInput) []string {
	parts := []string{}
	if len(input.CSVColumnTypes) > 0 {
		parts = append(append(parts, "columns"), input.CSVColumnTypes...)
	}
	if input.CSVKeepFirstRow {
		parts = append(parts, "keep-first-row")
	}
	return parts
}

// parseWorkspaceImportCSV makes a database of a CSV: a property for each
// column named by its first row, typed as chosen in the CSV window or else, as
// a CSV in a ZIP is, by its values (inferWorkspaceImportCSVType), and a row
// for each row after the first, or for the first too when it is kept.
const workspaceImportCSVColumnWidth = 250

const workspaceImportCSVDelimiterRecords = 10

// newWorkspaceImportCSVReader reads a CSV by the delimiter it is written with,
// found as the import window finds it (web/src/lib/csvImportModel.ts) and as
// Notion's does (commas, tabs, pipes and semicolons, seen 2026-11-05): of those
// whose first records split into two fields or more, the one whose records
// differ least and, among those, have the most fields. A comma when none does.
func newWorkspaceImportCSVReader(value string) *csv.Reader {
	best, bestDifference, bestFields := ',', -1, 0.0
	for _, candidate := range []rune{',', '\t', '|', ';'} {
		reader := csv.NewReader(strings.NewReader(value))
		reader.FieldsPerRecord = -1
		reader.Comma = candidate
		total, difference, count, previous := 0, 0, 0, 0
		for count < workspaceImportCSVDelimiterRecords {
			record, err := reader.Read()
			if err != nil {
				break
			}
			if !workspaceImportCSVRowHasValue(record) {
				continue
			}
			if count > 0 {
				difference += max(len(record)-previous, previous-len(record))
			}
			total, count, previous = total+len(record), count+1, len(record)
		}
		if count == 0 {
			continue
		}
		fields := float64(total) / float64(count)
		if fields < 2 || (bestDifference >= 0 && difference > bestDifference) || fields <= bestFields {
			continue
		}
		best, bestDifference, bestFields = candidate, difference, fields
	}
	reader := csv.NewReader(strings.NewReader(value))
	reader.FieldsPerRecord = -1
	reader.Comma = best
	return reader
}

func parseWorkspaceImportCSV(
	value, title, locale string, chosen []string, keepFirstRow bool, reserved []workspaceImportReservedRow, dataSourceID string,
) (workspaceImportItemSource, error) {
	reader := newWorkspaceImportCSVReader(value)
	records := [][]string{}
	width := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, workspaceImportInputError()
		}
		if !workspaceImportCSVRowHasValue(record) {
			continue
		}
		records = append(records, record)
		width = max(width, len(record))
	}
	if len(records) == 0 || width == 0 {
		return nil, workspaceImportInputError()
	}
	headers := workspaceImportCSVHeaders(records[0], width, locale)
	rows := records[1:]
	if keepFirstRow {
		rows = records
	}
	if len(chosen) > 0 {
		if len(chosen) != width {
			return nil, workspaceImportInputError()
		}
		types, formats := make([]string, width), make([]string, width)
		for column, choice := range chosen {
			types[column], formats[column] = workspaceImportCSVColumnChoice(choice)
		}
		return buildWorkspaceImportCSVItems(title, locale, headers, types, formats, true, rows, reserved, dataSourceID)
	}
	types, formats := make([]string, width), make([]string, width)
	titleColumn := -1
	for column := range width {
		values := make([]string, 0, len(rows))
		for _, row := range rows {
			if column < len(row) && strings.TrimSpace(row[column]) != "" {
				values = append(values, strings.TrimSpace(row[column]))
			}
		}
		types[column], formats[column] = inferWorkspaceImportCSVType(values)
		if titleColumn < 0 && types[column] == graphValueTypeRichText {
			titleColumn = column
		}
	}
	// The title is the first column read as text, as Notion's ZIP import and
	// CSV window choose it (seen 2026-11-12: a number, date, checkbox, email
	// or URL column before it keeps its type). Notion's window asks for one
	// when none reads as text; here the first column takes it.
	titleColumn = max(titleColumn, 0)
	types[titleColumn], formats[titleColumn] = graphValueTypeTitle, ""
	return buildWorkspaceImportCSVItems(title, locale, headers, types, formats, false, rows, reserved, dataSourceID)
}

func workspaceImportCSVRowHasValue(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func workspaceImportCSVHeaders(raw []string, width int, locale string) []string {
	name, column := "Name", func(index int) string { return "Column " + strconv.Itoa(index) }
	if locale == workspaceImportLocaleKorean {
		name, column = "이름", func(index int) string { return "열 " + strconv.Itoa(index) }
	}
	used := map[string]int{}
	headers := make([]string, width)
	for index := range width {
		fallback := column(index + 1)
		if index == 0 {
			fallback = name
		}
		base := fallback
		if index < len(raw) && strings.TrimSpace(raw[index]) != "" {
			base = strings.TrimSpace(raw[index])
		}
		used[base]++
		headers[index] = base
		if used[base] > 1 {
			headers[index] += " " + strconv.Itoa(used[base])
		}
	}
	return headers
}
