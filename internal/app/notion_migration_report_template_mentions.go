package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's report names a database template as the template, in words:
// a mention of a template's page reads as an untitled page, the template being
// no row its title cell names (2026-11-23: a move's report said a template's
// file and linked database each on an empty mention). Every mention the report
// writes passes here, whichever section wrote it, so a row the run made into a
// template after the report named it (notion_import_row_stamps.go, whose run
// writes the report again at its end) is said as its template.

// notionMigrationReportTemplateMentions writes each of markdown's mentions of
// a database template's page as the template by its name.
func notionMigrationReportTemplateMentions(app core.App, language, markdown string) (string, error) {
	found := notionMigrationReportWrittenMention.FindAllStringSubmatch(markdown, -1)
	if len(found) == 0 {
		return markdown, nil
	}
	ids := make([]string, 0, len(found))
	for _, mention := range found {
		ids = append(ids, mention[1])
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return markdown, err
	}
	templates := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[title]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[database_template]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&templates); err != nil || len(templates) == 0 {
		return markdown, err
	}
	text := notionMigrationReportText[language]
	if text == nil {
		text = notionMigrationReportText[accountLanguageEn]
	}
	said := make(map[string]string, len(templates))
	for _, template := range templates {
		said[template.ID] = notionImportIssuePlaceTitle(text, template.Title, true)
	}
	return notionMigrationReportWrittenMention.ReplaceAllStringFunc(markdown, func(mention string) string {
		if words, isTemplate := said[notionMigrationReportWrittenMention.FindStringSubmatch(mention)[1]]; isTemplate {
			return words
		}
		return mention
	}), nil
}
