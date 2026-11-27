package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's database templates are pages its API lists but whose look it
// doesn't give, and a view's own default template is a setting it doesn't
// give either (seen 2026-11-16: the templates list says is_default only for
// the database's default, "모든 보기에 적용"; a default set for one view,
// "… 보기에만 적용", reads false there and the view API names none). The
// reader reads both in the person's Notion — each template page's full
// width, small text and font, and a view's default template
// (notion_migration_formats_reader.js) — and the run matches them to the
// templates it made, by the migration's ledger (notion_import_templates.go):
// a template's look as its page's "..." menu sets it, a view's default as
// the view's own, which wins over the database's (viewDefaultTemplate.ts).

// template gives the template the run made of a Notion template the look
// the reader read of it; one the run made no template of is left.
func (matcher *notionMigrationFormatsMatcher) template(notionID string, format notionMigrationPageFormat) (bool, error) {
	templateID, err := notionMigrationTemplateNative(matcher.app, matcher.payload.MigrationID, notionID)
	if err != nil || templateID == "" {
		return false, err
	}
	font, smallText, fullWidth := format.Font, format.SmallText, format.FullWidth
	err = setDatabaseRowPageStyle(matcher.app, templateID, matcher.job.WorkspaceID, "", &font, &smallText, &fullWidth)
	return err == nil, ignoreNotionMigrationFormatConflict(err)
}

// viewDefaultTemplate sets a view's own default template to the one the run
// made of the Notion template the view's default is.
func (matcher *notionMigrationFormatsMatcher) viewDefaultTemplate(format notionMigrationViewFormat, config map[string]any) error {
	if format.DefaultTemplate == "" {
		return nil
	}
	templateID, err := notionMigrationTemplateNative(matcher.app, matcher.payload.MigrationID, canonicalNotionID(format.DefaultTemplate))
	if err != nil || templateID == "" {
		return err
	}
	config[viewDefaultTemplateKey] = templateID
	return nil
}

// viewDefaultTemplateKey is where a view keeps a default template of its own
// (the web's viewDefaultTemplate.ts).
const viewDefaultTemplateKey = "defaultTemplateId"

// finishNotionImportTemplate gives a template the run just made of a Notion
// template the row page's look (copyNotionImportTemplateLook) and, in a
// migration, makes it the default of the views whose own default the reader
// read it as: a run makes its templates last (notion_import_row_stamps.go),
// after the step that matched the reading, which found none to name.
func finishNotionImportTemplate(app core.App, job storedJob, payload notionImportJobPayload, rowID, sourceID, templateID string) error {
	if err := copyNotionImportTemplateLook(app, job.WorkspaceID, rowID, templateID); err != nil || payload.MigrationID == "" {
		return err
	}
	// The look the reader read of it holds, as a run again doesn't match a
	// reading it matched before to the row it reads the template as anew.
	var read string
	if err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_notion_migration_formats}}
		WHERE [[migration_id]]={:id} AND [[kind]]='page' AND [[notion_id]]={:notionId}`).
		Bind(dbx.Params{"id": payload.MigrationID, "notionId": canonicalNotionID(sourceID)}).Row(&read); err == nil {
		format := notionMigrationPageFormat{}
		if json.Unmarshal([]byte(read), &format) == nil {
			font, smallText, fullWidth := format.Font, format.SmallText, format.FullWidth
			if err := setDatabaseRowPageStyle(app, templateID, job.WorkspaceID, "", &font, &smallText, &fullWidth); ignoreNotionMigrationFormatConflict(err) != nil {
				return err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	stored := []struct {
		NotionID string `db:"notion_id"`
		Value    string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[notion_id]],[[value]] FROM {{leafen_notion_migration_formats}}
		WHERE [[migration_id]]={:id} AND [[kind]]='view' AND json_valid([[value]])
			AND json_extract([[value]], '$.defaultTemplate') IS NOT NULL`).
		Bind(dbx.Params{"id": payload.MigrationID}).All(&stored); err != nil {
		return err
	}
	matcher := &notionMigrationFormatsMatcher{app: app, job: job, payload: payload, batches: map[string]*structuralMutationBatch{}}
	for _, item := range stored {
		format := notionMigrationViewFormat{}
		if json.Unmarshal([]byte(item.Value), &format) != nil || canonicalNotionID(format.DefaultTemplate) != canonicalNotionID(sourceID) {
			continue
		}
		viewID, err := matcher.mapped(notionImportViewSourceID(format.Database, item.NotionID), "view")
		if err != nil {
			return err
		}
		var pageID string
		if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": viewID}).Row(&pageID); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		view, found, err := loadDatabaseViewRecord(app, pageID, viewID)
		if err != nil || !found {
			return err
		}
		config := maps.Clone(view.Config)
		if config == nil {
			config = map[string]any{}
		}
		config[viewDefaultTemplateKey] = templateID
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		next := &graphDatabaseView{ID: viewID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: encoded}
		if err := matcher.apply(pageID, []graphOperation{{Kind: graphOperationKindUpsertView, View: next}}); err != nil {
			if ignored := ignoreNotionMigrationFormatConflict(err); ignored != nil {
				return ignored
			}
		}
	}
	return saveStructuralMutationBatches(app, job.Owner, matcher.batches)
}
