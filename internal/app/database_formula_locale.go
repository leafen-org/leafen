package app

import (
	"strconv"
	"strings"
)

// A formula writes dates as text in the language of whoever reads them, as
// Notion's do: its screen writes "2026년 1월 2일 오전 10:30" in Korean, and
// what its server writes for that reader — a filter of a view, an export —
// matches and writes the same words (seen 2026-11-12: a view's saved filter
// "포함: 오전" kept the morning rows on screen and in the export, "AM" kept
// none; the export's formula cells read "2026년 1월 2일 오전 10:30"). The
// Public API reads English ("January 2, 2026 10:30 AM (GMT+9)", its "AM"
// filter keeping the morning rows). The server writes Korean and English, as
// its other dates (dateMentionLabelWords), the words and layouts the web's
// formulas write them in (formulaLocales.ts); any other language English.

type databaseFormulaLocale struct {
	months, monthsShort, weekdays, weekdaysShort, weekdaysMin []string
	meridiem                                                  func(hour int, lower bool) string
	ordinal                                                   func(day int) string
	layouts                                                   map[string]string
	// zones names time zones, by offset in minutes, where the short name is
	// not the plain offset.
	zones map[string]map[int]string
	// untitledPage is what an untitled page's name reads in format().
	untitledPage string
}

var databaseFormulaEnglishWeekdays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

var databaseFormulaEnglishLocale = func() *databaseFormulaLocale {
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	return &databaseFormulaLocale{
		months: months, monthsShort: databaseFormulaPrefixes(months, 3), weekdays: databaseFormulaEnglishWeekdays,
		weekdaysShort: databaseFormulaPrefixes(databaseFormulaEnglishWeekdays, 3), weekdaysMin: databaseFormulaPrefixes(databaseFormulaEnglishWeekdays, 2),
		meridiem: func(hour int, lower bool) string {
			period := "AM"
			if hour >= 12 {
				period = "PM"
			}
			if lower {
				return strings.ToLower(period)
			}
			return period
		},
		ordinal: databaseFormulaOrdinal,
		layouts: map[string]string{
			"LT": "h:mm A", "LTS": "h:mm:ss A", "L": "MM/DD/YYYY", "LL": "MMMM D, YYYY", "LLL": "MMMM D, YYYY h:mm A",
			"LLLL": "dddd, MMMM D, YYYY h:mm A", "l": "M/D/YYYY", "ll": "MMM D, YYYY", "lll": "MMM D, YYYY h:mm A",
			"llll": "ddd, MMM D, YYYY h:mm A",
		},
		zones: englishTimeZoneShortNames, untitledPage: notionUntitledPageMentionTitle,
	}
}()

var databaseFormulaKoreanLocale = func() *databaseFormulaLocale {
	months := make([]string, 12)
	for index := range months {
		months[index] = strconv.Itoa(index+1) + "월"
	}
	short := []string{"일", "월", "화", "수", "목", "금", "토"}
	weekdays := make([]string, len(short))
	for index, day := range short {
		weekdays[index] = day + "요일"
	}
	return &databaseFormulaLocale{
		months: months, monthsShort: months, weekdays: weekdays, weekdaysShort: short, weekdaysMin: short,
		meridiem: func(hour int, _ bool) string {
			if hour < 12 {
				return "오전"
			}
			return "오후"
		},
		ordinal: func(day int) string { return strconv.Itoa(day) + "일" },
		layouts: map[string]string{
			"LT": "A h:mm", "LTS": "A h:mm:ss", "L": "YYYY.MM.DD.", "LL": "YYYY년 MMMM D일", "LLL": "YYYY년 MMMM D일 A h:mm",
			"LLLL": "YYYY년 MMMM D일 dddd A h:mm", "l": "YYYY.MM.DD.", "ll": "YYYY년 MMMM D일", "lll": "YYYY년 MMMM D일 A h:mm",
			"llll": "YYYY년 MMMM D일 dddd A h:mm",
		},
		zones: koreanTimeZoneShortNames, untitledPage: "새 페이지",
	}
}()

func databaseFormulaPrefixes(words []string, length int) []string {
	prefixes := make([]string, len(words))
	for index, word := range words {
		prefixes[index] = word[:length]
	}
	return prefixes
}

// databaseFormulaLocaleFor is the language formulas write dates in for a
// reader of this language: Korean, else English.
func databaseFormulaLocaleFor(language string) *databaseFormulaLocale {
	if strings.HasPrefix(strings.ToLower(language), accountLanguageKo) {
		return databaseFormulaKoreanLocale
	}
	return databaseFormulaEnglishLocale
}

func (environment *databaseFormulaEnvironment) locale() *databaseFormulaLocale {
	return databaseFormulaLocaleFor(environment.language)
}
