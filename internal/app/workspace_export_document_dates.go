package app

import (
	"strings"
	"time"
)

// A readable export writes its dates on the clock of the person exporting,
// in their account's time zone, as Notion's export does (seen 2026-11-12,
// a Seoul exporter's Markdown & CSV and HTML exports of the same database):
//
//   - a date property's CSV and Markdown give each timed end on that clock
//     and name it: "2026년 1월 2일 오전 10:30 (GMT+9)", a time set in UTC
//     "2026년 5월 6일 오후 5:00 (GMT+9)", a range "… (GMT+9) → … (GMT+9)",
//     a day alone with no name;
//   - its HTML gives each end on the clock the date keeps, naming that clock
//     only when its offset is not the exporter's: "2026년 1월 2일 오전 10:30",
//     "2026년 5월 6일 오전 8:00 (UTC)", "2026년 6월 7일 오전 9:15 (GMT-4)",
//     in <time datetime="2026-05-06T08:00">;
//   - a row's created and edited times are on the exporter's clock without a
//     name, "2026년 11월 12일 오후 6:15", in HTML in <time datetime>;
//   - formulas work on the exporter's clock, as the app's cells do for their
//     viewer (workspaceExportDocument.queryContext).
//
// A time written without a clock of its own is on the exporter's, as the
// app shows it on its viewer's.

// exportLocation is the exporter's time zone, the server's when the document
// has none (personZoneLocation).
func (document *workspaceExportDocument) exportLocation() *time.Location {
	if document.Location == nil {
		return personZoneLocation("")
	}
	return document.Location
}

// workspaceExportTimeZone is the time zone an export writes its dates on:
// its person's clock (person_clock.go) — their account's zone, else the
// server's, named "Local" (no zone's name: its offset names it).
func workspaceExportTimeZone(zone string) (string, *time.Location) {
	if location, ok := databaseFormulaLocation(zone); ok {
		return zone, location
	}
	location := personZoneLocation("")
	return location.String(), location
}

// formulaEnvironment is a formula's environment for writing values the way
// the export reads its dates: on the exporter's clock, in its language.
func (document *workspaceExportDocument) formulaEnvironment() *databaseFormulaEnvironment {
	return &databaseFormulaEnvironment{location: document.exportLocation(), language: document.Language}
}

// dateCellPart writes a date property's value in its own date and time
// formats and the exporter's language: Text for CSV and Markdown on the
// exporter's clock, HTMLText on the date's own, and its start as DateTime.
func (document *workspaceExportDocument) dateCellPart(property *workspaceExportDocProperty, value any) workspaceExportCellPart {
	date, _ := value.(map[string]any)
	zone := mapString(date, "time_zone")
	if zone == "" {
		zone = mapString(date, "timeZone")
	}
	start, end := mapString(date, "start"), mapString(date, "end")
	ends, ok := document.dateEnds(start, end, zone)
	if !ok {
		return workspaceExportCellPart{Text: workspaceExportDateText(value)}
	}
	format, timeFormat := mapString(property.Config, "dateFormat"), mapString(property.Config, "timeFormat")
	exporter, own := make([]string, len(ends)), make([]string, len(ends))
	for index, at := range ends {
		exporter[index] = document.dateEndWords(at, format, timeFormat, true)
		own[index] = document.dateEndWords(at, format, timeFormat, false)
	}
	return workspaceExportCellPart{
		Text: strings.Join(exporter, " → "), HTMLText: strings.Join(own, " → "), DateTime: workspaceExportClock(ends[0]),
	}
}

// dateEnds reads a date's ends on the clocks they keep: a time zone's, a
// written offset's, UTC's for a time written in UTC (as a formula reads a
// date property, databaseFormulaPropertyDate), or the exporter's.
func (document *workspaceExportDocument) dateEnds(start, end, zone string) ([]databaseFormulaDateEnd, bool) {
	if _, _, ok := parseNotionMentionDateRange(start, end, zone); !ok {
		return nil, false
	}
	read, ok := databaseFormulaPropertyDate(map[string]any{"start": start, "end": end, "timeZone": zone}).(databaseFormulaDate)
	if !ok {
		return nil, false
	}
	environment := document.formulaEnvironment()
	ends := []databaseFormulaDateEnd{}
	for _, text := range strings.Split(read.Text, "/") {
		at, ok := environment.dateEnd(text, read.Zone)
		if !ok {
			return nil, false
		}
		ends = append(ends, at)
	}
	return ends, true
}

// dateEndWords writes one end: on the exporter's clock naming it, or on its
// own naming that only when its offset is not the exporter's.
func (document *workspaceExportDocument) dateEndWords(end databaseFormulaDateEnd, format, timeFormat string, exporterClock bool) string {
	if format == "" || format == dateMentionFormatRelative {
		format = "full"
	}
	if !end.timed || timeFormat == "hidden" {
		return dateMentionLabelWords(end.at.Format(time.DateOnly), format, timeFormat, document.Language, time.UTC, time.Time{})
	}
	locale := databaseFormulaLocaleFor(document.Language)
	at, zone := end.at, end.zone
	if exporterClock {
		at, zone = at.In(document.exportLocation()), document.TimeZone
	}
	words := dateMentionLabelWords(at.Format("2006-01-02T15:04"), format, timeFormat, document.Language, time.UTC, time.Time{})
	_, offset := at.Zone()
	if _, exporter := at.In(document.exportLocation()).Zone(); exporterClock || offset != exporter {
		words += " (" + databaseFormulaZoneLabel(at, zone, locale) + ")"
	}
	return words
}

// workspaceExportClock is an end as HTML's <time datetime> gives it, on its
// own clock: "2026-05-06T08:00", or a day.
func workspaceExportClock(end databaseFormulaDateEnd) string {
	if !end.timed {
		return end.at.Format(time.DateOnly)
	}
	return end.at.Format("2006-01-02T15:04")
}

// timeCellPart writes a row's created or edited time in the property's
// formats on the exporter's clock.
func (document *workspaceExportDocument) timeCellPart(property *workspaceExportDocProperty, milliseconds int64) workspaceExportCellPart {
	if milliseconds < 1 {
		return workspaceExportCellPart{}
	}
	at := time.UnixMilli(milliseconds).In(document.exportLocation())
	format, timeFormat := mapString(property.Config, "dateFormat"), mapString(property.Config, "timeFormat")
	if format == "" || format == dateMentionFormatRelative {
		format = "full"
	}
	clock := at.Format("2006-01-02T15:04")
	return workspaceExportCellPart{
		Text: dateMentionLabelWords(clock, format, timeFormat, document.Language, time.UTC, time.Time{}), DateTime: clock,
	}
}
