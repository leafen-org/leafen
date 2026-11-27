package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's row without a title is called by the name the database keeps
// for its untitled pages (data_source_untitled_name.go), else after its
// untitled default template, wherever Notion names it, whichever way the row
// was made (seen 2026-11-07 and 2026-11-08 in a task tracker that calls them
// "신규 작업": its board card, its page's title, window and path, a search's
// results, the trash and a relation's picker all read "신규 작업"); its
// table cell stays blank, and a mention's search, which matches titles,
// finds none. Without either it goes by its database's kind ("새 작업" in a
// task database, database_untitled_row_kind.go), else it is a new page. The
// name travels with what lists rows: a search's, the trash's and a
// mention's rows, a row page and its related pages, the reader's tasks,
// recent edits and backlinks (databaseUntitledRowNameFor, the kind's word
// in the reader's language), and a graph read's data sources and the
// database an action or a relation reads, which mark the kind for the web
// to name it in its own (markProjectDatabases, markSprintDatabases).

// databaseTemplateNameSQL is a template's name as its database's menus show
// it: its page's title, or the name it was made with while it has none (a
// task tracker's "신규 작업", whose page is untitled). The search, the trash,
// Home's recent edits and the rows called after a default template all name
// a template so, as Notion's names a template by its page's title alone.
func databaseTemplateNameSQL(template string) string {
	return `COALESCE(NULLIF(trim(` + template + `.[[title]]), ''), trim(` + template + `.[[name]]))`
}

// databaseUntitledRowName is what a data source calls its pages without a
// title: the name it keeps for them, which Notion's task tracker keeps
// whichever template is its default (seen 2026-11-11: "신규 작업" with
// "Tmpl B" made default), else its live default template's name while that
// template's page has no title of its own, or "". Notion names its
// database's untitled pages, rows and templates alike, by its word for its
// items, not by a titled default template, which gives the rows it makes its
// title (seen 2026-11-11: an untitled row and a new template where "Tmpl A"
// is the default read "새 페이지").
func databaseUntitledRowName(app core.App, dataSourceID string) (string, error) {
	if dataSourceID == "" {
		return "", nil
	}
	name, err := dataSourceUntitledName(app, dataSourceID)
	if err != nil || name != "" {
		return name, err
	}
	err = app.DB().NewQuery(`
		SELECT CASE WHEN trim(COALESCE([[template]].[[title]], '')) = '' THEN trim(COALESCE([[template]].[[name]], '')) ELSE '' END
		FROM {{leafen_database_templates}} AS [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} AS [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[data_source_id]] = {:id} AND [[template]].[[is_default]] = 1
			AND COALESCE([[state]].[[deleted_at]], '') = ''
		ORDER BY [[template]].[[position]], [[template]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// markUntitledRowNames tells a read what each of its data sources' rows
// without a title are called, and the name the database keeps for them,
// which goes before its templates as they change on screen.
func markUntitledRowNames(app core.App, items []graphReadItem) error {
	for _, item := range items {
		if item.Kind != graphReadKindDataSource {
			continue
		}
		source, _ := item.Value["id"].(string)
		own, err := dataSourceUntitledName(app, source)
		if err != nil {
			return err
		}
		name := own
		if own != "" {
			item.Value["untitledName"] = own
		} else if name, err = databaseUntitledRowName(app, source); err != nil {
			return err
		}
		if name != "" {
			item.Value["untitledRowName"] = name
		}
	}
	return nil
}

// databaseUntitledRowNames reads each data source's untitled-row name once
// for a reader's list of rows from several databases, its kind's word in
// the reader's language (databaseUntitledRowNameFor).
type databaseUntitledRowNames struct {
	app      core.App
	language string
	read     map[string]string
}

func newDatabaseUntitledRowNames(ctx context.Context, app core.App, principalID string) *databaseUntitledRowNames {
	return &databaseUntitledRowNames{app: app, language: untitledRowReaderLanguage(ctx, app, principalID), read: map[string]string{}}
}

func (names *databaseUntitledRowNames) of(dataSourceID string) (string, error) {
	if name, ok := names.read[dataSourceID]; ok {
		return name, nil
	}
	name, err := databaseUntitledRowNameIn(names.app, dataSourceID, names.language)
	if err != nil {
		return "", err
	}
	names.read[dataSourceID] = name
	return name, nil
}
