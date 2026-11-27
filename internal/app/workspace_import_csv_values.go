package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	workspaceImportCSVNumberPattern     = regexp.MustCompile(`^[-+]?(?:\d+|\d*\.\d+)$`)
	workspaceImportCSVPercentPattern    = regexp.MustCompile(`^(?:\d+|\d*\.\d+)%$`)
	workspaceImportCSVDigitsPattern     = regexp.MustCompile(`^\d+$`)
	workspaceImportCSVKoreanDatePattern = regexp.MustCompile(`^(\d{4})\s*년\s*(\d{1,2})\s*월\s*(\d{1,2})\s*일?$`)
	workspaceImportCSVYMDPattern        = regexp.MustCompile(`^(\d{4})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{1,2})$`)
	workspaceImportCSVUSDatePattern     = regexp.MustCompile(`^(\d{1,2})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{2,4})$`)
	workspaceImportCSVEmailPattern      = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	workspaceImportCSVSchemeURLPattern  = regexp.MustCompile(`(?i)^[a-z][a-z\d+.-]*://\S+$`)
	workspaceImportCSVBareURLPattern    = regexp.MustCompile(`(?i)^(?:[a-z\d](?:[a-z\d-]*[a-z\d])?\.)+[a-z]{2,}(?::\d+)?(?:[/?#]\S*)?$`)
	workspaceImportCSVURLSchemePattern  = regexp.MustCompile(`(?i)^[a-z][a-z\d+.-]*:(?:\D|$)`)
	// The days, and times, Notion's import reads a date column from (seen
	// 2026-11-12); web/src/lib/csvImportModel.ts reads the same.
	workspaceImportCSVDashDateTimePattern  = regexp.MustCompile(`(?i)^(\d{4})-(\d{1,2})-(\d{1,2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$`)
	workspaceImportCSVSlashYearDateTime    = regexp.MustCompile(`^(\d{4})/(\d{1,2})/(\d{1,2})(?: (\d{1,2}):(\d{2})(?::(\d{2}))?)?$`)
	workspaceImportCSVSlashDateTimePattern = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})(?: (\d{1,2}):(\d{2})(?::(\d{2}))?)?$`)
	workspaceImportCSVFullMonthPattern     = regexp.MustCompile(`(?i)^(january|february|march|april|may|june|july|august|september|october|november|december) (\d{1,2}), (\d{4})$`)
)

// inferWorkspaceImportCSVType is the type, and for a number its format, a
// column of a CSV in a ZIP takes from its filled values, as Notion's ZIP import
// infers it (seen 2026-11-12): a number when every value is one — signed, or
// a percent without a sign, with no thousands commas — a column of percents
// shown as percents; then a checkbox, an email, a URL (with a scheme or a bare
// domain) or a date (workspaceImportCSVDateTime) when every value reads as
// one; else text. The CSV window infers its columns itself, with a looser
// rule (csvImportModel.ts), and sends the types it chose.
func inferWorkspaceImportCSVType(values []string) (string, string) {
	if len(values) == 0 {
		return graphValueTypeRichText, ""
	}
	every := func(reads func(string) bool) bool {
		for _, value := range values {
			if !reads(value) {
				return false
			}
		}
		return true
	}
	switch {
	case every(workspaceImportCSVPercentPattern.MatchString):
		return graphValueTypeNumber, "percent"
	case every(func(value string) bool {
		return workspaceImportCSVNumberPattern.MatchString(value) || workspaceImportCSVPercentPattern.MatchString(value)
	}):
		return graphValueTypeNumber, ""
	case every(func(value string) bool { _, ok := parseWorkspaceImportCSVBoolean(value); return ok }):
		return graphValueTypeCheckbox, ""
	case every(workspaceImportCSVEmailPattern.MatchString):
		return graphValueTypeEmail, ""
	case every(func(value string) bool {
		return workspaceImportCSVSchemeURLPattern.MatchString(value) || workspaceImportCSVBareURLPattern.MatchString(value)
	}):
		return graphValueTypeURL, ""
	case every(func(value string) bool { _, ok := workspaceImportCSVDateTime(value); return ok }):
		return graphValueTypeDate, ""
	}
	return graphValueTypeRichText, ""
}

// parseWorkspaceImportCSVNumber reads a cell's number: an optional sign,
// digits and a point, its commas left out, a percent its number ("12%" is 12,
// as Notion's CSV window keeps it, seen 2026-11-12).
func parseWorkspaceImportCSVNumber(value string) (float64, bool) {
	normalized := strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(value), ",", ""), "%")
	if !workspaceImportCSVNumberPattern.MatchString(normalized) {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(normalized, 64)
	return parsed, err == nil
}

// workspaceImportCSVPercentValue is a cell of a column of percents as Notion's
// ZIP import keeps it: a fraction, "12%" 0.12 (seen 2026-11-12).
func workspaceImportCSVPercentValue(value string) (json.RawMessage, bool) {
	number, ok := parseWorkspaceImportCSVNumber(value)
	if !ok {
		return nil, false
	}
	parsed, err := strconv.ParseFloat(strconv.FormatFloat(number/100, 'g', 15, 64), 64)
	if err != nil {
		return nil, false
	}
	encoded, err := json.Marshal(parsed)
	return encoded, err == nil
}

// workspaceImportCSVURL is a URL cell as Notion's CSV window keeps it: one
// without a scheme gets "https://" ("example.com", seen 2026-11-12); a CSV in
// a ZIP keeps it as written.
func workspaceImportCSVURL(value string) string {
	url := strings.TrimSpace(value)
	if url == "" || workspaceImportCSVURLSchemePattern.MatchString(url) {
		return url
	}
	return "https://" + url
}

func parseWorkspaceImportCSVBoolean(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "y", "1", "checked", "check", "x", "on":
		return true, true
	case "false", "no", "n", "0", "unchecked", "off":
		return false, true
	default:
		return false, false
	}
}

// workspaceImportCSVDateTime reads a cell's day, or its day and time, as
// Notion's import reads a date column (seen 2026-11-12): a year-first day with
// dashes or slashes, or a month-first one with slashes (day-first when it
// can't be month-first), each with or without a 24-hour time after a space —
// or after a "T" — kept to the minute and on the reader's clock; a time
// written with its zone keeps it; a day written as Notion's export writes it
// ("November 1, 2026"). An abbreviated month, a Korean date, dots and a
// 12-hour time are not among them.
func workspaceImportCSVDateTime(value string) (string, bool) {
	raw := strings.TrimSpace(value)
	if workspaceImportCSVFullMonthPattern.MatchString(raw) {
		parsed, err := time.Parse("January 2, 2006", raw)
		return parsed.Format(time.DateOnly), err == nil
	}
	match := workspaceImportCSVDashDateTimePattern.FindStringSubmatch(raw)
	if match == nil {
		match = workspaceImportCSVSlashYearDateTime.FindStringSubmatch(raw)
	}
	day, ok := "", false
	if match != nil {
		day, ok = workspaceImportCSVDateFromParts(workspaceImportCSVInt(match[1]), workspaceImportCSVInt(match[2]), workspaceImportCSVInt(match[3]))
	} else if match = workspaceImportCSVSlashDateTimePattern.FindStringSubmatch(raw); match != nil {
		first, second, year := workspaceImportCSVInt(match[1]), workspaceImportCSVInt(match[2]), workspaceImportCSVInt(match[3])
		if day, ok = workspaceImportCSVDateFromParts(year, first, second); !ok {
			day, ok = workspaceImportCSVDateFromParts(year, second, first)
		}
	}
	if !ok {
		return "", false
	}
	if match[4] == "" {
		return day, true
	}
	hour, minute := workspaceImportCSVInt(match[4]), workspaceImportCSVInt(match[5])
	if hour > 23 || minute > 59 {
		return "", false
	}
	clock := fmt.Sprintf("%sT%02d:%02d", day, hour, minute)
	if len(match) < 8 || match[7] == "" {
		return clock, true
	}
	zone := strings.ToUpper(match[7])
	if zone != "Z" && !strings.Contains(zone, ":") {
		zone = zone[:3] + ":" + zone[3:]
	}
	return clock + ":00" + zone, true
}

func workspaceImportCSVInt(text string) int {
	number, _ := strconv.Atoi(text)
	return number
}

// parseWorkspaceImportCSVDate reads a cell's day, as a column chosen as a
// date takes one the import doesn't infer a date from: a Korean or year-first
// date; a month-first one, or a day-first one when it can't be month-first; a
// month's name; an ISO time's day.
func parseWorkspaceImportCSVDate(value string) (string, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" || workspaceImportCSVDigitsPattern.MatchString(raw) {
		return "", false
	}
	for _, candidate := range []struct {
		pattern *regexp.Regexp
		order   string
	}{
		{workspaceImportCSVKoreanDatePattern, "ymd"},
		{workspaceImportCSVYMDPattern, "ymd"},
		{workspaceImportCSVUSDatePattern, "mdy"},
	} {
		match := candidate.pattern.FindStringSubmatch(raw)
		if match == nil {
			continue
		}
		first, second, third := workspaceImportCSVInt(match[1]), workspaceImportCSVInt(match[2]), workspaceImportCSVInt(match[3])
		year, month, day := first, second, third
		if candidate.order == "mdy" {
			month, day, year = first, second, third
			if year < 100 {
				year += 2000
			}
		}
		if result, ok := workspaceImportCSVDateFromParts(year, month, day); ok {
			return result, true
		}
		// A date that can't be month-first is read day-first, as Notion
		// reads "13/02/2026" (seen 2026-10-01).
		if candidate.order == "mdy" {
			return workspaceImportCSVDateFromParts(year, day, month)
		}
		return "", false
	}
	for _, layout := range []string{time.RFC3339, "January 2, 2006", "Jan 2, 2006"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.Format(time.DateOnly), true
		}
	}
	return "", false
}

func workspaceImportCSVDateFromParts(year, month, day int) (string, bool) {
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if parsed.Year() != year || int(parsed.Month()) != month || parsed.Day() != day {
		return "", false
	}
	return parsed.Format(time.DateOnly), true
}
