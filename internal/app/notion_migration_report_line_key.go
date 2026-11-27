package app

import (
	"cmp"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A line of a migration's report is the same line whether the run wrote it
// or its page reads it back (2026-11-17: once a page and a template the
// source no longer gave came back, their lines stayed on the report as if a
// person had written them, the heading above them gone): the run writes a
// mention as <mention-page url="ID"/>, and the page reads it back as
// <mention url="/p/ID">its title</mention>, or, for the page that holds a
// full-page database, as the database's <mention url="/p/…" type="database">;
// and the page reads words back with Markdown's escapes (\[, \*, \|). The
// report's merge (notion_migration_report_merge.go) compares lines by what
// they say: each mention by the page it names, words without their escapes.

var (
	notionMigrationReportWrittenMention = regexp.MustCompile(`<mention-page url="([^"]*)"\s*/>`)
	notionMigrationReportReadMention    = regexp.MustCompile(
		`<mention url="([^"]*)"(?:\s+[A-Za-z][A-Za-z-]*="[^"]*")*\s*(?:/>|>(?:\\.|[^\\])*?</mention>)`)
	notionMigrationReportEscape = regexp.MustCompile("\\\\([!-/:-@\\[-`{-~])")
)

// notionMigrationReportLineKeys says what a report's line says, as the
// merge compares it.
func notionMigrationReportLineKeys(app core.App) func(string) string {
	pages := map[string]string{}
	// The page a mention names: a database's, the page that holds it.
	page := func(address string) string {
		id := address
		if at := strings.LastIndex(id, "/p/"); at >= 0 {
			id = id[at+len("/p/"):]
		}
		if at := strings.IndexAny(id, "#?"); at >= 0 {
			id = id[:at]
		}
		id = canonicalNotionID(id)
		if known, found := pages[id]; found {
			return known
		}
		holder := ""
		// One no database answers to is a page's.
		_ = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": id}).Row(&holder)
		pages[id] = cmp.Or(holder, id)
		return pages[id]
	}
	named := func(pattern *regexp.Regexp, line string) string {
		out, last := strings.Builder{}, 0
		for _, found := range pattern.FindAllStringSubmatchIndex(line, -1) {
			out.WriteString(line[last:found[0]])
			out.WriteString("\x00mention:" + page(line[found[2]:found[3]]) + "\x00")
			last = found[1]
		}
		out.WriteString(line[last:])
		return out.String()
	}
	return func(line string) string {
		if strings.Contains(line, "<mention") {
			line = named(notionMigrationReportReadMention, named(notionMigrationReportWrittenMention, line))
		}
		if strings.Contains(line, `\`) {
			line = notionMigrationReportEscape.ReplaceAllString(line, "$1")
		}
		return line
	}
}
