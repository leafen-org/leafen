package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database of one of Notion's kinds calls its rows without a title by its
// kind's word while it has neither a name of its own for them nor an
// untitled default template to name them by (databaseUntitledRowName): a
// task database "새 작업" ("New task") however it became one — Notion's
// database turned into a task database by hand names an untitled row so in
// its title's place, its window, its path, a sub-page's path below it, the
// sidebar and its template menu alike (seen 2026-11-26, where a plain
// database's reads "새 페이지" and a task tracker's "신규 작업") — a Sprints
// database "새 스프린트", one made as projects "새 프로젝트", as meeting notes
// "신규 회의", from the docs hub "새 문서". The web names them by the same
// kinds in the same order, in its reader's language (web/src/lib/
// untitledRowNames.ts databaseKindRowName); a list the server writes for a
// reader, and an agent's answer, carry the word in Korean or English as the
// server's other texts do (serverTextLanguage).

const (
	databaseRowKindSprint  = "sprint"
	databaseRowKindProject = "project"
	databaseRowKindMeeting = "meeting"
	databaseRowKindDocs    = "docs"
	databaseRowKindTask    = "task"
)

// databaseUntitledKindWords are each kind's word for its untitled rows, by
// language.
var databaseUntitledKindWords = map[string]map[string]string{
	accountLanguageKo: {
		databaseRowKindSprint: "새 스프린트", databaseRowKindProject: "새 프로젝트", databaseRowKindMeeting: "신규 회의",
		databaseRowKindDocs: "새 문서", databaseRowKindTask: "새 작업",
	},
	accountLanguageEn: {
		databaseRowKindSprint: "New sprint", databaseRowKindProject: "New project", databaseRowKindMeeting: "New meeting",
		databaseRowKindDocs: "New doc", databaseRowKindTask: "New task",
	},
}

// databaseUntitledRowKind is the kind a data source's untitled rows are
// named by, or "": a Sprints database first, then one made as projects, as
// meeting notes or from the docs hub, as the graph read marks them
// (markSprintDatabases, markProjectDatabases), then a task database.
func databaseUntitledRowKind(app core.App, dataSourceID string) (string, error) {
	if dataSourceID == "" {
		return "", nil
	}
	read := struct {
		Stored string `db:"database_type"`
		Origin string `db:"starter_template"`
		Tasks  int    `db:"tasks"`
		Sprint int    `db:"sprint"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[database_type]], [[source]].[[starter_template]],
			COALESCE(json_extract([[features]].[[tasks_json]], '$.enabled'), 0) AS [[tasks]],
			EXISTS(SELECT 1 FROM {{leafen_database_sprints}} WHERE [[sprints_data_source_id]] = [[source]].[[id]]) AS [[sprint]]
		FROM {{leafen_data_sources}} AS [[source]]
		LEFT JOIN {{leafen_database_task_features}} AS [[features]] ON [[features]].[[data_source_id]] = [[source]].[[id]]
		WHERE [[source]].[[id]] = {:id}`).Bind(dbx.Params{"id": dataSourceID}).One(&read)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", err
	case read.Sprint == 1:
		return databaseRowKindSprint, nil
	case read.Stored == notionDatabaseTypeProjects || read.Origin == notionDatabaseTypeProjects:
		return databaseRowKindProject, nil
	case read.Stored == notionDatabaseTypeMeetingNotes || read.Origin == databaseStarterMeetings:
		return databaseRowKindMeeting, nil
	case read.Origin == databaseStarterDocs:
		return databaseRowKindDocs, nil
	case read.Tasks == 1:
		return databaseRowKindTask, nil
	}
	return "", nil
}

// databaseUntitledRowNameIn is what a data source calls its pages without a
// title, in the language given: its name for them or its untitled default
// template's (databaseUntitledRowName), else its kind's word, or "".
func databaseUntitledRowNameIn(app core.App, dataSourceID, language string) (string, error) {
	name, err := databaseUntitledRowName(app, dataSourceID)
	if err != nil || name != "" {
		return name, err
	}
	kind, err := databaseUntitledRowKind(app, dataSourceID)
	if err != nil || kind == "" {
		return "", err
	}
	words, known := databaseUntitledKindWords[language]
	if !known {
		words = databaseUntitledKindWords[accountLanguageEn]
	}
	return words[kind], nil
}

// databaseUntitledRowNameFor is databaseUntitledRowNameIn in the language
// a reader reads the server's texts in: their own request's in ctx where
// there is one (serverTextLanguageIn), else the rule's later steps.
func databaseUntitledRowNameFor(ctx context.Context, app core.App, principalID, dataSourceID string) (string, error) {
	return databaseUntitledRowNameIn(app, dataSourceID, untitledRowReaderLanguage(ctx, app, principalID))
}

// untitledRowReaderLanguage is the language a reader's lists name untitled
// rows in.
func untitledRowReaderLanguage(ctx context.Context, app core.App, principalID string) string {
	language, err := serverTextLanguageIn(ctx, app, principalID)
	if err != nil {
		return accountLanguageKo
	}
	return language
}
