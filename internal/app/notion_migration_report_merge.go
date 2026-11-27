package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The report page is the run's, written again by a run again, a check
// again and a reading of the person's Notion after the run; what the person
// changed on it since the run last wrote it stays, as a run again keeps
// what a person changed in Leafen (2026-11-04, a run again's titles: a
// person's change stays). Line by line against the run's own report as it
// last wrote it (leafen_notion_migrations.report_markdown: its lines on the
// page and those the person took away, theirs left out), each line by what
// it says, as written or as the page reads it back
// (notion_migration_report_line_key.go), a toggle whole as one thing
// (notion_migration_report_units.go): a line the person added, changed
// or took away stays so, and the run's own lines become the new report's.
// A page the run never wrote this way, or that the person left as it was,
// is the new report whole. Lines are matched by the lines that differ
// (line_matches.go); where more differ than a match follows, the lines
// between those the reports begin and end with alike are merged as one
// stretch, so the person's lines stay there too, only placed less closely.

// notionMigrationReportLine is a line of the merged report: the run's, the
// person's, or the run's that the person took away, which the page leaves
// out and the run keeps as its own.
type notionMigrationReportLine struct {
	text   string
	person bool
	away   bool
}

// writeNotionMigrationReportPage writes the report's Markdown on its page,
// keeping what the person changed there; made is a page this write made.
func (r *durableJobRunner) writeNotionMigrationReportPage(
	ctx context.Context, job storedJob, migrationID, pageID string, made bool,
	markdown string, report notionMigrationRunReport,
) error {
	written := ""
	if !made {
		if err := r.app.DB().NewQuery(`SELECT [[report_markdown]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": migrationID}).Row(&written); err != nil {
			return err
		}
	}
	// Each version of the report is its own change: one written again once the
	// run stopped or ended, after a check again, or on what it last wrote
	// (a reading, another, then the first again) goes through after the first.
	digest := sha256.Sum256([]byte("notion-migration-report\x00" + job.ID + "\x00" + notionMigrationReportCheck(report) + markdown +
		"\x00" + written))
	lines, key := []notionMigrationReportLine{}, notionMigrationReportLineKeys(r.app)
	result, err := r.ygo.replacePageMarkdown(ctx, r.app, pageMarkdownReplacement{
		PrincipalID: job.Owner, PublicPageID: pageID, MutationID: "notion-migration-report:" + job.ID + ":" + hex.EncodeToString(digest[:6]),
		Reader: principalMarkdownReader(job.Owner), PrepareInput: principalMarkdownMentionInput(job.Owner),
		RequestHash: hex.EncodeToString(digest[:]),
		Authorize: func(tx core.App) (pageAuthorization, error) {
			return authorizePageBody(tx, job.Owner, pageID, permissionWorkspaceWrite)
		},
		Next: func(current string) (string, bool, error) {
			lines = mergeNotionMigrationReport(written, current, markdown, key)
			page := []string{}
			for _, line := range lines {
				if !line.away {
					page = append(page, line.text)
				}
			}
			return strings.Join(page, "\n"), true, nil
		},
	})
	if err != nil || result.Receipt.Duplicate {
		return err
	}
	_, err = r.app.DB().NewQuery(`UPDATE {{leafen_notion_migrations}} SET [[report_markdown]]={:markdown} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"markdown": notionMigrationReportRuns(lines), "id": migrationID}).Execute()
	return err
}

// notionMigrationReportRuns is the run's own report after a write: its
// lines on the page, as written or as the page holds them, and those the
// person took away. (Matching the page's lines as read back by place took
// a person's line for one of the run's once the page read back as many
// lines as were written, its blank lines after the headings among them.)
func notionMigrationReportRuns(lines []notionMigrationReportLine) string {
	runs := []string{}
	for _, line := range lines {
		if !line.person {
			runs = append(runs, line.text)
		}
	}
	return strings.Join(runs, "\n")
}

// notionMigrationReportSpan is lines of a report with what each says, a
// toggle whole as one (notion_migration_report_units.go).
type notionMigrationReportSpan struct {
	texts, keys []string
}

func newNotionMigrationReportSpan(markdown string, key func(string) string) notionMigrationReportSpan {
	span := notionMigrationReportSpan{}
	span.texts, span.keys = notionMigrationReportUnits(markdown, key)
	return span
}

func (span notionMigrationReportSpan) slice(from, to int) notionMigrationReportSpan {
	return notionMigrationReportSpan{texts: span.texts[from:to], keys: span.keys[from:to]}
}

// where is the span's lines whose key keep holds.
func (span notionMigrationReportSpan) where(keep func(string) bool) notionMigrationReportSpan {
	kept := notionMigrationReportSpan{}
	for index, key := range span.keys {
		if keep(key) {
			kept.texts, kept.keys = append(kept.texts, span.texts[index]), append(kept.keys, key)
		}
	}
	return kept
}

// mergeNotionMigrationReport is the next report on a page that holds
// current, where the run last wrote written: three ways, line by line, each
// line by what key says it says.
func mergeNotionMigrationReport(written, current, next string, key func(string) string) []notionMigrationReportLine {
	after := newNotionMigrationReportSpan(next, key)
	whole := func() []notionMigrationReportLine {
		lines := []notionMigrationReportLine{}
		for _, text := range after.texts {
			lines = append(lines, notionMigrationReportLine{text: text})
		}
		return lines
	}
	if written == "" {
		return whole()
	}
	base, now := newNotionMigrationReportSpan(written, key), newNotionMigrationReportSpan(current, key)
	if slices.Equal(now.keys, base.keys) || slices.ContainsFunc(now.keys, notionMigrationReportTitleAsWords) {
		return whole()
	}
	toNow, _ := lineMatches(base.keys, now.keys)
	toAfter, _ := lineMatches(base.keys, after.keys)
	merged := []notionMigrationReportLine{}
	lastBase, lastNow, lastAfter := -1, -1, -1
	// The lines all three hold alike hold the rest in place, as the new
	// report writes them.
	for index := 0; index <= len(base.keys); index++ {
		atNow, atAfter := len(now.keys), len(after.keys)
		if index < len(base.keys) {
			if atNow, atAfter = toNow[index], toAfter[index]; atNow < 0 || atAfter < 0 {
				continue
			}
		}
		merged = append(merged, notionMigrationReportBetween(
			base.slice(lastBase+1, index), now.slice(lastNow+1, atNow), after.slice(lastAfter+1, atAfter))...)
		if index < len(base.keys) {
			merged = append(merged, notionMigrationReportLine{text: after.texts[atAfter]})
		}
		lastBase, lastNow, lastAfter = index, atNow, atAfter
	}
	return merged
}

// notionMigrationReportBetween is what goes between two lines all three
// hold: the new report's where the person left the run's lines, the
// person's where the report didn't change; where both did, the person's
// own lines stay in their places and the new report's take the place of
// the run's, but for those the person took away — all of them, when the
// person left none of the run's lines there.
func notionMigrationReportBetween(written, current, next notionMigrationReportSpan) []notionMigrationReportLine {
	runs, kept := map[string]bool{}, map[string]bool{}
	for _, key := range written.keys {
		runs[key] = true
	}
	for _, key := range current.keys {
		kept[key] = true
	}
	lines := func(span notionMigrationReportSpan, person, away func(string) bool) []notionMigrationReportLine {
		out := make([]notionMigrationReportLine, 0, len(span.texts))
		for index, text := range span.texts {
			out = append(out, notionMigrationReportLine{text: text, person: person(span.keys[index]), away: away(span.keys[index])})
		}
		return out
	}
	never := func(string) bool { return false }
	always := func(string) bool { return true }
	persons := func(key string) bool { return !runs[key] }
	takenAway := func(key string) bool { return runs[key] && !kept[key] }
	personsOnly := !slices.ContainsFunc(current.keys, func(key string) bool { return runs[key] })
	switch {
	case slices.Equal(current.keys, written.keys):
		return lines(next, never, never)
	case slices.Equal(current.keys, next.keys):
		return lines(current, never, never)
	case len(written.keys) == 0:
		return append(lines(current, always, never), lines(next, never, never)...)
	case slices.Equal(next.keys, written.keys):
		return append(lines(current, persons, never), lines(next.where(takenAway), never, always)...)
	case personsOnly:
		return append(lines(current, always, never), lines(next, never, always)...)
	}
	merged, placed := []notionMigrationReportLine{}, false
	for index, key := range current.keys {
		if !runs[key] {
			merged = append(merged, notionMigrationReportLine{text: current.texts[index], person: true})
		} else if !placed {
			merged, placed = append(merged, lines(next, never, takenAway)...), true
		}
	}
	return merged
}
