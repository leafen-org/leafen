package app

// A date mention shows its date in one of Notion's formats (seen
// 2026-10-02): the full date, the short date, month/day/year,
// day/month/year, year/month/day, or relative to today. A mention made in
// the editor takes its maker's default, the account's "날짜 형식"
// (account_display_preferences.go); one made through the API shows its
// full date, as Notion's does. A mention without one reads as relative, as
// Leafen's mentions showed before they had a format.

const (
	dateMentionFormatFull     = "full"
	dateMentionFormatRelative = "relative"
)

var dateMentionFormats = map[string]struct{}{
	dateMentionFormatFull: {}, "short": {}, "month_day_year": {}, "day_month_year": {}, "year_month_day": {},
	dateMentionFormatRelative: {},
}

func validDateMentionFormat(format string) bool {
	_, ok := dateMentionFormats[format]
	return ok
}

// A date mention with a time shows it in 12 hours (its default, kept as no
// value), 24 hours or not at all, as Notion's "시간 형식" (seen 2026-10-05).
func validDateMentionTimeFormat(format string) bool {
	return format == "12" || format == "24" || format == "hidden"
}
