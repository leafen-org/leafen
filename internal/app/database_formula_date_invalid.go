package app

import (
	"math"
	"strconv"
	"strings"
)

// databaseFormulaInvalidDate is Notion's invalid date, which a formula keeps
// as a date: a time whose UTC year isn't 0 to 9999, or a day past what
// JavaScript's dates count, 100,000,000 days either side of 1970 (seen
// 2026-11-15 through its API: fromTimestamp(3e14) and dateAdd(now(), 3e6,
// "days") are "Invalid DateTime", dateAdd(parseDate("9999-12-31"), 400,
// "days") 10001-02-03, fromTimestamp(-62135596800001) 0000-12-31T23:59). Its
// cell shows nothing and empty() of it is false; a function that reads a date
// gives a number that is no number of it (null through the API) — of a day
// past 9999 too, whose text has more than four digits — but dateAdd(),
// dateStart(), dateEnd() and dateRange() keep it, and as text it is "" ("@"
// joined to text). The editor's is the same
// (formulaDates.ts INVALID_FORMULA_DATE).
const databaseFormulaInvalidDate = "Invalid DateTime"

// unreadableDate reports whether a value given where a date is read is a date
// that can't be read: Notion's invalid date or a day past 9999.
func (environment *databaseFormulaEnvironment) unreadableDate(value any) bool {
	date, isDate := value.(databaseFormulaDate)
	if !isDate {
		return false
	}
	_, ok := environment.momentOf(date)
	return !ok
}

// databaseFormulaDayText writes a day, its year in four digits or more and a
// minus before a year before 0, as JavaScript writes one ("-0001-11-27").
func databaseFormulaDayText(year, month, day int) string {
	sign := ""
	if year < 0 {
		sign, year = "-", -year
	}
	pad := func(value, width int) string {
		text := strconv.Itoa(value)
		return strings.Repeat("0", max(0, width-len(text))) + text
	}
	return sign + pad(year, 4) + "-" + pad(month, 2) + "-" + pad(day, 2)
}

// databaseFormulaCountable reports whether an amount of a unit added to a
// date stays within the days JavaScript counts from any day it counts.
func databaseFormulaCountable(amount float64, unit string) bool {
	days := databaseFormulaCasualDays[unit]
	if days == 0 {
		days = databaseFormulaClockUnits[unit] / 864e5
	}
	return math.Abs(amount*days) <= 2e8
}
