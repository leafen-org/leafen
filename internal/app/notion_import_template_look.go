package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A template an import makes of a Notion template takes the look of the page
// the import read it as — its cover, and its font, small text, full width
// and table of contents as its "..." menu sets them — as a page made from
// it takes the template's (database_toolbar_template_row.go); its icon is
// the template's own (notionImportTemplateIcon). A template a person
// deleted stays as it is.

// copyNotionImportTemplateLook gives a template its row page's look.
func copyNotionImportTemplateLook(app core.App, workspaceID, rowID, templateID string) error {
	look := struct {
		Cover                  string `db:"cover"`
		CoverPosition          int    `db:"cover_position"`
		Font                   string `db:"font"`
		SmallText              bool   `db:"small_text"`
		FullWidth              bool   `db:"full_width"`
		TableOfContentsDisplay string `db:"table_of_contents_display"`
	}{}
	rows := []struct {
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[record]].[[icon]], [[record]].[[icon_type]] FROM {{leafen_pages}} AS [[record]]
		WHERE [[record]].[[id]]={:templateId} AND [[record]].[[database_template]] AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_toolbar_template_state}} AS [[state]]
			WHERE [[state]].[[template_id]]={:templateId} AND [[state]].[[deleted_at]]!='')
	`).Bind(dbx.Params{"templateId": templateID}).All(&rows); err != nil || len(rows) != 1 {
		return err
	}
	if err := app.DB().NewQuery(`
		SELECT [[cover]], [[cover_position]], [[font]], [[small_text]], [[full_width]], [[table_of_contents_display]]
		FROM {{leafen_pages}} WHERE [[id]]={:rowId} AND [[workspace]]={:workspaceId}
	`).Bind(dbx.Params{"rowId": rowID, "workspaceId": workspaceID}).One(&look); err != nil {
		return nil
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[cover]]={:cover}, [[cover_position]]={:coverPosition}, [[font]]={:font},
			[[small_text]]={:smallText}, [[full_width]]={:fullWidth}, [[table_of_contents_display]]={:tableOfContents}
		WHERE [[id]]={:templateId} AND [[database_template]]
	`).Bind(dbx.Params{
		"templateId": templateID, "cover": look.Cover, "coverPosition": look.CoverPosition, "font": look.Font,
		"smallText": look.SmallText, "fullWidth": look.FullWidth, "tableOfContents": look.TableOfContentsDisplay,
	}).Execute(); err != nil {
		return err
	}
	// The cover's file is the template's own now, as a page's cover is; the
	// row's goes with the row.
	return replaceProductFilePageReferences(app, workspaceID, templateID, rows[0].Icon, rows[0].IconType, look.Cover)
}
