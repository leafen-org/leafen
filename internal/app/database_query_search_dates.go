package app

import (
	"encoding/json"
	"strings"
	"time"
)

// A view's search finds a date by the words its cell shows its reader, as
// Notion's (seen 2026-11-15 at 1440 and on a phone: "5" finds a row dated
// 2026년 11월 13일 → 2026년 11월 15일, "13일" finds it too): each end in the
// property's date and time formats, in the reader's language and on their
// clock, the ends joined by " → ", as a readable export writes a date cell
// (workspace_export_document_dates.go). Before, a date read as its start's
// ISO day alone ("2026-11-13"), so neither "5" nor "13일" found that row.

// matchesDatabaseQuerySearch reports whether a row holds the search in the
// text its cells show whoever queries.
func matchesDatabaseQuerySearch(row databaseQueryRow, search string, context databaseQueryContext, principalID string) bool {
	search = foldDatabaseViewSearchText(strings.TrimSpace(search))
	if search == "" {
		return true
	}
	language := context.People.readerLanguage(principalID)
	for _, property := range context.Properties {
		// A value the person may not see doesn't match their search.
		if context.hidesQueryValue(row, property.ID) {
			continue
		}
		value := databaseQueryPropertyValue(row, property)
		text, ok := databaseQueryDateWords(value, property, context.FormulaTime, language)
		if !ok {
			text = databaseQueryPropertyDisplayText(value, property, context)
		}
		if strings.Contains(foldDatabaseViewSearchText(text), search) {
			return true
		}
	}
	return false
}

// readerLanguage is the language the screens of whoever queries show dates
// in, as the server writes them its texts (serverTextLanguageIn): an account
// following its system's reads what its own request names, else the rule's
// later steps (server_text_language.go).
func (people *databaseFormulaPeople) readerLanguage(principalID string) string {
	if people == nil || people.app == nil || principalID == "" {
		return ""
	}
	if language, ok := people.languages[principalID]; ok {
		return language
	}
	language, err := serverTextLanguageIn(people.request, people.app, principalID)
	if err != nil {
		language = ""
	}
	if people.languages == nil {
		people.languages = map[string]string{}
	}
	people.languages[principalID] = language
	return language
}

// databaseQueryDateWords writes a date as its cell shows it; ok is false for
// a value that is no date.
func databaseQueryDateWords(value any, property storedDatabaseQueryProperty, clock databaseFormulaTime, language string) (string, bool) {
	date, _ := value.(map[string]any)
	start := mapString(date, "start")
	if start == "" {
		return "", false
	}
	zone := mapString(date, "time_zone")
	if zone == "" {
		zone = mapString(date, "timeZone")
	}
	location := clock.Location
	if location == nil {
		location = time.UTC
	}
	config := map[string]any{}
	_ = json.Unmarshal([]byte(property.Config), &config)
	format, timeFormat := mapString(config, "dateFormat"), mapString(config, "timeFormat")
	// A property's cell shows the whole date unless it is set to read as relative.
	if format == "" || (format == dateMentionFormatRelative && clock.Now.IsZero()) {
		format = "full"
	}
	environment := &databaseFormulaEnvironment{location: location, language: language}
	words := []string{}
	for _, text := range []string{start, mapString(date, "end")} {
		if text == "" {
			continue
		}
		end, ok := environment.dateEnd(text, zone)
		if !ok {
			return "", false
		}
		shown := end.at.Format(time.DateOnly)
		if end.timed && timeFormat != "hidden" {
			shown = end.at.In(location).Format("2006-01-02T15:04")
		}
		words = append(words, dateMentionLabelWords(shown, format, timeFormat, language, location, clock.Now))
	}
	return strings.Join(words, " → "), true
}
