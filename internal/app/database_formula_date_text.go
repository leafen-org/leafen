package app

import (
	"strconv"
	"strings"
	"time"
)

// A formula writes a date as text as Notion does, in its reader's language
// (database_formula_locale.go): a day in its LL layout, "August 30, 2023",
// and a time in its LLL layout, "August 30, 2023 2:05 PM", on the clock the
// time keeps, that clock's name following in brackets, "(EDT)" or
// "(GMT+9)", when the viewer's zone is at another offset. A named time zone
// goes by the short name Intl gives it in the language, else by its offset.
// A range joins its ends with " → ", the end's time alone (LT) when it falls
// on the same day. Joined to other text, a date is a mention showing "@" and
// this text (database_formula_rich_text.go).

type databaseFormulaDateEnd struct {
	at    time.Time
	timed bool
	// zone names the time zone the time keeps.
	zone string
}

// dateEnd reads one end of a date as the viewer sees it: a day, or a time on
// the clock it keeps.
func (environment *databaseFormulaEnvironment) dateEnd(text, zone string) (databaseFormulaDateEnd, bool) {
	moment, ok := environment.parseZonedDate(text, zone)
	if !ok {
		return databaseFormulaDateEnd{}, false
	}
	if !moment.hasTime {
		return databaseFormulaDateEnd{at: moment.at}, true
	}
	return databaseFormulaDateEnd{at: moment.clock(), timed: true, zone: moment.zone}, true
}

// databaseFormulaZoneLabel names the clock a time keeps: a named time zone
// by its short name in the language at that offset, else the offset.
func databaseFormulaZoneLabel(at time.Time, zone string, locale *databaseFormulaLocale) string {
	_, offset := at.Zone()
	if name, named := locale.zones[zone][offset/60]; named {
		return name
	}
	return databaseFormulaOffsetLabel(offset)
}

func databaseFormulaOffsetLabel(offset int) string {
	if offset == 0 {
		return "UTC"
	}
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	label := "GMT" + sign + strconv.Itoa(offset/3600)
	if minutes := offset % 3600 / 60; minutes != 0 {
		label += ":" + databaseFormulaPad(minutes, 2)
	}
	return label
}

func (environment *databaseFormulaEnvironment) dateEndText(end databaseFormulaDateEnd) string {
	locale := environment.locale()
	if !end.timed {
		return databaseFormulaFormatDate(databaseFormulaMoment{at: end.at}, locale.layouts["LL"], locale)
	}
	text := databaseFormulaFormatDate(databaseFormulaMoment{at: end.at}, locale.layouts["LLL"], locale)
	_, own := end.at.Zone()
	if _, viewer := end.at.In(environment.location).Zone(); own != viewer {
		text += " (" + databaseFormulaZoneLabel(end.at, end.zone, locale) + ")"
	}
	return text
}

// dateText writes a date as Notion writes it as text.
func (environment *databaseFormulaEnvironment) dateText(value databaseFormulaDate) string {
	startText, endText, ranged := strings.Cut(value.Text, "/")
	// A date it can't read — Notion's invalid date, a day past 9999 — is no
	// text, a range's end with its arrow alone, its spaces kept (seen
	// 2026-11-15 through Notion's API: format(dateRange(now(),
	// fromTimestamp(3e14))) + "|" is "November 15, 2026 5:18 AM → |", the
	// other way "| → November 15, 2026 5:18 AM"), as the editor's
	// formulaDateText.
	start, startOK := environment.dateEnd(startText, value.Zone)
	text := ""
	if startOK {
		text = environment.dateEndText(start)
	}
	if !ranged {
		return text
	}
	end, ok := environment.dateEnd(endText, value.Zone)
	if !startOK || !ok {
		other := ""
		if ok {
			other = environment.dateEndText(end)
		}
		return text + " → " + other
	}
	if start.timed && end.timed && start.at.Format(time.DateOnly) == end.at.Format(time.DateOnly) {
		locale := environment.locale()
		return text + " → " + databaseFormulaFormatDate(databaseFormulaMoment{at: end.at}, locale.layouts["LT"], locale)
	}
	return text + " → " + environment.dateEndText(end)
}

// formatText is a value as format() writes it: a date as its text, rich
// text as its plain text, a list's items, flattened, between commas.
func (environment *databaseFormulaEnvironment) formatText(value any) string {
	switch typed := value.(type) {
	case databaseFormulaDate:
		return environment.dateText(typed)
	case databaseFormulaRichText:
		return typed.plain()
	case []any:
		parts := make([]string, len(typed))
		for index, item := range typed {
			parts[index] = environment.formatText(item)
		}
		return strings.Join(parts, ",")
	case databaseFormulaReference:
		// An untitled page is "New page", as Notion's format() writes it
		// through its API, "새 페이지" on its Korean screen (seen 2026-11-12).
		if typed.Kind == "page" && typed.Name == "" {
			return environment.locale().untitledPage
		}
	}
	return databaseFormulaText(value)
}
