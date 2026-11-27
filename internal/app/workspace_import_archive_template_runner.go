package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceImportTemplateFiles struct {
	unscoped   []string
	properties map[string][]string
	// body is the files the template's body blocks hold, referenced by the
	// blocks of the template's page.
	body []string
}

func (r *durableJobRunner) applyWorkspaceArchiveTemplates(
	job storedJob,
	payload workspaceImportJobPayload,
	chunk workspaceImportStagedChunk,
	nextOffset int64,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex || currentPayload.ItemOffset != 0 {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-template-frontier")
		}
		authorization, err := authorizeWorkspace(
			txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		templates, references, err := validateWorkspaceImportTemplateChunk(
			txApp, current, authorization, chunk,
		)
		if err != nil {
			return err
		}
		allFileIDs := []string{}
		for _, reference := range references {
			allFileIDs = append(allFileIDs, reference.unscoped...)
			allFileIDs = append(allFileIDs, reference.body...)
			for _, ids := range reference.properties {
				allFileIDs = append(allFileIDs, ids...)
			}
		}
		if len(allFileIDs) > 0 {
			if _, err := loadGraphFileAuthorityRecords(
				txApp, authorization.WorkspaceID, uniqueWorkspaceImportCommentIDs(allFileIDs),
			); err != nil {
				return workspaceImportFailureAt(err, "archive-template-file-authority")
			}
		}
		now := r.clock.now().UTC().Format(time.RFC3339Nano)
		for index, template := range templates {
			createdAt := template.CreatedAt
			if createdAt == "" {
				createdAt = now
			}
			// Formatting stays only where it spells its text value; which of
			// those values are text is read where the template is used.
			values := map[string]json.RawMessage{}
			_ = json.Unmarshal(template.Properties, &values)
			textValues := make(map[string]bool, len(values))
			for propertyID := range values {
				textValues[propertyID] = true
			}
			spans, err := databaseTemplateTextSpans(textValues, string(template.Properties), template.PropertySpans, false)
			if err != nil {
				return workspaceImportFailureAt(err, "archive-template-spans")
			}
			if _, err := txApp.DB().Insert(databaseTemplatesTable, dbx.Params{
				"id": template.ID, "workspace_id": authorization.WorkspaceID,
				"owner": authorization.OwnerID, "page_id": template.PageID,
				"data_source_id": template.DataSourceID, "name": template.Name,
				"icon": template.Icon, "title": template.Title,
				"properties_json": string(template.Properties), "property_spans_json": spans, "blocks_json": "[]",
				"is_default": template.IsDefault, "position": template.Position,
				"workspace_import_job_id": current.ID, "created_at": createdAt, "updated_at": now,
			}).Execute(); err != nil {
				return workspaceImportFailureAt(err, "archive-template-insert")
			}
			if err := insertWorkspaceImportTemplateFileReferences(
				txApp, authorization.WorkspaceID, template, references[index],
			); err != nil {
				return err
			}
			// The template's page, made with the template, takes its body.
			blocks, err := databaseTemplateBodyBlocks(template.ID, template.Blocks, true)
			if err != nil {
				return workspaceImportStateErrorAt("archive-template-body")
			}
			if err := writeRowPageBody(txApp, template.ID, current.Owner, blocks, r.clock.now().UTC().UnixMilli(), false); err != nil {
				return workspaceImportFailureAt(err, "archive-template-body-write")
			}
		}
		currentPayload.SourceVerified = true
		currentPayload.ItemOffset = 0
		currentPayload.ChunkIndex++
		currentPayload.ReadyToPublish = chunk.Last
		currentPayload.Phase = workspaceImportPhaseApply
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, strconv.FormatInt(nextOffset, 10),
			current.Processed+len(chunk.Items), true,
		)
	})
}

func validateWorkspaceImportTemplateChunk(
	app core.App,
	job storedJob,
	authorization workspaceAuthorization,
	chunk workspaceImportStagedChunk,
) ([]workspaceImportStagedTemplate, []workspaceImportTemplateFiles, error) {
	templates := make([]workspaceImportStagedTemplate, len(chunk.Items))
	references := make([]workspaceImportTemplateFiles, len(chunk.Items))
	pageIDs, dataSourceIDs, propertyIDs := []string{}, []string{}, []string{}
	seen := map[string]struct{}{}
	for index, item := range chunk.Items {
		if item.Template == nil || item.Page != nil || item.Comment != nil || item.Operation != nil ||
			item.PageID == "" || item.PageID != item.Template.PageID {
			return nil, nil, workspaceImportStateErrorAt("archive-template-item")
		}
		template := *item.Template
		if _, duplicate := seen[template.ID]; duplicate || !validWorkspaceImportStagedTemplate(template) {
			return nil, nil, workspaceImportStateErrorAt("archive-template-shape")
		}
		seen[template.ID] = struct{}{}
		reference, err := workspaceImportTemplateFileIDs(template)
		if err != nil {
			return nil, nil, err
		}
		templates[index], references[index] = template, reference
		pageIDs = append(pageIDs, template.PageID)
		dataSourceIDs = append(dataSourceIDs, template.DataSourceID)
		for propertyID := range reference.properties {
			propertyIDs = append(propertyIDs, propertyID)
		}
	}
	pageIDs = uniqueWorkspaceImportCommentIDs(pageIDs)
	if err := validateWorkspaceImportCommentPages(app, job, authorization, pageIDs); err != nil {
		return nil, nil, err
	}
	params := dbx.Params{"owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID}
	sources, err := loadWorkspaceImportCommentOwners(
		app, "leafen_data_sources", "[[owner]]={:owner} AND [[workspace_id]]={:workspaceId}",
		uniqueWorkspaceImportCommentIDs(dataSourceIDs), params,
	)
	if err != nil || len(sources) != len(uniqueWorkspaceImportCommentIDs(dataSourceIDs)) {
		return nil, nil, workspaceImportStateErrorAt("archive-template-data-source-owner")
	}
	properties, err := loadWorkspaceImportCommentOwners(
		app, "leafen_database_properties", "[[owner]]={:owner} AND [[workspace_id]]={:workspaceId}",
		uniqueWorkspaceImportCommentIDs(propertyIDs), params,
	)
	if err != nil || len(properties) != len(uniqueWorkspaceImportCommentIDs(propertyIDs)) {
		return nil, nil, workspaceImportStateErrorAt("archive-template-property-owner")
	}
	for index, template := range templates {
		if sources[template.DataSourceID].PageID != template.PageID {
			return nil, nil, workspaceImportStateErrorAt("archive-template-data-source-page")
		}
		for propertyID := range references[index].properties {
			if properties[propertyID].PageID != template.PageID {
				return nil, nil, workspaceImportStateErrorAt("archive-template-property-page")
			}
		}
	}
	return templates, references, nil
}

// validWorkspaceImportStagedTemplate takes a template's and its data
// source's IDs as any graph identifier: a moved workspace keeps them as made
// (UUIDs), not a record's 15 characters; its icon is any template icon
// (workspaceImportArchiveTemplateIcon).
func validWorkspaceImportStagedTemplate(template workspaceImportStagedTemplate) bool {
	if !validWorkspaceImportArchiveGraphID(template.ID) ||
		!validWorkspaceImportArchiveGraphID(template.PageID) ||
		!validWorkspaceImportArchiveGraphID(template.DataSourceID) ||
		len(template.Icon) > maxDatabaseTemplateIconBytes || !utf8.ValidString(template.Icon) ||
		strings.TrimSpace(template.Position) != template.Position || template.Position == "" ||
		len([]rune(template.Name)) > maxGraphTitleRunes || len([]rune(template.Title)) > maxGraphTitleRunes ||
		len(template.Properties) == 0 || len(template.Properties) > workspaceImportArchiveEntityJSONBytes ||
		len(template.Blocks) == 0 || len(template.Blocks) > workspaceImportArchiveEntityJSONBytes ||
		!json.Valid(template.Properties) || !json.Valid(template.Blocks) {
		return false
	}
	if template.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, template.CreatedAt); err != nil {
			return false
		}
	}
	var properties map[string]any
	var blocks []any
	return json.Unmarshal(template.Properties, &properties) == nil && json.Unmarshal(template.Blocks, &blocks) == nil
}

func workspaceImportTemplateFileIDs(
	template workspaceImportStagedTemplate,
) (workspaceImportTemplateFiles, error) {
	result := workspaceImportTemplateFiles{properties: map[string][]string{}}
	// A picture icon names its file at its address or, as staged before
	// icons kept their address, by its ID.
	if fileID, managed := productFilePresentationID(template.Icon); managed {
		result.unscoped = append(result.unscoped, fileID)
	}
	properties := map[string]any{}
	if json.Unmarshal(template.Properties, &properties) != nil {
		return result, workspaceImportStateErrorAt("archive-template-properties")
	}
	for propertyID, value := range properties {
		if !workspaceExportFileRecordIDPattern.MatchString(propertyID) {
			return result, workspaceImportStateErrorAt("archive-template-property")
		}
		ids, err := collectWorkspaceImportTemplateFileIDs(value, "")
		if err != nil {
			return result, err
		}
		result.properties[propertyID] = ids
	}
	blocks := any(nil)
	if json.Unmarshal(template.Blocks, &blocks) != nil {
		return result, workspaceImportStateErrorAt("archive-template-blocks")
	}
	ids, err := collectWorkspaceImportTemplateFileIDs(blocks, "")
	if err != nil {
		return result, err
	}
	result.body = uniqueWorkspaceImportCommentIDs(ids)
	result.unscoped = uniqueWorkspaceImportCommentIDs(result.unscoped)
	for propertyID, values := range result.properties {
		result.properties[propertyID] = uniqueWorkspaceImportCommentIDs(values)
	}
	return result, nil
}

func collectWorkspaceImportTemplateFileIDs(value any, field string) ([]string, error) {
	ids := []string{}
	var walk func(any, string) error
	walk = func(current any, currentField string) error {
		if currentField == "fileId" || currentField == "uploadId" || currentField == "fileUploadId" {
			id, ok := current.(string)
			if !ok || !workspaceExportFileRecordIDPattern.MatchString(id) {
				return workspaceImportStateErrorAt("archive-template-file")
			}
			ids = append(ids, id)
			return nil
		}
		if currentField == "attachmentIds" {
			values, ok := current.([]any)
			if !ok {
				return workspaceImportStateErrorAt("archive-template-attachments")
			}
			for _, value := range values {
				id, ok := value.(string)
				if !ok || !workspaceExportFileRecordIDPattern.MatchString(id) {
					return workspaceImportStateErrorAt("archive-template-attachment")
				}
				ids = append(ids, id)
			}
			return nil
		}
		switch typed := current.(type) {
		case []any:
			for _, child := range typed {
				if err := walk(child, currentField); err != nil {
					return err
				}
			}
		case map[string]any:
			for key, child := range typed {
				if err := walk(child, key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value, field); err != nil {
		return nil, err
	}
	return ids, nil
}

func insertWorkspaceImportTemplateFileReferences(
	app core.App,
	workspaceID string,
	template workspaceImportStagedTemplate,
	references workspaceImportTemplateFiles,
) error {
	insert := func(propertyID string, ids []string) error {
		if len(ids) == 0 {
			return nil
		}
		items := make(graphFilesValue, len(ids))
		for index, fileID := range ids {
			items[index] = graphFileValueItem{FileID: fileID}
		}
		return insertProductFileReferences(
			app, workspaceID, template.PageID, productFileReferenceOwnerTemplate,
			template.ID, propertyID, items,
		)
	}
	if err := insert("", references.unscoped); err != nil {
		return workspaceImportFailureAt(err, "archive-template-file-reference")
	}
	for propertyID, ids := range references.properties {
		if err := insert(propertyID, ids); err != nil {
			return workspaceImportFailureAt(err, "archive-template-file-reference")
		}
	}
	return nil
}
