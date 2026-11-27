package app

import (
	"fmt"
	"strings"
	"time"
)

// The words the Notion-shaped MCP tools (notion-*) answer in around what
// they read and do, in Leafen's own words and kept here so every tool says
// them alike: the heads of a fetch, the notes on a data source's SQLite
// columns, what a change waits on the person's yes with, and what a version
// read before something was made says. The tags, keys, addresses and values
// around them keep the form Notion's MCP gives them.

// mcpFetchPageHead opens a page's fetch: the page's address and the moment
// it was read.
func mcpFetchPageHead(address, at string) string {
	return "Page " + address + ", as notion-fetch read it at " + at + ":\n"
}

// mcpFetchViewHead opens a view's fetch.
func mcpFetchViewHead(address string) string {
	return "View {{" + address + "}}, as notion-fetch read it:\n"
}

// mcpFetchTitleLine names a database's or a data source's title in its
// fetch.
func mcpFetchTitleLine(kind, title string) string {
	return kind + " title: " + title
}

// What a database's fetch says over its data sources and its views, each
// listed after its line as its own fetch gives it. Notion's adds that each
// one's own fetch gives it whole (seen 2026-09-30); Leafen's doesn't, as
// it is already whole there and an agent that fetched it again read the
// same table twice (user 2026-11-26, an agent's real use).
var (
	mcpFetchDataSourcesLines = []string{"The database's data sources:"}
	mcpFetchViewsLines       = []string{"The database's views:"}
)

// What a data source's fetch says over its state and its table, and what a
// database page's says of its values.
const (
	mcpFetchDataSourceStateHead = "What can be set on this data source, as it is now:"
	mcpFetchReadOnlyNote        = "A property with `readOnly: true` is kept by Leafen or a sync: don't set its values through page updates."
	mcpFetchDataSourceTableHead = "The data source as a SQLite table. " + mcpSQLitePlainText +
		": a change to a value's formatting starts from the value notion-fetch gives its page, not SQL's."
	// A title's or text's value in <properties> is Notion's "v2" Markdown,
	// as Notion's API writes the same text (measured 2026-11-26): an agent
	// that added a line with ~ and _ and looked for it as written never
	// found it (an agent's real use, 2026-11-26). A page's fetch says it
	// after the page while a value there reads escaped (mcpFetchTextValueNote).
	mcpFetchTextValueForm = "A title or text in <properties> is Notion-flavored Markdown, its literal ~ _ * ` [ ] | $ \\ < escaped with \\ " +
		"and a line break <br />: written back as given it stays the same, and text written plain is found there so escaped."
	// SQL reads a title or text value as plain text, as Notion's does (seen
	// 2026-11-22 on a synthetic database: "a <mention>page</mention> 끝"
	// reads "a  끝", "[words](address)" "words", "**bold**" "bold"), but one
	// holding a mention, a link or an equation as fetch's Markdown, so that
	// written back it keeps them (databaseSQLTextValue, 2026-11-26).
	mcpSQLitePlainText = "SQL reads its title and text columns as plain text, and a value holding a mention, a link or an equation " +
		"as the Markdown notion-fetch gives it, which written back keeps them; a value's formatting alone it leaves out"
	// A date's or a place's columns, beside its schema entry.
	mcpFetchQueryColumnsUsage = "The columns SQL reads this property by; its own name isn't one of them."
	mcpFetchBlankPage         = "<blank-page>This page is empty.</blank-page>"
)

// The notes a data source's SQLite table gives its columns.
const (
	mcpSQLiteCreatedComment = "The page's own creation time, an ISO-8601 date and time that Leafen sets, in the person's time zone."
	mcpSQLiteEditedComment  = "The page's own last edit time, an ISO-8601 date and time that Leafen sets, in the person's time zone."
	mcpSQLiteCheckboxNote   = `"__YES__" when checked; "__NO__" or NULL when not`
	mcpSQLitePeopleNote     = "JSON array of zero or more user URLs"
	mcpSQLiteFilesNote      = `JSON array of uploaded files' IDs and of files linked from elsewhere as {"type":"external","name":…,"external":{"url":…}}, empty for none`
	mcpSQLiteDateStartNote  = "ISO-8601 date, or date and time in the person's time zone, so date() gives their day; set it as date:<column_name>:start, a time without an offset read in their zone."
	mcpSQLiteDateEndNote    = "ISO-8601 date, or date and time in the person's time zone, or empty: NULL for a single date, set for a range; set it as date:<column_name>:end."
	mcpSQLiteDateTimeNote   = "1 when the date has a time, 0 or NULL when it doesn't; set it as date:<column_name>:is_datetime."
)

// The notes a database page's short table gives a date's and a relation's
// columns (mcp_parent_schema_text.go); its head says the clock once.
const (
	mcpSQLiteDateStartShort = "ISO-8601 date, or date and time"
	mcpSQLiteDateEndShort   = "NULL for a single date, set for a range"
	mcpSQLiteDateTimeShort  = "1 when the date has a time"
)

// mcpFetchParentTableHead opens the short table of the data source a
// database page is in (mcp_parent_schema_text.go): what its columns are for,
// how much of an option list it shows and where the whole table is.
func mcpFetchParentTableHead(source string, shown int) string {
	return fmt.Sprintf("This page's data source in short, the columns its properties are set and queried by, each option list to its first %d names: "+
		"notion-fetch %s gives the table whole, every option and how SQL reads each column; fetch it before setting an option not listed here, "+
		"as a name no option has makes a new one.", shown, source)
}

// mcpQueryDataSourcesDescription is notion-query-data-sources', with how its
// times read and filter by day (person_clock.go).
const mcpQueryDataSourcesDescription = "Read rows from one authorized Leafen data source or run bounded read-only logical SQL across explicitly authorized data sources. " +
	"Times are in the time zone of the person this connection works as, SQL's as its wall-clock time: date() and bounds such as >= '2026-11-23' go by their day, as rows and view filters' days do. " +
	"A data source in the trash reads its rows as they stood, with a warning saying so."

// mcpTimesOnClock says, after a SQLite table's head, which clock its times
// are on (person_clock.go), the zone of the person the connection works as,
// and how to filter by their day.
func mcpTimesOnClock(clock *time.Location) string {
	now := time.Now()
	return "Its times are on " + mcpClockZoneNamed(clock, now) + ", the time zone of the person this connection works as: notion-fetch and rows give them " +
		"with that offset, SQL as its wall-clock time without it, so date() and bounds such as >= '" + now.In(clock).Format(time.DateOnly) +
		"' (their today; SQLite's 'now' is UTC) go by their day; a time set with another offset or Z is the moment it names."
}

// mcpTimesOnClockShort is mcpTimesOnClock as a database page's short table
// says it: the zone, and that a time set without an offset is read in it.
func mcpTimesOnClockShort(clock *time.Location) string {
	return "Its times are on " + mcpClockZoneNamed(clock, time.Now()) +
		", the time zone of the person this connection works as; a time set without an offset is read in it."
}

// mcpClockZoneNamed names the person's zone (personClockZone) and, for one
// not named by its offset, its offset now.
func mcpClockZoneNamed(clock *time.Location, now time.Time) string {
	zone := personClockZone(clock, now)
	if !strings.HasPrefix(zone, "UTC") {
		zone += " (" + now.In(clock).Format("UTC-07:00") + " now)"
	}
	return zone
}

// mcpSQLiteNamesNote is a multi-select's column: the names it may hold, as a
// JSON list.
func mcpSQLiteNamesNote(names string) string {
	return "JSON array of names from " + names + ", empty for none"
}

// mcpSQLiteRelationNote is a relation's column: its pages' URLs, what a
// page from elsewhere does to a write (mcpDatabasePageRelations), and the
// data source to fetch before querying them.
func mcpSQLiteRelationNote(source string) string {
	return "JSON array of URLs of pages in " + source + "; a write naming any other page changes nothing and says which; " +
		"fetch " + source + " before querying this column"
}

// mcpConfirmFirst ends what a tool that waits on the person's yes says
// without it.
const mcpConfirmFirst = "Show this to the person first, and call this tool again with user_confirmed: true only once they say yes."

// mcpVersionBeforeMade refuses a version tool's time before the page or
// database (kind) was made.
func mcpVersionBeforeMade(kind string) error {
	return mcpInvalid("The %s was made after that time.", kind)
}
