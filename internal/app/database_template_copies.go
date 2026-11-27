package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page duplicate or a database block copy takes the databases' templates
// with them, as they belong to the database's setup: each live template of a
// source the copy made, on the copy's page, with its values moved onto the
// copy's properties and its page's body copied. A copy takes templates even
// without content, since templates are not the database's items.

// databaseTemplateCopyID names a template's copy within one copy job, so a
// job that runs its step again finds the copy it made; a template is a page,
// so its copy has a page's ID (derivedPageID).
func databaseTemplateCopyID(jobID, templateID string) string {
	return derivedPageID("database-template-copy", jobID, templateID)
}

// copyDatabaseTemplates copies the live templates of the sources a copy job
// made from the source page, in the source owner's workspace, onto the target
// page.
func copyDatabaseTemplates(
	app core.App, jobID, sourceOwnerID, sourceWorkspaceID, ownerID, workspaceID, actorID, sourcePageID, targetPageID string,
) error {
	templates := []struct {
		ID                 string `db:"id"`
		TargetDataSourceID string `db:"target_data_source_id"`
		Name               string `db:"name"`
		Icon               string `db:"icon"`
		Title              string `db:"title"`
		Properties         string `db:"properties_json"`
		PropertySpans      string `db:"property_spans_json"`
		IsDefault          bool   `db:"is_default"`
		Position           string `db:"position"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[template]].[[id]], [[source]].[[target_id]] AS [[target_data_source_id]],
			[[template]].[[name]], [[template]].[[icon]], [[template]].[[title]],
			[[template]].[[properties_json]], [[template]].[[property_spans_json]],
			[[template]].[[is_default]], [[template]].[[position]]
		FROM {{leafen_database_templates}} [[template]]
		JOIN {{leafen_page_duplication_identity_map}} [[source]]
			ON [[source]].[[job_id]] = {:jobId} AND [[source]].[[kind]] = 'data_source'
			AND [[source]].[[source_id]] = [[template]].[[data_source_id]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[owner]] = {:ownerId} AND [[template]].[[workspace_id]] = {:workspaceId}
			AND [[template]].[[page_id]] = {:sourcePageId} AND COALESCE([[state]].[[deleted_at]], '') = ''
		ORDER BY [[template]].[[position]], [[template]].[[id]]
	`).Bind(dbx.Params{
		"jobId": jobID, "ownerId": sourceOwnerID, "workspaceId": sourceWorkspaceID, "sourcePageId": sourcePageID,
	}).All(&templates); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, template := range templates {
		copyID := databaseTemplateCopyID(jobID, template.ID)
		var copied int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": copyID}).Row(&copied); err != nil {
			return err
		}
		if copied != 0 {
			continue
		}
		properties, err := remapDatabaseTemplateCopyProperties(app, jobID, template.Properties)
		if err != nil {
			return err
		}
		spans, err := remapDatabaseTemplateCopySpans(app, jobID, template.PropertySpans)
		if err != nil {
			return err
		}
		// A workspace emoji icon names the one a copy into another workspace
		// brings (page_duplication_custom_emoji.go).
		if template.Icon, err = pageDuplicationTemplateCustomEmojiIcon(app, jobID, template.Icon); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_templates}}
				([[id]], [[workspace_id]], [[owner]], [[page_id]], [[data_source_id]], [[name]], [[icon]],
				 [[title]], [[properties_json]], [[property_spans_json]], [[blocks_json]], [[is_default]], [[position]],
				 [[workspace_import_job_id]], [[created_at]], [[updated_at]])
			VALUES ({:id}, {:workspaceId}, {:ownerId}, {:targetPageId}, {:dataSourceId}, {:name}, {:icon},
				{:title}, {:properties}, {:spans}, '[]', {:isDefault}, {:position}, '', {:now}, {:now})
		`).Bind(dbx.Params{
			"id": copyID, "workspaceId": workspaceID, "ownerId": ownerID, "targetPageId": targetPageID,
			"dataSourceId": template.TargetDataSourceID, "name": template.Name, "icon": template.Icon,
			"title": template.Title, "properties": properties, "spans": spans, "isDefault": template.IsDefault,
			"position": template.Position, "now": now,
		}).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_database_toolbar_template_state}}
				([[template_id]], [[workspace_id]], [[authority_revision]], [[deleted_at]])
			VALUES ({:id}, {:workspaceId}, 1, '')
		`).Bind(dbx.Params{"id": copyID, "workspaceId": workspaceID}).Execute(); err != nil {
			return err
		}
		if actorID != "" {
			if _, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[created_by]] = {:actorId}, [[last_edited_by]] = {:actorId}
				WHERE [[id]] = {:id} AND [[database_template]]`).
				Bind(dbx.Params{"actorId": actorID, "id": copyID}).Execute(); err != nil {
				return err
			}
		}
		// Its synced copies of originals the copy took show their copies, as
		// Notion's (seen 2026-11-14), and what else it names that the copy
		// took names the copy, as a row's body does (pageDuplicationBodyRewrite).
		if err := duplicateDatabaseRowPageBodyFrom(
			app, template.ID, sourceWorkspaceID, copyID, actorID, pageDuplicationBodyCustomEmoji(app, jobID),
			pointRowPageBodySyncedCopies(func(contents []map[string]any) (map[string]copiedSyncedBlock, error) {
				return copiedSyncedOriginals(app, jobID, contents)
			}),
			pageDuplicationBodyRewrite(app, storedPageDuplicationJob{
				ID: jobID, OwnerID: ownerID, WorkspaceID: workspaceID,
				SourceOwnerID: sourceOwnerID, SourceWorkspaceID: sourceWorkspaceID,
			}),
		); err != nil {
			return err
		}
	}
	return nil
}

// remapDatabaseTemplateCopySpans moves a template's text formatting onto the
// copy's properties, as its values move: a property the copy lacks leaves
// its formatting out.
func remapDatabaseTemplateCopySpans(app core.App, jobID, raw string) (string, error) {
	spans := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &spans); err != nil || len(spans) == 0 {
		return "{}", nil
	}
	propertyIDs := make([]string, 0, len(spans))
	for propertyID := range spans {
		propertyIDs = append(propertyIDs, propertyID)
	}
	properties, err := pageDuplicationReferenceMappings(app, jobID, "property", propertyIDs)
	if err != nil {
		return "", err
	}
	moved := make(map[string]json.RawMessage, len(spans))
	for propertyID, value := range spans {
		target := properties[propertyID]
		if target == "" {
			continue
		}
		// Mentions of what the copy made name the copies.
		var text []graphTextSpan
		if json.Unmarshal(value, &text) == nil && len(text) > 0 {
			if err := remapPageDuplicationSpans(app, jobID, [][]graphTextSpan{text}); err != nil {
				return "", err
			}
			if encoded, err := json.Marshal(text); err == nil {
				value = encoded
			}
		}
		moved[target] = value
	}
	encoded, err := json.Marshal(moved)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// remapDatabaseTemplateCopyProperties moves a template's values onto the
// copy's properties: IDs the copy made are its own, a relation to an item the
// copy made names the copied item, and a value of a property the source no
// longer has is left out. Files stay the files the template names.
func remapDatabaseTemplateCopyProperties(app core.App, jobID, raw string) (string, error) {
	values := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil || len(values) == 0 {
		return "{}", nil
	}
	propertyIDs := make([]string, 0, len(values))
	for propertyID := range values {
		propertyIDs = append(propertyIDs, propertyID)
	}
	properties, err := pageDuplicationReferenceMappings(app, jobID, "property", propertyIDs)
	if err != nil {
		return "", err
	}
	encodedIDs, err := json.Marshal(propertyIDs)
	if err != nil {
		return "", err
	}
	types := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
		Bind(dbx.Params{"ids": string(encodedIDs)}).All(&types); err != nil {
		return "", err
	}
	propertyTypes := make(map[string]string, len(types))
	for _, property := range types {
		propertyTypes[property.ID] = property.Type
	}
	relationRows := map[string]struct{}{}
	for propertyID, value := range values {
		if propertyTypes[propertyID] == graphValueTypeRelation {
			for _, rowID := range graphRelationRowIDs(value) {
				relationRows[rowID] = struct{}{}
			}
		}
	}
	rows, err := pageDuplicationReferenceMappings(app, jobID, "row", referenceIDs(relationRows))
	if err != nil {
		return "", err
	}
	trashed, err := trashedDatabaseRowIDs(app, referenceIDs(relationRows))
	if err != nil {
		return "", err
	}
	result := make(map[string]json.RawMessage, len(values))
	for propertyID, value := range values {
		target, found := properties[propertyID]
		if !found {
			continue
		}
		switch propertyTypes[propertyID] {
		case graphValueTypeFiles:
		case graphValueTypeRelation:
			if remapped, err := remapPageDuplicationRelationValue(value, rows, trashed); err == nil {
				value = remapped
			}
		default:
			if remapped, err := remapPageDuplicationDatabaseJSON(app, jobID, []json.RawMessage{value}); err == nil {
				value = remapped[0]
			}
		}
		result[target] = value
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

// removeDatabaseTemplateCopies removes the templates a copy job made on its
// target page, as rolling the copy back removes the databases they are of.
func removeDatabaseTemplateCopies(app core.App, jobID, ownerID, targetPageID string) error {
	// The templates are found by their page; the owner is read after (+),
	// or SQLite, which keeps no statistics, reads the owner's every template.
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_templates}}
		WHERE +[[owner]] = {:ownerId} AND [[page_id]] = {:targetPageId} AND [[data_source_id]] IN (
			SELECT [[target_id]] FROM {{leafen_page_duplication_identity_map}}
			WHERE [[job_id]] = {:jobId} AND [[kind]] = 'data_source')
	`).Bind(dbx.Params{"jobId": jobID, "ownerId": ownerID, "targetPageId": targetPageID}).Execute()
	if err != nil {
		return err
	}
	return wakeRowPageContentCleanup(app)
}
