package app

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// A rollup's value is written as Notion's export writes it (seen 2026-11-12,
// Markdown & CSV and HTML of the same database, a Seoul exporter in Korean),
// the same text in the CSV, a row page's Markdown and the HTML table:
//
//   - the related values shown ("원래 값 표시", "고유한 값 표시"): each related
//     row's items between commas with no space, "2.25,-1.5,10,0", one with no
//     value left out, the same value twice once for the unique ones;
//     a date as a formula's date reads, on the clock it keeps naming it only
//     when its offset is not the exporter's, "2026년 6월 7일 오전 9:15
//     (GMT-4)", a range "… → …", with no "@"; a checkbox "Yes" or "No"; a
//     person, a bot or an option by its name; a formula's value as a
//     formula's list writes it, "@" before a date, "true" and "false"; a
//     created or edited time on the exporter's clock with no name; a
//     relation's pages in HTML by title, linked, between ", ", and in CSV and
//     Markdown by their addresses, each row's between ", " and the rows'
//     between ",", "https://app.notion.com/〈나〉, https://app.notion.com/〈다〉,
//     https://app.notion.com/〈다〉" — here each page's address on this
//     server, an approved difference;
//   - the earliest and the latest date as the day alone, "2026년 2월 3일";
//   - a date range as the time between its ends, as the app's cell shows it
//     (rollupDateDisplay.ts), "4.1개월", "0분";
//   - a share as its fraction, "0.4", "0.3333333333333333", and counts and
//     sums as numbers, "13", "2.6".
//
// Notion's export writes the earliest and the latest day of a day without a
// time one day early ("2026년 3월 3일" for 3월 4일, which its own screen
// shows); the export writes the day itself, as the app's cell and Notion's
// screen do.

// rollupCell writes a rollup's computed value.
func (document *workspaceExportDocument) rollupCell(property *workspaceExportDocProperty, computed databaseComputedValue) workspaceExportCell {
	stored, ok := workspaceExportQueryProperty(property)
	if !ok {
		return workspaceExportCell{}
	}
	config, err := decodeDatabaseComputedConfig(stored.Config)
	if err != nil {
		return workspaceExportCell{}
	}
	_, targetID, _ := config.rollupFields()
	function := config.rollupCalculation()
	value := databaseQueryRawValue(computed.Value)
	text := ""
	switch {
	case databaseRollupKeepsLeaves(function) && len(computed.Leaves) > 0:
		return document.rollupShownCell(document.property(targetID), computed.Leaves, function == "show_unique")
	case function == "earliest_date" || function == "latest_date":
		text = document.rollupDayText(value)
	case function == "date_range":
		text = document.rollupRangeText(value)
	default:
		number, isNumber := value.(float64)
		if !isNumber {
			text = workspaceExportComputedValueText(value)
			break
		}
		if !math.IsNaN(number) && !math.IsInf(number, 0) {
			text = strconv.FormatFloat(number, 'f', -1, 64)
		}
	}
	return workspaceExportCell{Parts: []workspaceExportCellPart{{Text: closedMentionsInLine(text, document.Language)}}}
}

// rollupShownCell writes the related values: one part of their items between
// commas, or a relation's pages each as a linked part, with their addresses
// for CSV and Markdown.
func (document *workspaceExportDocument) rollupShownCell(target *workspaceExportDocProperty, encoded json.RawMessage, unique bool) workspaceExportCell {
	leaves := []json.RawMessage{}
	if target == nil || json.Unmarshal(encoded, &leaves) != nil {
		return workspaceExportCell{}
	}
	if target.Type == graphValueTypeRelation {
		cell, rows := workspaceExportCell{}, []string{}
		for _, leaf := range leaves {
			addresses := []string{}
			for _, id := range workspaceExportStrings(databaseQueryRawValue(leaf)) {
				if part, ok := document.relatedPart(id); ok {
					cell.Parts = append(cell.Parts, part)
					if address := document.pageAddress(id); address != "" {
						addresses = append(addresses, address)
					}
				}
			}
			if len(addresses) > 0 {
				rows = append(rows, strings.Join(addresses, ", "))
			}
		}
		cell.Plain = strings.Join(rows, ",")
		return cell
	}
	items, seen := []string{}, map[string]bool{}
	for _, leaf := range leaves {
		texts := document.rollupItemTexts(target, databaseQueryRawValue(leaf))
		key := strings.Join(texts, "\x00")
		if len(texts) == 0 || unique && seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, texts...)
	}
	return workspaceExportCell{Parts: []workspaceExportCellPart{{Text: closedMentionsInLine(strings.Join(items, ","), document.Language)}}}
}

// rollupItemTexts are one related value's items, none for an empty value.
func (document *workspaceExportDocument) rollupItemTexts(target *workspaceExportDocProperty, value any) []string {
	texts := []string{}
	add := func(text string) {
		if strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
	}
	switch target.Type {
	case graphValueTypeFormula:
		if input := databaseFormulaInput(value); input != nil && input != "" {
			add(document.formulaValueText(input, false))
		}
	case graphValueTypeDate:
		date, _ := value.(map[string]any)
		zone := mapString(date, "timeZone")
		if zone == "" {
			zone = mapString(date, "time_zone")
		}
		ends, ok := document.dateEnds(mapString(date, "start"), mapString(date, "end"), zone)
		if !ok {
			add(workspaceExportDateText(value))
			break
		}
		words := make([]string, len(ends))
		for index, end := range ends {
			words[index] = document.dateEndWords(end, "", "", false)
		}
		add(strings.Join(words, " → "))
	case graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
		text, _ := value.(string)
		if at, err := time.Parse(time.RFC3339Nano, text); err == nil {
			clock := at.In(document.exportLocation()).Format("2006-01-02T15:04")
			add(dateMentionLabelWords(clock, "full", "", document.Language, time.UTC, time.Time{}))
		}
	case graphValueTypeCheckbox:
		if checked, ok := value.(bool); ok {
			add(workspaceExportCheckboxPart(checked).Text)
		}
	case graphValueTypeNumber, graphValueTypeUniqueID:
		if number, ok := value.(float64); ok && !math.IsNaN(number) && !math.IsInf(number, 0) {
			text := strconv.FormatFloat(number, 'f', -1, 64)
			if prefix := strings.TrimSpace(mapString(target.Config, "idPrefix")); prefix != "" && target.Type == graphValueTypeUniqueID {
				text = prefix + "-" + text
			}
			add(text)
		}
	case graphValueTypePerson, graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		for _, id := range workspaceExportStrings(value) {
			add(document.personName(id))
		}
	case graphValueTypeSelect, graphValueTypeStatus:
		if id, ok := value.(string); ok {
			name, _ := document.optionName(target, id)
			add(name)
		}
	case graphValueTypeMultiSelect:
		for _, id := range workspaceExportStrings(value) {
			name, _ := document.optionName(target, id)
			add(name)
		}
	case graphValueTypeFiles:
		items, _ := value.([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			add(mapString(item, "name"))
		}
	default:
		add(workspaceExportComputedValueText(value))
	}
	return texts
}

// rollupDayText is the earliest or the latest date as the day it falls on,
// as the app's cell shows it (rollupShownText).
func (document *workspaceExportDocument) rollupDayText(value any) string {
	date, _ := value.(map[string]any)
	start := strings.TrimSpace(mapString(date, "start"))
	if len(start) < len(time.DateOnly) {
		return start
	}
	if _, err := time.Parse(time.DateOnly, start[:len(time.DateOnly)]); err != nil {
		return start
	}
	return dateMentionLabelWords(start[:len(time.DateOnly)], "full", "", document.Language, time.UTC, time.Time{})
}

// rollupRangeText is a date range as the time between its ends, as the app's
// cell shows it (rollupDurationText): minutes under an hour, hours under a
// day, days under 30, then months of 30 days under a year and years of 365,
// those two to one decimal.
func (document *workspaceExportDocument) rollupRangeText(value any) string {
	span, _ := value.(map[string]any)
	start, end := mapString(span, "start"), mapString(span, "end")
	if end == "" {
		end = start
	}
	from, fromOK := databaseRollupTime(start)
	to, toOK := databaseRollupTime(end)
	if !fromOK || !toOK {
		return workspaceExportDateText(value)
	}
	minutes := math.Abs(to.Sub(from).Minutes())
	hours, days := minutes/60, minutes/60/24
	switch {
	case minutes < 60:
		return document.rollupDurationWords(math.Round(minutes), "minute")
	case hours < 24:
		return document.rollupDurationWords(math.Round(hours), "hour")
	case days < 30:
		return document.rollupDurationWords(math.Round(days), "day")
	case days < 365:
		return document.rollupDurationWords(math.Round(days/30*10)/10, "month")
	}
	return document.rollupDurationWords(math.Round(days/365*10)/10, "year")
}

// rollupDurationWords is a count of a unit in the export's language, as the
// app's propertyCell.rollupDuration words.
func (document *workspaceExportDocument) rollupDurationWords(count float64, unit string) string {
	number := strconv.FormatFloat(count, 'f', -1, 64)
	if document.Language == accountLanguageKo {
		korean := map[string]string{"minute": "분", "hour": "시간", "day": "일", "month": "개월", "year": "년"}
		return number + korean[unit]
	}
	if count != 1 {
		unit += "s"
	}
	return number + " " + unit
}
