package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a migration's run left at Leafen's defaults because Notion's API
// doesn't give it and the reading of the person's Notion
// (notion_migration_formats_read.go) didn't match it — the report's "맞추지
// 못한 꾸밈", a kind for each the reader hands over: each page
// that isn't a row (its width, small text and font), each database's rows
// that came from Notion (the same; notionMigrationReportRowsSQL — a row made
// in Leafen has no Notion look to match), each database template the run made (the same,
// notion_migration_formats_left_templates.go), each database's page layout
// and the title its block keeps of its own, each board's columns as set by hand (their order, hiding and colors), each
// other view's groups as set by hand, each table's wrap, each form's title,
// description, questions and who may answer, a form's checkbox labels, which
// Leafen's form has no place for, each view's own default template (a view
// of a database with templates, until a reading says it has none), and each
// view whose rows people may have put in an order by hand, which Notion's
// API doesn't give (view_row_order_unavailable): its rows show newest first
// until matched to the order Notion's screen shows. A view's conditions on
// properties people deleted can't come at all, so the plan's agenda says them
// (notion_migration_formats_deleted_filters.go), and which blocks have
// comments only steers the run's comments step. What the reading matched is
// left out; Reason says whether the rest wasn't read (no reading, or the
// reader couldn't read it), was read and couldn't be matched, or was read
// in a view the run couldn't save, which keeps none of it.

// notionMigrationFormatSettings are the settings each kind leaves at its
// defaults, as the names to match them by.
var notionMigrationFormatSettings = map[string][]string{
	"page":     {"full_width", "small_text", "font"},
	"rows":     {"full_width", "small_text", "font"},
	"template": {"full_width", "small_text", "font"},
	// A database's page layout and the title its block keeps of its own,
	// which its rows' ancestor path names it by (database_block_title.go).
	"database": {"page_layout", "own_title"},
	"board":    {"group_order", "hidden_groups", "column_colors", "group_counts"},
	// A view other than a board that groups its rows.
	"groups":                        {"group_order", "hidden_groups"},
	"wrap":                          {"wrap"},
	"form":                          {"questions"},
	notionMigrationViewTemplateLeft: {"default_template"},
	"row_order":                     {"row_order"},
}

type leafenMCPNotionFormatLeft struct {
	// page, rows (a database's row pages), template (a database template),
	// database, board, groups (another view's groups), wrap (a table's),
	// form, view_template (a view's own default template) or row_order (a
	// view whose rows' order set by hand Notion didn't give).
	Kind string `json:"kind"`
	// The Leafen page or template, or the view.
	ID string `json:"id"`
	// The Notion page, template, database or view it came from.
	NotionID string `json:"notion_id"`
	Title    string `json:"title"`
	// Where a view or a database is, as the report names it before its own
	// name (notion_migration_formats_left_place.go).
	Place string `json:"place,omitempty"`
	// A view's layout (table, board, …), which names one left unnamed.
	Layout string `json:"layout,omitempty"`
	// How many row pages a database's rows are.
	Count    int      `json:"count,omitempty"`
	Settings []string `json:"settings"`
	// not_read, not_matched or not_saved (read, but the run couldn't save
	// the view it is of: nothing read of that view came).
	Reason string `json:"reason"`
}

// notionMigrationViewUnsaved is the reason of what a view the run couldn't
// save left (notionMigrationViewFormat.Unsaved).
const notionMigrationViewUnsaved = "not_saved"

// notionMigrationViewUnsavedLeft is what a view the run couldn't save leaves:
// all it read, a form's questions with the rest.
func notionMigrationViewUnsavedLeft(format notionMigrationViewFormat) []string {
	left := []string{"groups", "wrap", "rows"}
	if format.Form != nil {
		left = append(left, "questions")
	}
	return left
}

// notionMigrationFormatsLeftKinds counts them by kind, for the end of the
// import screen's own run: how many pages, row pages, templates, databases,
// boards, other grouped views, tables' wraps, forms, views' own default
// templates and views' row orders Leafen left at its defaults, which an
// agent can match from Notion (decision of 2026-11-12, "6의 보탬"); a
// form's checkbox labels no reading brings.
func notionMigrationFormatsLeftKinds(app core.App, jobID string) ([]notionMigrationFormatsLeftKind, error) {
	left, err := notionMigrationFormatsLeftAll(app, jobID)
	if err != nil {
		return nil, err
	}
	kinds := []notionMigrationFormatsLeftKind{}
	for _, kind := range []string{"page", "rows", "template", "database", graphViewTypeBoard, "groups", "wrap",
		graphViewTypeForm, notionMigrationViewTemplateLeft, "row_order"} {
		count := 0
		for _, item := range left {
			if item.Kind != kind {
				continue
			}
			if kind == "rows" {
				count += item.Count
			} else {
				count++
			}
		}
		if count > 0 {
			kinds = append(kinds, notionMigrationFormatsLeftKind{Kind: kind, Count: count})
		}
	}
	return kinds, nil
}

func notionMigrationFormatsLeftAll(app core.App, jobID string) ([]leafenMCPNotionFormatLeft, error) {
	if jobID == "" {
		return nil, nil
	}
	rows := []struct {
		Kind     string `db:"kind"`
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
		Title    string `db:"title"`
		ViewType string `db:"view_type"`
		Config   string `db:"view_config"`
		// The data source a view shows.
		Source string `db:"view_source"`
		Rows   int    `db:"rows"`
		// The page holding a view or a database, and a view's database.
		Holder     string `db:"holder"`
		HolderKind string `db:"holder_kind"`
		Database   string `db:"database"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[mapping]].[[kind]], [[mapping]].[[source_id]], [[mapping]].[[native_id]],
			COALESCE([[page]].[[title]], [[view]].[[name]], [[source]].[[title]], '') AS [[title]],
			COALESCE([[view]].[[type]], '') AS [[view_type]], COALESCE([[view]].[[config]], '') AS [[view_config]],
			COALESCE([[view]].[[data_source_id]], '') AS [[view_source]],
			COALESCE([[holder]].[[title]], '') AS [[holder]], COALESCE([[holder]].[[kind]], '') AS [[holder_kind]],
			COALESCE([[shown]].[[title]], '') AS [[database]],
			CASE WHEN [[mapping]].[[kind]] = 'data_source' THEN (
				` + notionMigrationReportRowsSQL("COUNT(*)", "[[mapping]].[[native_id]]") + `
			) ELSE 0 END AS [[rows]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		LEFT JOIN {{leafen_pages}} AS [[page]] ON [[mapping]].[[kind]] = 'page' AND [[page]].[[id]] = [[mapping]].[[native_id]]
		LEFT JOIN {{leafen_database_views}} AS [[view]] ON [[mapping]].[[kind]] = 'view' AND [[view]].[[id]] = [[mapping]].[[native_id]]
		LEFT JOIN {{leafen_data_sources}} AS [[source]] ON [[mapping]].[[kind]] = 'data_source' AND [[source]].[[id]] = [[mapping]].[[native_id]]
		LEFT JOIN {{leafen_data_sources}} AS [[shown]] ON [[shown]].[[id]] = [[view]].[[data_source_id]]
		LEFT JOIN {{leafen_pages}} AS [[holder]] ON [[holder]].[[id]] = COALESCE([[view]].[[page_id]], [[source]].[[page_id]])
		WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[kind]] IN ('page', 'data_source', 'view')
		ORDER BY [[mapping]].[[kind]] = 'view', [[mapping]].[[kind]], [[mapping]].[[created]], [[mapping]].[[source_id]]
	`).Bind(dbx.Params{"jobId": jobID}).All(&rows); err != nil {
		return nil, err
	}
	// The databases whose views keep an order people set by hand, which the
	// run reported it couldn't bring (notion_import_view_discovery.go).
	unordered := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[parent_source_id]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] = 'issue' AND [[text]] = {:reason}
	`).Bind(dbx.Params{"jobId": jobID, "reason": notionImportIssueViewRowOrderUnavailable}).Column(&unordered); err != nil {
		return nil, err
	}
	read, err := loadNotionMigrationFormatsMatched(app, jobID)
	if err != nil {
		return nil, err
	}
	templates, err := loadNotionMigrationTemplatesMade(app, jobID)
	if err != nil {
		return nil, err
	}
	left := []leafenMCPNotionFormatLeft{}
	orders := []leafenMCPNotionFormatLeft{}
	// Each of a row's: a view or a database named where it is.
	place, layout := "", ""
	add := func(kind, id, notionID, title string, count int) {
		reason, done := read.left(kind, notionID)
		if done {
			return
		}
		settings := notionMigrationFormatSettings[kind]
		// A form no reading read, or whose view the run couldn't save, takes
		// no answers either: who may answer comes with the reading
		// (notion_migration_formats_form.go).
		if kind == graphViewTypeForm && (reason == "not_read" || reason == notionMigrationViewUnsaved) {
			settings = append(slices.Clone(settings), "audience")
		}
		left = append(left, leafenMCPNotionFormatLeft{
			Kind: kind, ID: id, NotionID: notionID, Title: title, Place: place, Layout: layout, Count: count, Settings: settings, Reason: reason,
		})
	}
	for _, row := range rows {
		notionID := strings.TrimPrefix(row.SourceID, "notion-database-root-page:")
		place, layout = notionMigrationFormatsLeftPlace(row.Kind, row.Holder, row.HolderKind, row.Database), row.ViewType
		switch row.Kind {
		case "page":
			add("page", row.NativeID, notionID, row.Title, 0)
		case "data_source":
			add("database", row.NativeID, notionID, row.Title, 0)
			// Its templates are no rows, a row standing for one either.
			if rows := row.Rows - templates.pending[row.NativeID]; rows > 0 {
				add("rows", row.NativeID, notionID, row.Title, rows)
			}
			// Its templates' look goes with the database's reading, which
			// reads them; one with no look set the reader leaves out.
			for _, template := range templates.bySource[row.NativeID] {
				if reason, done := read.templateLeft(template.NotionID, notionID); !done {
					left = append(left, leafenMCPNotionFormatLeft{
						Kind: "template", ID: template.NativeID, NotionID: template.NotionID, Title: template.Title, Place: row.Title,
						Settings: notionMigrationFormatSettings["template"], Reason: reason,
					})
				}
			}
		case "view":
			// A view's source ID is its database's and the view's: "…:view:<id>".
			databaseID, viewID, found := strings.Cut(row.SourceID, ":view:")
			if found && (row.ViewType == graphViewTypeBoard || row.ViewType == graphViewTypeForm) {
				add(row.ViewType, row.NativeID, viewID, row.Title, 0)
			}
			config := struct {
				GroupBy         string `json:"groupBy"`
				DefaultTemplate string `json:"defaultTemplateId"`
			}{}
			_ = json.Unmarshal([]byte(row.Config), &config)
			if found && row.ViewType != graphViewTypeBoard && config.GroupBy != "" {
				add("groups", row.NativeID, viewID, row.Title, 0)
			}
			if found && row.ViewType == graphViewTypeTable {
				add("wrap", row.NativeID, viewID, row.Title, 0)
			}
			if found && notionMigrationViewTakesTemplates(row.ViewType) {
				reason, done := read.viewTemplateLeft(viewID, config.DefaultTemplate, row.Source, templates)
				if !done {
					left = append(left, leafenMCPNotionFormatLeft{
						Kind: notionMigrationViewTemplateLeft, ID: row.NativeID, NotionID: viewID, Title: row.Title, Place: place, Layout: layout,
						Settings: notionMigrationFormatSettings[notionMigrationViewTemplateLeft], Reason: reason,
					})
				}
			}
			if found && slices.Contains(unordered, databaseID) && notionMigrationViewKeepsHandOrder(row.ViewType, row.Config) {
				if reason, done := read.left("row_order", viewID); !done {
					orders = append(orders, leafenMCPNotionFormatLeft{
						Kind: "row_order", ID: row.NativeID, NotionID: viewID, Title: row.Title, Place: place, Layout: layout,
						Settings: notionMigrationFormatSettings["row_order"], Reason: reason,
					})
				}
			}
		}
	}
	// Last, as the import screen lists them (notionMigrationFormatsLeftKinds).
	return append(left, orders...), nil
}

// notionMigrationViewKeepsHandOrder reports whether a view shows its rows in
// the order people put them in: a view whose rows can be dragged
// (notionImportManualOrderViews) with no sorts, which order them instead.
func notionMigrationViewKeepsHandOrder(viewType, config string) bool {
	if _, manual := notionImportManualOrderViews[viewType]; !manual {
		return false
	}
	sorts := struct {
		Sorts []json.RawMessage `json:"sorts"`
	}{}
	return json.Unmarshal([]byte(config), &sorts) == nil && len(sorts.Sorts) == 0
}

// notionMigrationFormatsMatched is what a migration's reading matched, by
// Notion id, for the job its run made.
type notionMigrationFormatsMatched struct {
	items map[string]string
	// The Notion template each view read names as its own default.
	viewTemplates map[string]string
}

func loadNotionMigrationFormatsMatched(app core.App, jobID string) (notionMigrationFormatsMatched, error) {
	matched := notionMigrationFormatsMatched{items: map[string]string{}, viewTemplates: map[string]string{}}
	rows := []struct {
		Kind     string `db:"kind"`
		NotionID string `db:"notion_id"`
		Value    string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[format]].[[kind]], [[format]].[[notion_id]], [[format]].[[value]] FROM {{leafen_notion_migration_formats}} AS [[format]]
		JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration]].[[id]] = [[format]].[[migration_id]]
		WHERE [[migration]].[[import_job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).All(&rows); err != nil {
		return matched, err
	}
	for _, row := range rows {
		state := struct {
			Matched bool     `json:"matched"`
			Rows    []string `json:"rows"`
			Groups  []any    `json:"groups"`
			Wrap    *bool    `json:"wrap"`
			Left    []string `json:"left"`
			// A view's rows' order as kept once, or that Leafen didn't take it
			// (notion_migration_formats_parts.go).
			RowsOf       string `json:"rowsOf"`
			RowsNotTaken bool   `json:"rowsNotTaken"`
			// A view's own default template, when it has one.
			DefaultTemplate string `json:"defaultTemplate"`
			// A form's questions, when the reading read them.
			Form    json.RawMessage `json:"form"`
			Unsaved bool            `json:"unsaved"`
		}{}
		if json.Unmarshal([]byte(row.Value), &state) != nil {
			continue
		}
		key := row.Kind + ":" + canonicalNotionID(row.NotionID)
		switch row.Kind {
		case "reader":
		case "view":
			missed := "not_matched"
			if state.Unsaved {
				missed = notionMigrationViewUnsaved
			}
			// A view Notion keeps no groups, wrap or order of has none to match.
			for setting, kinds := range map[string][]string{"groups": {"board", "groups"}, "wrap": {"wrap"}, "rows": {"row_order"}} {
				done := !slices.Contains(state.Left, setting)
				if setting == "rows" && state.Rows == nil && state.RowsOf == "" && !state.RowsNotTaken || setting == "groups" && state.Groups == nil || setting == "wrap" && state.Wrap == nil {
					done = true
				}
				for _, kind := range kinds {
					matched.items[kind+":"+canonicalNotionID(row.NotionID)] = map[bool]string{true: "matched", false: missed}[done]
				}
			}
			// One with no default template of its own has none to match; one
			// with its own is matched once the view names the template the
			// run made of it, which the run makes last (viewTemplateLeft).
			matched.items[notionMigrationViewTemplateLeft+":"+canonicalNotionID(row.NotionID)] = map[bool]string{true: "matched", false: missed}[state.DefaultTemplate == ""]
			if state.DefaultTemplate != "" {
				matched.viewTemplates[canonicalNotionID(row.NotionID)] = canonicalNotionID(state.DefaultTemplate)
			}
			// A form read has its questions matched, or not, its checkbox
			// labels with them.
			if len(state.Form) > 0 && string(state.Form) != "null" {
				done := !slices.Contains(state.Left, "questions")
				matched.items[graphViewTypeForm+":"+canonicalNotionID(row.NotionID)] = map[bool]string{true: "matched", false: missed}[done]
			}
		default:
			matched.items[key] = map[bool]string{true: "matched", false: "not_matched"}[state.Matched]
		}
	}
	return matched, nil
}

// left says why an item is still at Leafen's defaults, or that it isn't: a
// database's rows go with the database's reading, which reads them all.
func (matched notionMigrationFormatsMatched) left(kind, notionID string) (string, bool) {
	lookup := kind
	if kind == "rows" {
		lookup = "database"
	}
	switch reason := matched.items[lookup+":"+canonicalNotionID(notionID)]; reason {
	case "matched":
		return "", true
	case "not_matched", notionMigrationViewUnsaved:
		if kind == "rows" {
			return "", true
		}
		return reason, false
	}
	return "not_read", false
}
