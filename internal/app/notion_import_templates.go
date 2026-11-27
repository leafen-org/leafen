package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion database template is a page of its data source, so the import reads
// it as a row, its values and its body going through all a row's do, and at the
// end makes the database's template of it: its values and a copy of its body,
// the row going for good. A migration's ledger keeps which template each came
// to, so a run again doesn't make it twice.

const (
	notionImportRowTemplateKey    = "leafen:template"
	notionMigrationLedgerTemplate = "template"
)

type notionImportRowTemplate struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	// Update names the template an earlier run made of it, which takes its
	// values and body again.
	Update string `json:"update,omitempty"`
}

// notionImportTemplateSources stages the templates of the database being read.
// One Notion won't give is passed by, as a database's views are.
func notionImportTemplateSources(
	ctx context.Context, app core.App, client notionProviderClient, jobID string, payload *notionImportJobPayload,
) ([]stagedNotionSource, []string, error) {
	databaseID := payload.ActiveDatabaseID
	listed := struct {
		DataSources []notionProviderOtherDataSource `json:"data_sources"`
	}{}
	err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &listed,
	)
	if notionProviderRefused(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	sources, fileIDs := []stagedNotionSource{}, []string{}
	position := payload.DatabaseRowPosition
	for index, dataSource := range listed.DataSources {
		key := databaseID
		if index > 0 {
			key = notionImportDataSourceKey(databaseID, dataSource.ID)
		}
		templates, err := client.dataSourceTemplates(ctx, dataSource.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, template := range templates {
			// One made by a migration's earlier run stays as it is, unless
			// Notion changed it since: then it takes Notion's values and body
			// again, as a row does, in the trash too (decision 3절 원칙 4) —
			// unless a person deleted it for good (notionMigrationTemplateGone).
			made, err := notionMigrationTemplateNative(app, payload.MigrationID, template.ID)
			if err != nil {
				return nil, nil, err
			}
			page, err := client.page(ctx, template.ID)
			if notionProviderRefused(err) {
				continue
			}
			if err != nil {
				return nil, nil, err
			}
			// What the run counts of it is what Notion still gives
			// (notion_import_counted.go).
			if made != "" {
				if err := markNotionImportTemplateGiven(app, jobID, payload.MigrationID, template.ID); err != nil {
					return nil, nil, err
				}
			}
			if made != "" && !notionMigrationChangedSince(*payload, page.LastEditedTime) {
				continue
			}
			if made != "" {
				if gone, err := notionMigrationTemplateGone(app, made); err != nil || gone {
					if err != nil {
						return nil, nil, err
					}
					continue
				}
			}
			results := []notionProviderObject{page}
			if err := completeNotionRowProperties(ctx, client, results); err != nil {
				return nil, nil, err
			}
			staged, files, err := notionDatabaseRowSources(app, key, results, position, nil)
			if err != nil {
				return nil, nil, err
			}
			position++
			for sourceIndex := range staged {
				if staged[sourceIndex].SourceID != page.ID || staged[sourceIndex].Kind != "row" {
					continue
				}
				values := map[string]notionDatabaseRowValuePlan{}
				if err := json.Unmarshal([]byte(staged[sourceIndex].Text), &values); err != nil {
					return nil, nil, err
				}
				values[notionImportRowTemplateKey] = notionDatabaseRowValuePlan{
					Type: "template", Value: notionImportRowTemplate{Name: template.Name, IsDefault: template.IsDefault, Update: made},
				}
				encoded, err := json.Marshal(values)
				if err != nil {
					return nil, nil, err
				}
				staged[sourceIndex].Text = string(encoded)
			}
			sources = append(sources, staged...)
			fileIDs = append(fileIDs, files...)
		}
	}
	return sources, fileIDs, nil
}

type notionProviderTemplate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// dataSourceTemplates lists a data source's templates; none when Notion won't.
func (client notionProviderClient) dataSourceTemplates(ctx context.Context, dataSourceID string) ([]notionProviderTemplate, error) {
	templates, cursor := []notionProviderTemplate{}, ""
	for {
		query := url.Values{"page_size": []string{"100"}}
		if cursor != "" {
			query.Set("start_cursor", cursor)
		}
		page := struct {
			Templates  []notionProviderTemplate `json:"templates"`
			HasMore    bool                     `json:"has_more"`
			NextCursor *string                  `json:"next_cursor"`
		}{}
		err := client.requestVersion(
			ctx, notionProviderDataSourceAPIVersion, http.MethodGet,
			"/v1/data_sources/"+url.PathEscape(dataSourceID)+"/templates?"+query.Encode(), nil, &page,
		)
		if notionProviderRefused(err) {
			return templates, nil
		}
		if err != nil {
			return nil, err
		}
		templates = append(templates, page.Templates...)
		if !page.HasMore || page.NextCursor == nil || *page.NextCursor == "" || *page.NextCursor == cursor {
			return templates, nil
		}
		cursor = *page.NextCursor
	}
}

// notionImportRowTemplateOf reads whether a staged row stands for a template.
func notionImportRowTemplateOf(text string) (notionImportRowTemplate, bool) {
	values := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(text), &values) != nil {
		return notionImportRowTemplate{}, false
	}
	raw, found := values[notionImportRowTemplateKey]
	if !found {
		return notionImportRowTemplate{}, false
	}
	plan := struct {
		Value notionImportRowTemplate `json:"value"`
	}{}
	return plan.Value, json.Unmarshal(raw, &plan) == nil
}

// notionImportTemplateSkippedValueTypes hold nothing a template can give a new page.
var notionImportTemplateSkippedValueTypes = map[string]bool{
	graphValueTypeTitle: true, graphValueTypeUniqueID: true, "created_time": true, "created_by": true,
	"last_edited_time": true, "last_edited_by": true, "formula": true, "rollup": true,
}

// makeNotionImportTemplate makes the database's template of a row that stood
// for one, and takes the row away; errNotionImportPassedBy when a person took
// the row or its database away meanwhile. It says whether the migration's
// report named the row, which now names its template.
func makeNotionImportTemplate(
	app core.App, job storedJob, payload notionImportJobPayload, sourceID, rowID string, template notionImportRowTemplate,
) (bool, error) {
	authority := databaseToolbarConfigAuthority{}
	position := ""
	row := []struct {
		Owner        string `db:"owner"`
		WorkspaceID  string `db:"workspace_id"`
		PageID       string `db:"page_id"`
		DataSourceID string `db:"data_source_id"`
		Position     string `db:"position"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[position]]
		FROM {{leafen_database_rows}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": rowID}).All(&row); err != nil {
		return false, err
	}
	// One a person deleted for good, or whose database they moved to the
	// trash, while the run went on is passed by (notion_import_changed_meanwhile.go).
	if len(row) != 1 {
		return false, errNotionImportPassedBy
	}
	if away, err := notionImportPageTakenAway(app, row[0].PageID); err != nil || away {
		return false, cmp.Or(err, errNotionImportPassedBy)
	}
	authority = databaseToolbarConfigAuthority{
		OwnerID: row[0].Owner, WorkspaceID: row[0].WorkspaceID, PageID: row[0].PageID, DataSourceID: row[0].DataSourceID,
	}
	position = row[0].Position
	templateID := template.Update
	if template.Update != "" {
		if err := updateNotionImportTemplate(app, job, payload, authority, rowID, template); err != nil {
			return false, err
		}
	} else if created, err := createNotionImportTemplate(app, job, authority, rowID, position, template, payload, sourceID); err != nil {
		return false, err
	} else {
		templateID = created
	}
	if err := finishNotionImportTemplate(app, job, payload, rowID, sourceID, templateID); err != nil {
		return false, err
	}
	if err := clearNotionImportTemplateRowBody(app, job, payload, rowID, templateID); err != nil {
		return false, err
	}
	// The databases of its body are copied into the template after the rest
	// (database_template_body_databases.go), from the row: it goes once they
	// are, else now; the import ends after them (notion_import_template_copies_wait.go).
	deferred, err := deferRowPageRemovalToDatabaseCopies(app, job.Owner, job.WorkspaceID, templateID, rowID, payload.AuthorityOwnerID, job.ID)
	if err != nil {
		return false, err
	}
	if !deferred {
		if err := removeNotionImportTemplateRow(app, job.Owner, payload.AuthorityOwnerID, job.WorkspaceID, rowID); err != nil {
			return false, err
		}
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_import_mappings}} WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}
	`).Bind(dbx.Params{"jobId": job.ID, "sourceId": sourceID}).Execute(); err != nil {
		return false, err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_reports}} SET [[rows]]=MAX([[rows]]-1, 0) WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).Execute(); err != nil {
		return false, err
	}
	// What the migration's report named on the row is on the template now.
	return repointNotionMigrationIssuesToTemplate(app, payload.MigrationID, rowID, templateID)
}

// deleteNotionImportPageLinkedDatabases takes a page's databases away with
// their views.
func deleteNotionImportPageLinkedDatabases(app core.App, job storedJob, payload notionImportJobPayload, rowID string) error {
	return deleteNotionImportPageDatabases(app, payload.AuthorityOwnerID, job.WorkspaceID, job.Owner, rowID)
}

func deleteNotionImportPageDatabases(app core.App, authorityOwnerID, workspaceID, actorID, rowID string) error {
	views, linked := []string{}, []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_views}} WHERE [[page_id]]={:rowId}`).
		Bind(dbx.Params{"rowId": rowID}).Column(&views); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:rowId} AND [[type]]={:type}
	`).Bind(dbx.Params{"rowId": rowID, "type": graphBlockTypeInlineDatabase}).Column(&linked); err != nil {
		return err
	}
	if len(views)+len(linked) == 0 {
		return nil
	}
	operations := make([]graphOperation, 0, len(views)+len(linked))
	for _, id := range views {
		operations = append(operations, graphOperation{Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: id}})
	}
	for _, id := range linked {
		operations = append(operations, graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: id}})
	}
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, rowID, batches)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperations(app, batch, authorityOwnerID, workspaceID, rowID, actorID, operations); err != nil {
		return err
	}
	return saveStructuralMutationBatches(app, actorID, batches)
}

// updateNotionImportTemplate gives the template an earlier run made the
// row's values and body as Notion has them now. One a person moved to the
// trash takes them there, as a row in the trash does, and has them once
// restored (2026-11-17: a template deleted in Leafen kept its old body after
// Notion changed it, restored or not); one deleted for good stays gone
// (notionMigrationTemplateGone).
func updateNotionImportTemplate(
	app core.App, job storedJob, payload notionImportJobPayload, authority databaseToolbarConfigAuthority, rowID string,
	template notionImportRowTemplate,
) error {
	existing, err := databaseToolbarTemplateForMutation(app, authority, template.Update)
	if errors.Is(err, errDatabaseToolbarConfigNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	title, properties, spans, err := notionImportTemplateValues(app, rowID)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(template.Name)
	if name == "" {
		name = strings.TrimSpace(title)
	}
	icon, err := notionImportTemplateIcon(app, job.WorkspaceID, rowID)
	if err != nil {
		return err
	}
	revision := existing.AuthorityRevision
	state := databaseToolbarMutationState{ActorID: job.Owner, InTrash: existing.DeletedAt != ""}
	if err := updateDatabaseToolbarTemplate(app, authority, databaseToolbarConfigMutationOperation{
		TemplateID: &template.Update, ExpectedRevision: &revision, Name: &name, Icon: &icon, Title: &title,
		Properties: properties, PropertySpans: spans, IsDefault: &template.IsDefault,
	}, &state); err != nil {
		return err
	}
	// Its body is the row's now, linked databases and their views with it;
	// the own databases the move made in it go for good.
	if err := replaceNotionImportTemplateDatabases(app, job, payload, template.Update); err != nil {
		return err
	}
	if err := replaceDatabaseTemplatePageBody(app, template.Update, job.WorkspaceID, job.Owner, json.RawMessage(`[]`)); err != nil {
		return err
	}
	if err := duplicateDatabaseRowPageBodyFrom(app, rowID, job.WorkspaceID, template.Update, job.Owner); err != nil {
		return err
	}
	// The sub-pages it made before, which the run passes by, keep their
	// blocks (notion_import_template_sub_pages.go).
	return requestChildPageBlockBackfill(app, job.WorkspaceID, template.Update)
}

// notionImportTemplateValues reads a template row's title and the values and
// spans its template keeps.
func notionImportTemplateValues(app core.App, rowID string) (string, json.RawMessage, json.RawMessage, error) {
	values := []struct {
		PropertyID string `db:"property_id"`
		Type       string `db:"type"`
		Value      string `db:"value"`
		Spans      string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property_id]],[[type]],[[value]],[[spans]] FROM {{leafen_database_row_values}} WHERE [[row_id]]={:id}
	`).Bind(dbx.Params{"id": rowID}).All(&values); err != nil {
		return "", nil, nil, err
	}
	title := ""
	properties, spans := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	for _, value := range values {
		if value.Type == graphValueTypeTitle {
			_ = json.Unmarshal([]byte(value.Value), &title)
		}
		if notionImportTemplateSkippedValueTypes[value.Type] || value.Value == `""` || value.Value == "null" || value.Value == "[]" {
			continue
		}
		properties[value.PropertyID] = json.RawMessage(value.Value)
		if strings.TrimSpace(value.Spans) != "" {
			spans[value.PropertyID] = json.RawMessage(value.Spans)
		}
	}
	encodedProperties, err := json.Marshal(properties)
	if err != nil {
		return "", nil, nil, err
	}
	encodedSpans, err := json.Marshal(spans)
	return title, encodedProperties, encodedSpans, err
}

func createNotionImportTemplate(
	app core.App, job storedJob, authority databaseToolbarConfigAuthority, rowID, position string,
	template notionImportRowTemplate, payload notionImportJobPayload, sourceID string,
) (string, error) {
	title, encodedProperties, encodedSpans, err := notionImportTemplateValues(app, rowID)
	if err != nil {
		return "", err
	}
	templateID := uuid.NewString()
	name := strings.TrimSpace(template.Name)
	if name == "" {
		name = strings.TrimSpace(title)
	}
	icon, err := notionImportTemplateIcon(app, job.WorkspaceID, rowID)
	if err != nil {
		return "", err
	}
	state := databaseToolbarMutationState{ActorID: job.Owner}
	if err := createDatabaseToolbarTemplate(app, authority, databaseToolbarConfigMutationOperation{
		TemplateID: &templateID, Name: &name, Icon: &icon, Title: &title, Position: &position,
		Properties: encodedProperties, PropertySpans: encodedSpans, IsDefault: &template.IsDefault,
	}, &state); err != nil {
		return "", err
	}
	if err := duplicateDatabaseRowPageBodyFrom(app, rowID, job.WorkspaceID, templateID, job.Owner); err != nil {
		return "", err
	}
	if payload.MigrationID == "" {
		return templateID, nil
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_ledger}}
			([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
		VALUES ({:migrationId},{:workspaceId},{:sourceId},{:kind},{:nativeId},'','')
		ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO NOTHING
	`).Bind(dbx.Params{
		"migrationId": payload.MigrationID, "workspaceId": job.WorkspaceID, "sourceId": sourceID,
		"kind": notionMigrationLedgerTemplate, "nativeId": templateID,
	}).Execute()
	return templateID, err
}

// notionImportTemplateIcon is the icon a template made from an imported row
// takes: the row page's, in the form a template keeps one.
func notionImportTemplateIcon(app core.App, workspaceID, rowID string) (string, error) {
	page, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil {
		return "", nil
	}
	return databaseTemplateIconOfPage(app, workspaceID, page.GetString("icon"), page.GetString("icon_type"))
}
