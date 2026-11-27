package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func ensureDatabaseToolbarTemplateState(
	app core.App,
	authority databaseToolbarConfigAuthority,
	templateID string,
) error {
	_, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_database_toolbar_template_state}}
		([[template_id]], [[workspace_id]], [[authority_revision]], [[deleted_at]])
		VALUES ({:templateId}, {:workspaceId}, 1, '')`).Bind(dbx.Params{
		"templateId": templateID, "workspaceId": authority.WorkspaceID,
	}).Execute()
	return err
}

func databaseToolbarTemplateForMutation(
	app core.App,
	authority databaseToolbarConfigAuthority,
	templateID string,
) (storedDatabaseToolbarTemplate, error) {
	template := storedDatabaseToolbarTemplate{}
	err := app.DB().NewQuery(`SELECT [[template]].[[id]], [[template]].[[name]], [[template]].[[icon]],
		[[template]].[[title]], [[template]].[[properties_json]], [[template]].[[property_spans_json]],
		[[template]].[[is_default]], [[template]].[[position]],
		COALESCE([[state]].[[authority_revision]],1) AS [[authority_revision]],
		COALESCE([[state]].[[deleted_at]],'') AS [[deleted_at]], [[template]].[[updated_at]]
		FROM {{leafen_database_templates}} [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]]
			ON [[state]].[[template_id]]=[[template]].[[id]]
		WHERE [[template]].[[id]]={:templateId} AND [[template]].[[owner]]={:owner}
			AND [[template]].[[workspace_id]]={:workspaceId} AND [[template]].[[page_id]]={:pageId}
			AND [[template]].[[data_source_id]]={:dataSourceId}`).Bind(dbx.Params{
		"templateId": templateID, "owner": authority.OwnerID,
		"workspaceId": authority.WorkspaceID, "pageId": authority.PageID,
		"dataSourceId": authority.DataSourceID,
	}).One(&template)
	if errors.Is(err, sql.ErrNoRows) {
		return template, errDatabaseToolbarConfigNotFound
	}
	return template, err
}

func clearDatabaseToolbarDefaultTemplates(
	app core.App,
	authority databaseToolbarConfigAuthority,
	exceptID, now string,
) error {
	if _, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_database_toolbar_template_state}}
		([[template_id]], [[workspace_id]], [[authority_revision]], [[deleted_at]])
		SELECT [[id]], [[workspace_id]], 1, '' FROM {{leafen_database_templates}}
		WHERE [[data_source_id]]={:dataSourceId} AND [[is_default]]=1 AND [[id]]!={:exceptId}`).
		Bind(dbx.Params{"dataSourceId": authority.DataSourceID, "exceptId": exceptID}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[authority_revision]]=[[authority_revision]]+1
		WHERE [[template_id]] IN (SELECT [[id]] FROM {{leafen_database_templates}}
			WHERE [[data_source_id]]={:dataSourceId} AND [[is_default]]=1 AND [[id]]!={:exceptId})`).
		Bind(dbx.Params{"dataSourceId": authority.DataSourceID, "exceptId": exceptID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`UPDATE {{leafen_database_templates}}
		SET [[is_default]]=0, [[updated_at]]={:now}
		WHERE [[data_source_id]]={:dataSourceId} AND [[is_default]]=1 AND [[id]]!={:exceptId}`).
		Bind(dbx.Params{"dataSourceId": authority.DataSourceID, "exceptId": exceptID, "now": now}).Execute()
	return err
}

func createDatabaseToolbarTemplate(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	// A template is a page of its data source, so its ID may name no other
	// template or page.
	var existing int
	if err := app.DB().NewQuery(`SELECT
		(SELECT COUNT(*) FROM {{leafen_database_templates}} WHERE [[id]]={:id})
		+ (SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]]={:id})`).
		Bind(dbx.Params{"id": *operation.TemplateID}).Row(&existing); err != nil {
		return err
	}
	if existing != 0 {
		return databaseToolbarStateRefused("templateId names a template or page that is there already: a new template takes a new id")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	icon, title := "", ""
	if operation.Icon != nil {
		// An uploaded icon is a picture (look_picture_files.go).
		if err := checkGivenLookFiles(app, authority.WorkspaceID, givenLookFile{Field: "icon", Value: *operation.Icon, File: true}); err != nil {
			return err
		}
		icon = databaseTemplateIconValue(*operation.Icon)
	}
	if operation.Title != nil {
		title = *operation.Title
	}
	properties := "{}"
	if len(operation.Properties) > 0 {
		properties = string(operation.Properties)
	}
	properties, givenSpans, err := keepDatabaseToolbarTemplateValues(app, authority, properties, operation.PropertySpans)
	if err != nil {
		return err
	}
	textProperties, err := databaseTemplateTextProperties(app, authority)
	if err != nil {
		return err
	}
	spans, err := databaseTemplateTextSpans(textProperties, properties, givenSpans, true)
	if err != nil {
		return err
	}
	isDefault := operation.IsDefault != nil && *operation.IsDefault
	if isDefault {
		if err := clearDatabaseToolbarDefaultTemplates(app, authority, *operation.TemplateID, now); err != nil {
			return err
		}
	}
	if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_templates}}
		([[id]], [[workspace_id]], [[owner]], [[page_id]], [[data_source_id]], [[name]], [[icon]],
		 [[title]], [[properties_json]], [[property_spans_json]], [[blocks_json]], [[is_default]], [[position]],
		 [[workspace_import_job_id]], [[created_at]], [[updated_at]])
		VALUES ({:id}, {:workspaceId}, {:owner}, {:pageId}, {:dataSourceId}, {:name}, {:icon},
		 {:title}, {:properties}, {:spans}, '[]', {:isDefault}, {:position}, '', {:now}, {:now})`).
		Bind(dbx.Params{"id": *operation.TemplateID, "workspaceId": authority.WorkspaceID,
			"owner": authority.OwnerID, "pageId": authority.PageID, "dataSourceId": authority.DataSourceID,
			"name": *operation.Name, "icon": icon, "title": title, "properties": properties, "spans": spans,
			"isDefault": isDefault, "position": *operation.Position, "now": now}).Execute(); err != nil {
		return err
	}
	if err := ensureDatabaseToolbarTemplateState(app, authority, *operation.TemplateID); err != nil {
		return err
	}
	if state.ActorID != "" {
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[created_by]]={:actorId}, [[last_edited_by]]={:actorId}
			WHERE [[id]]={:id} AND [[database_template]]`).
			Bind(dbx.Params{"actorId": state.ActorID, "id": *operation.TemplateID}).Execute(); err != nil {
			return err
		}
		if err := followCreatedRowComments(app, authority.WorkspaceID, *operation.TemplateID, state.ActorID, time.Now().UTC()); err != nil {
			return err
		}
	}
	if len(operation.Blocks) > 0 {
		if err := replaceDatabaseTemplatePageBody(
			app, *operation.TemplateID, authority.WorkspaceID, state.ActorID, operation.Blocks,
		); err != nil {
			return err
		}
	}
	state.TemplateAuthorityRevision = 1
	return nil
}

func updateDatabaseToolbarTemplate(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	template, err := databaseToolbarTemplateForMutation(app, authority, *operation.TemplateID)
	if err != nil {
		return err
	}
	if template.DeletedAt != "" && !state.InTrash {
		return errDatabaseToolbarTemplateTrashed
	}
	if template.AuthorityRevision != *operation.ExpectedRevision {
		return errDatabaseToolbarConfigConflict
	}
	if err := ensureDatabaseToolbarTemplateState(app, authority, template.ID); err != nil {
		return err
	}
	name, icon, title, position := template.Name, template.Icon, template.Title, template.Position
	properties := template.Properties
	if operation.Name != nil {
		name = *operation.Name
	}
	if operation.Icon != nil {
		if err := checkGivenLookFiles(app, authority.WorkspaceID, givenLookFile{Field: "icon", Value: *operation.Icon, Stored: template.Icon, File: true}); err != nil {
			return err
		}
		icon = databaseTemplateIconValue(*operation.Icon)
	}
	if operation.Title != nil {
		title = *operation.Title
	}
	if operation.Position != nil {
		position = *operation.Position
	}
	if len(operation.Properties) > 0 {
		properties = string(operation.Properties)
	}
	// Spans a change gives must spell their values; the ones kept from
	// before stay only while they still do.
	spans, strict := []byte(template.PropertySpans), false
	if len(operation.PropertySpans) > 0 {
		spans, strict = operation.PropertySpans, true
	}
	properties, spans, err = keepDatabaseToolbarTemplateValues(app, authority, properties, spans)
	if err != nil {
		return err
	}
	textProperties, err := databaseTemplateTextProperties(app, authority)
	if err != nil {
		return err
	}
	keptSpans, err := databaseTemplateTextSpans(textProperties, properties, spans, strict)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = app.DB().NewQuery(`UPDATE {{leafen_database_templates}} SET [[name]]={:name},
		[[icon]]={:icon}, [[title]]={:title}, [[properties_json]]={:properties},
		[[property_spans_json]]={:spans}, [[position]]={:position}, [[updated_at]]={:now}
		WHERE [[id]]={:id}`).Bind(dbx.Params{
		"name": name, "icon": icon, "title": title, "properties": properties, "spans": keptSpans,
		"position": position, "now": now, "id": template.ID,
	}).Execute(); err != nil {
		return err
	}
	next := template.AuthorityRevision + 1
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[authority_revision]]={:next} WHERE [[template_id]]={:id} AND [[authority_revision]]={:current}`).
		Bind(dbx.Params{"next": next, "id": template.ID, "current": template.AuthorityRevision}).Execute()
	if err := requireDatabaseToolbarMutationApplied(result, err); err != nil {
		return err
	}
	state.TemplateAuthorityRevision = next
	return nil
}

func duplicateDatabaseToolbarTemplate(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	source, err := databaseToolbarTemplateForMutation(app, authority, *operation.TemplateID)
	if err != nil {
		return err
	}
	if source.DeletedAt != "" {
		return errDatabaseToolbarTemplateTrashed
	}
	if source.AuthorityRevision != *operation.ExpectedRevision {
		return errDatabaseToolbarConfigConflict
	}
	// The copy is named as a page's copy is — "Rpt" makes "Rpt (1)" and
	// "Rpt (2)" makes "Rpt (3)" — and comes right after it, as Notion's (seen
	// 2026-11-04); its shown name is its title, else its name.
	name, title := source.Name, source.Title
	if title != "" {
		title, _ = duplicateCopyTitle(title)
	} else {
		name, _ = duplicateCopyTitle(name)
	}
	if operation.Name != nil {
		name = *operation.Name
	}
	next := ""
	// The templates are found by their source; the owner is read after (+),
	// or SQLite, which keeps no statistics, reads the owner's every template.
	if err := app.DB().NewQuery(`SELECT COALESCE(MIN([[position]]), '') FROM {{leafen_database_templates}}
		WHERE +[[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}
			AND [[position]]>{:position}`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.PageID, "dataSourceId": authority.DataSourceID,
		"position": source.Position,
	}).Row(&next); err != nil {
		return err
	}
	position, err := databasePositionBetween(source.Position, next)
	if err != nil {
		return err
	}
	copyOperation := databaseToolbarConfigMutationOperation{
		Kind: "createTemplate", TemplateID: operation.TargetTemplateID,
		Name: &name, Icon: &source.Icon, Title: &title, Position: &position,
		Properties: json.RawMessage(source.Properties), PropertySpans: json.RawMessage(source.PropertySpans),
	}
	if err := createDatabaseToolbarTemplate(app, authority, copyOperation, state); err != nil {
		return err
	}
	if err := copyDatabaseToolbarTemplateRepeat(app, authority, source.ID, *operation.TargetTemplateID, state.ActorID); err != nil {
		return err
	}
	// The copy's page takes the source page's body under new IDs, its own
	// databases as copies, each synced block original as a copy of it, as
	// Notion's app duplicates a template (seen 2026-11-14).
	return duplicateDatabaseTemplateBodyText(
		app, source.ID, *operation.TargetTemplateID, authority.WorkspaceID, state.ActorID, nil, rowPageBodySyncedAsCopies,
	)
}

// copyDatabaseToolbarTemplateRepeat gives a template's copy the same repeat,
// as Notion's (seen 2026-11-05: "Rp"'s weekly repeat on "Rp (1)"), its pages
// counted anew and its own from whoever copied it. One that has ended stays
// with the source.
func copyDatabaseToolbarTemplateRepeat(
	app core.App, authority databaseToolbarConfigAuthority, sourceID, copyID, actorID string,
) error {
	repeatJSON := ""
	err := app.DB().NewQuery(`SELECT [[repeat_json]] FROM {{leafen_database_toolbar_template_state}}
		WHERE [[template_id]]={:id}`).Bind(dbx.Params{"id": sourceID}).Row(&repeatJSON)
	if errors.Is(err, sql.ErrNoRows) || err == nil && repeatJSON == "" {
		return nil
	}
	if err != nil {
		return err
	}
	nextRunAt := databaseTemplateRepeatNextRunAt(repeatJSON, 0, time.Now().UTC())
	if nextRunAt == 0 {
		return nil
	}
	if err := ensureDatabaseToolbarTemplateState(app, authority, copyID); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[repeat_json]]={:repeat}, [[repeat_actor_id]]={:actor}, [[repeat_next_run_at]]={:nextRunAt},
			[[repeat_paused_reason]]='', [[repeat_runs]]=0
		WHERE [[template_id]]={:id}`).Bind(dbx.Params{
		"repeat": repeatJSON, "actor": actorID, "nextRunAt": nextRunAt, "id": copyID,
	}).Execute()
	return err
}

func setDatabaseToolbarTemplateDeleted(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	deleted bool,
	state *databaseToolbarMutationState,
) error {
	template, err := databaseToolbarTemplateForMutation(app, authority, *operation.TemplateID)
	if err != nil {
		return err
	}
	if (template.DeletedAt != "") == deleted && deleted {
		return databaseToolbarStateRefused("templateId names a template in the trash already")
	}
	if (template.DeletedAt != "") == deleted {
		return databaseToolbarStateRefused("templateId names a template that isn't in the trash")
	}
	if template.AuthorityRevision != *operation.ExpectedRevision {
		return errDatabaseToolbarConfigConflict
	}
	if err := ensureDatabaseToolbarTemplateState(app, authority, template.ID); err != nil {
		return err
	}
	now, deletedAt := time.Now().UTC().Format(time.RFC3339Nano), ""
	if deleted {
		deletedAt = now
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_templates}}
		SET [[is_default]]=CASE WHEN {:deleted} THEN 0 ELSE [[is_default]] END, [[updated_at]]={:now}
		WHERE [[id]]={:id}`).Bind(dbx.Params{"deleted": deleted, "now": now, "id": template.ID}).Execute(); err != nil {
		return err
	}
	next, deletedBy := template.AuthorityRevision+1, ""
	if deleted {
		deletedBy = state.ActorID
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[deleted_at]]={:deletedAt}, [[deleted_by]]={:deletedBy}, [[authority_revision]]={:next}
		WHERE [[template_id]]={:id} AND [[authority_revision]]={:current}`).Bind(dbx.Params{
		"deletedAt": deletedAt, "deletedBy": deletedBy, "next": next, "id": template.ID, "current": template.AuthorityRevision,
	}).Execute()
	if err := requireDatabaseToolbarMutationApplied(result, err); err != nil {
		return err
	}
	state.TemplateAuthorityRevision = next
	return settleDatabaseTemplateRepeat(app, template.ID, deleted)
}

func setDatabaseToolbarDefaultTemplate(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	template, err := databaseToolbarTemplateForMutation(app, authority, *operation.TemplateID)
	if err != nil {
		return err
	}
	if template.DeletedAt != "" {
		return errDatabaseToolbarTemplateTrashed
	}
	if template.AuthorityRevision != *operation.ExpectedRevision {
		return errDatabaseToolbarConfigConflict
	}
	if err := ensureDatabaseToolbarTemplateState(app, authority, template.ID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	enabled := operation.IsDefault == nil || *operation.IsDefault
	if enabled {
		if err := clearDatabaseToolbarDefaultTemplates(app, authority, template.ID, now); err != nil {
			return err
		}
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_templates}}
		SET [[is_default]]={:enabled}, [[updated_at]]={:now} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"enabled": enabled, "now": now, "id": template.ID}).Execute(); err != nil {
		return err
	}
	next := template.AuthorityRevision + 1
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[authority_revision]]={:next}
		WHERE [[template_id]]={:id} AND [[authority_revision]]={:current}`).Bind(dbx.Params{
		"next": next, "id": template.ID, "current": template.AuthorityRevision,
	}).Execute()
	if err := requireDatabaseToolbarMutationApplied(result, err); err != nil {
		return err
	}
	state.TemplateAuthorityRevision = next
	return nil
}

func databaseToolbarMaxTemplateRevision(
	app core.App,
	authority databaseToolbarConfigAuthority,
) (int, error) {
	var revision int
	err := app.DB().NewQuery(`SELECT COALESCE(MAX(COALESCE([[state]].[[authority_revision]],1)),0)
		FROM {{leafen_database_templates}} [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]]
			ON [[state]].[[template_id]]=[[template]].[[id]]
		WHERE [[template]].[[owner]]={:owner} AND [[template]].[[workspace_id]]={:workspaceId}
			AND [[template]].[[page_id]]={:pageId} AND [[template]].[[data_source_id]]={:dataSourceId}`).
		Bind(dbx.Params{"owner": authority.OwnerID, "workspaceId": authority.WorkspaceID,
			"pageId": authority.PageID, "dataSourceId": authority.DataSourceID}).Row(&revision)
	return revision, err
}
