package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/unicode/norm"
)

// The migration report ends with what the person may want to tidy in what
// came, if anything (2026-11-16: after a real move, agents analysing it found
// by hand rows of the same name, an option written twice with and without a
// space, columns no row fills, a test row left in, and databases nobody had
// changed for months). Only suggestions, read from what the run made: nothing
// changes. Personal data is not looked for (the person's decision).

const (
	// The names a line shows.
	notionMigrationCleanupShown = 5
	// A database none of whose rows changed in Notion for this long.
	notionMigrationCleanupStale = 365 * 24 * time.Hour
)

var notionMigrationCleanupText = map[string]map[string]string{
	accountLanguageKo: {
		"title":     "정리 제안 (선택)",
		"lead":      "옮긴 내용에서 손볼 만한 것입니다. 리펜은 아무것도 바꾸지 않았으니, 필요한 것만 선택하세요.",
		"same":      "%s: 제목이 같은 행 — %s",
		"options":   "%s · %s: 띄어쓰기나 대소문자만 다른 옵션 — %s",
		"empty":     "%s: 어느 행에도 값이 없는 속성 — %s",
		"test":      "%s: 테스트용으로 보이는 행 — %s",
		"stale":     "%s: Notion에서 마지막으로 바뀐 날이 %s입니다(1년 넘게 그대로).",
		"more":      "외 %d개",
		"times":     "%s ×%d",
		"separator": ", ",
		// Many databases of one kind: how many, the databases folded under it.
		"sameGroup":    "제목이 같은 행이 있는 데이터베이스 %d개",
		"optionsGroup": "띄어쓰기나 대소문자만 다른 옵션이 있는 속성 %d개",
		"emptyGroup":   "어느 행에도 값이 없는 속성이 있는 데이터베이스 %d개",
		"testGroup":    "테스트용으로 보이는 행이 있는 데이터베이스 %d개",
		"staleGroup":   "Notion에서 1년 넘게 바뀌지 않은 데이터베이스 %d개",
		"item":         "%s — %s",
	},
	accountLanguageEn: {
		"title":     "Suggestions for tidying (optional)",
		"lead":      "What may be worth tidying in what came. Leafen changed none of it; pick what you need.",
		"same":      "%s: rows with the same title — %s",
		"options":   "%s · %s: options that differ only in spaces or case — %s",
		"empty":     "%s: properties no row has a value in — %s",
		"test":      "%s: rows that look like tests — %s",
		"stale":     "%s: last changed in Notion on %s (unchanged for over a year).",
		"more":      "and %d more",
		"times":     "%s ×%d",
		"separator": ", ",
		// Many databases of one kind: how many, the databases folded under it.
		"sameGroup":    "%d databases with rows of the same title",
		"optionsGroup": "%d properties with options that differ only in spaces or case",
		"emptyGroup":   "%d databases with properties no row has a value in",
		"testGroup":    "%d databases with rows that look like tests",
		"staleGroup":   "%d databases unchanged in Notion for over a year",
		"item":         "%s — %s",
	},
}

// notionMigrationTestTitles are row titles that read as a try rather than a
// record.
var notionMigrationTestTitles = map[string]bool{
	"test": true, "testing": true, "tests": true, "asdf": true, "qwer": true, "temp": true, "dummy": true, "sample": true,
	"hello": true, "foo": true, "테스트": true, "시험": true, "임시": true, "샘플": true, "더미": true, "안녕": true, // screen words: not shown (titles a test row has)
	"ㅁㄴㅇㄹ": true, "ㅇㅇ": true, "ㅎㅇ": true,
}

// notionMigrationCleanupKey is a name as the suggestions compare it: the same
// letters, whatever the spaces, case and Unicode form.
func notionMigrationCleanupKey(name string) string {
	var key strings.Builder
	for _, letter := range norm.NFC.String(strings.ToLower(name)) {
		if !unicode.IsSpace(letter) {
			key.WriteRune(letter)
		}
	}
	return key.String()
}

// notionMigrationCleanupLines reads the run's databases for suggestions:
// each kind a line of the report, the databases it is in folded under it when
// there are several (notion_migration_report_groups.go).
func notionMigrationCleanupLines(
	app core.App, jobID, language string, plan notionMigrationPlan, summary notionMigrationSummary, now time.Time,
) ([]notionMigrationReportGroup, error) {
	text := notionMigrationCleanupText[language]
	if text == nil {
		text = notionMigrationCleanupText[accountLanguageEn]
	}
	titles := map[string]string{}
	for _, database := range summary.Databases {
		titles[notionMigrationSourceKey(database.ID)] = database.Title
	}
	list := func(names []string) string {
		shown := names
		if len(names) > notionMigrationCleanupShown {
			shown = names[:notionMigrationCleanupShown]
		}
		joined := strings.Join(shown, text["separator"])
		if rest := len(names) - len(shown); rest > 0 {
			joined += text["separator"] + fmt.Sprintf(text["more"], rest)
		}
		return joined
	}
	grouping, lines := newNotionMigrationReportGrouping(), map[string]string{}
	add := func(kind, line, place, found string) {
		grouping.add(kind, fmt.Sprintf(text["item"], place, found), 1)
		lines[kind] = line
	}
	for _, planned := range plan.Databases {
		if planned.Action != "import" {
			continue
		}
		dataSourceID, err := notionImportMappedID(app, jobID, planned.SourceID, "data_source")
		if err != nil {
			return nil, err
		}
		if dataSourceID == "" {
			continue
		}
		name := cmpOr(strings.TrimSpace(planned.Title), titles[notionMigrationSourceKey(planned.SourceID)])
		if name = strings.TrimSpace(name); name == "" {
			name = notionMigrationReportText[language]["untitled"]
		}
		found, err := notionMigrationCleanupOf(app, jobID, dataSourceID, now)
		if err != nil {
			return nil, err
		}
		if len(found.same) > 0 {
			shown := list(found.sameShown(text["times"]))
			add("same", fmt.Sprintf(text["same"], name, shown), name, shown)
		}
		for _, options := range found.options {
			alike := strings.Join(options.names, " / ")
			add("options", fmt.Sprintf(text["options"], name, options.property, alike), name+" · "+options.property, alike)
		}
		if len(found.empty) > 0 {
			add("empty", fmt.Sprintf(text["empty"], name, list(found.empty)), name, list(found.empty))
		}
		if len(found.tests) > 0 {
			add("test", fmt.Sprintf(text["test"], name, list(found.tests)), name, list(found.tests))
		}
		if found.lastChanged > 0 {
			// The day on the person's clock, as the report's other times.
			day := time.UnixMilli(found.lastChanged).In(now.Location()).Format("2006-01-02")
			add("stale", fmt.Sprintf(text["stale"], name, day), name, day)
		}
	}
	return grouping.list([]string{"same", "options", "empty", "test", "stale"}, func(group *notionMigrationReportGroup) {
		group.Say = fmt.Sprintf(text[group.Key+"Group"], group.Count)
		if group.Total == 1 {
			group.One = lines[group.Key]
		}
	}), nil
}

// renderNotionMigrationCleanup is the report's section of suggestions, or
// nothing without any.
func renderNotionMigrationCleanup(language string, groups []notionMigrationReportGroup) string {
	if len(groups) == 0 {
		return ""
	}
	text := notionMigrationCleanupText[language]
	if text == nil {
		text = notionMigrationCleanupText[accountLanguageEn]
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n## %s\n\n%s\n\n", text["title"], text["lead"])
	writeNotionMigrationReportGroups(func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }, language, 0, groups)
	return out.String()
}

type notionMigrationCleanupOptions struct {
	property string
	names    []string
}

type notionMigrationCleanupFound struct {
	// Titles several rows share, with how many.
	same  map[string]int
	order []string
	// Option names of a property that differ only in spaces or case.
	options []notionMigrationCleanupOptions
	// Properties no row fills, and titles that read as tests.
	empty, tests []string
	// When the database's rows last changed in Notion, when that's over a
	// year before the run; 0 otherwise.
	lastChanged int64
}

func (found notionMigrationCleanupFound) sameShown(times string) []string {
	shown := make([]string, 0, len(found.order))
	for _, title := range found.order {
		shown = append(shown, fmt.Sprintf(times, title, found.same[title]))
	}
	return shown
}

// notionMigrationShownRowsSQL is a database's rows as its views show them,
// each its page — of the data source the SQL dataSource names: a template,
// which waits in the database's 새로 만들기 menu, a row in trash and a page
// the run still stages are no rows (2026-11-24: a run again's suggestions
// named two templates of one name as rows of the same title).
func notionMigrationShownRowsSQL(columns, dataSource string) string {
	return `SELECT ` + columns + ` FROM {{leafen_database_rows}} [[report_row]]
		JOIN {{leafen_pages}} [[report_page]] ON [[report_page]].[[id]] = [[report_row]].[[id]]
		WHERE [[report_row]].[[data_source_id]] = ` + dataSource + ` AND [[report_page]].[[in_trash]] = FALSE
			AND [[report_page]].[[database_template]] = FALSE AND [[report_page]].[[deletion_job_id]] = ''
			AND [[report_page]].[[notion_import_staging]] = FALSE AND ` + databaseRowNotTrashedSQL("[[report_row]].[[id]]")
}

// notionMigrationReportRowsSQL is the report's one query of a database's
// rows: of those its views show, the ones that came from the source — each a
// row the run's job ({:jobId}) maps, this run's or an earlier one's. A row
// made in Leafen is none of what came: the report says what came, and how
// it came (2026-11-24: a run again's report counted a row made in Leafen
// among the rows whose look Leafen couldn't match to Notion). Whether a
// property holds any value is the one thing read of every row the views
// show, as a value a row made in Leafen holds is the property's too.
func notionMigrationReportRowsSQL(columns, dataSource string) string {
	return notionMigrationShownRowsSQL(columns, dataSource) + ` AND EXISTS (SELECT 1 FROM {{leafen_notion_import_mappings}} [[came]]
			WHERE [[came]].[[job_id]] = {:jobId} AND [[came]].[[kind]] = 'row' AND [[came]].[[native_id]] = [[report_row]].[[id]])`
}

// notionMigrationCleanupOf reads one database's suggestions. Titles are
// compared by a key only Go makes (notionMigrationCleanupKey), so every row
// that came is read, in its order, one at a time: the rows of the same title
// and the test-looking rows are counted over all of them, as the empty
// properties and the last change are (2026-11-24: the first 5,000 rows were
// read as if they were all, a 6,000-row database's later rows of a same
// title and test rows left out).
func notionMigrationCleanupOf(app core.App, jobID, dataSourceID string, now time.Time) (notionMigrationCleanupFound, error) {
	found := notionMigrationCleanupFound{same: map[string]int{}}
	titles, err := app.DB().NewQuery(notionMigrationReportRowsSQL("[[report_page]].[[title]]", "{:dataSourceId}") + `
		ORDER BY [[report_row]].[[position]], [[report_row]].[[id]]
	`).Bind(dbx.Params{"jobId": jobID, "dataSourceId": dataSourceID}).Rows()
	if err != nil {
		return found, err
	}
	defer titles.Close()
	rowCount, byKey, first := 0, map[string]int{}, map[string]string{}
	for titles.Next() {
		var title string
		if err := titles.Scan(&title); err != nil {
			return found, err
		}
		rowCount++
		if title = strings.TrimSpace(title); title == "" {
			continue
		}
		key := notionMigrationCleanupKey(title)
		if notionMigrationTestTitles[key] {
			found.tests = append(found.tests, title)
		}
		byKey[key]++
		if _, seen := first[key]; !seen {
			first[key] = title
		}
	}
	if err := titles.Err(); err != nil {
		return found, err
	}
	titles.Close()
	for key, count := range byKey {
		if count > 1 {
			found.same[first[key]] = count
			found.order = append(found.order, first[key])
		}
	}
	sort.Strings(found.order)
	properties := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		Type   string `db:"type"`
		Config string `db:"config"`
		Filled int    `db:"filled"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[name]], [[property]].[[type]], [[property]].[[config]],
			(SELECT COUNT(*) FROM {{leafen_database_row_values}} [[value]]
				WHERE [[value]].[[property_id]] = [[property]].[[id]] AND [[value]].[[value]] NOT IN ('', 'null', '""', '[]', '{}', 'false')
					AND [[value]].[[row_id]] IN (` + notionMigrationShownRowsSQL("[[report_row]].[[id]]", "[[property]].[[data_source_id]]") + `)) AS [[filled]]
		FROM {{leafen_database_properties}} [[property]]
		WHERE [[property]].[[data_source_id]] = {:dataSourceId} AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
			AND EXISTS (SELECT 1 FROM {{leafen_notion_import_mappings}} [[came]]
				WHERE [[came]].[[job_id]] = {:jobId} AND [[came]].[[kind]] = 'property' AND [[came]].[[native_id]] = [[property]].[[id]])
		ORDER BY [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{"jobId": jobID, "dataSourceId": dataSourceID}).All(&properties); err != nil {
		return found, err
	}
	for _, property := range properties {
		_, stored := databasePropertyConversionStoredTypes[property.Type]
		if stored && property.Type != graphValueTypeTitle && property.Filled == 0 && rowCount > 1 {
			found.empty = append(found.empty, property.Name)
		}
		if notionMigrationOptionTypes[property.Type] {
			if names := notionMigrationCleanupAlikeOptions(property.Config); len(names) > 0 {
				for _, alike := range names {
					found.options = append(found.options, notionMigrationCleanupOptions{property: property.Name, names: alike})
				}
			}
		}
	}
	var lastChanged int64
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[author]].[[edited_at]]), 0) FROM {{leafen_notion_imported_authors}} [[author]]
		WHERE [[author]].[[page_id]] IN (` + notionMigrationReportRowsSQL("[[report_page]].[[id]]", "{:dataSourceId}") + `)
	`).Bind(dbx.Params{"jobId": jobID, "dataSourceId": dataSourceID}).Row(&lastChanged); err != nil {
		return found, err
	}
	if lastChanged > 0 && now.Sub(time.UnixMilli(lastChanged)) > notionMigrationCleanupStale {
		found.lastChanged = lastChanged
	}
	return found, nil
}

// notionMigrationCleanupAlikeOptions are an option property's names that
// differ only in spaces, case or Unicode form, each group in its order.
func notionMigrationCleanupAlikeOptions(config string) [][]string {
	decoded := map[string]any{}
	if json.Unmarshal([]byte(config), &decoded) != nil {
		return nil
	}
	options := mcpPropertyOptions(decoded)
	groups, order := map[string][]string{}, []string{}
	for _, option := range options {
		key := notionMigrationCleanupKey(option.Name)
		if key == "" {
			continue
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], option.Name)
	}
	alike := [][]string{}
	for _, key := range order {
		if len(groups[key]) > 1 {
			alike = append(alike, groups[key])
		}
	}
	return alike
}
