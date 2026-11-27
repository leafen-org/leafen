package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// applyNotionFile makes a file, image, PDF, video or audio block: true when
// it made one, when the file was left out, and when a run again changed one
// a run made before (rewriteNotionMigrationFileBlock).
func applyNotionFile(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
) (bool, bool, bool, error) {
	if blockID, found := mappings.nativeID(source.SourceID, "block"); found {
		changed, err := rewriteNotionMigrationFileBlock(app, job, payload, source, blockID, structuralDocuments)
		return false, false, changed, err
	}
	metadata, err := decodeNotionFileSourceMetadata(source.Text)
	if err != nil {
		return false, false, false, err
	}
	pageID, parentID, err := notionImportBlockParent(app, source, mappings)
	if err != nil {
		return false, false, false, err
	}
	caption, err := notionSourceTextGraphSpans(metadata.Caption)
	if err != nil {
		return false, false, false, err
	}
	content, blockType := []byte(nil), source.Kind
	if metadata.ExternalURL != "" {
		value := map[string]any{"url": metadata.ExternalURL, "caption": caption}
		if source.Kind == graphBlockTypeFile {
			value["source"], value["name"] = graphExternalFileSource, source.Title
		}
		content, err = json.Marshal(value)
	} else {
		task, loadErr := loadNotionFileTask(app, job.ID, source.SourceID)
		if loadErr != nil {
			return false, false, false, loadErr
		}
		if task.Status == "omitted" {
			return false, true, false, nil
		}
		if task.Status != "copied" || task.FileID == "" {
			return false, false, false, errors.New(errorCodeNotionImportFileCopyRequired)
		}
		file, loadErr := app.FindRecordById(filesCollection, task.FileID)
		if loadErr != nil || !workspaceFileRecordBelongsTo(file, job.WorkspaceID) {
			return false, false, false, errors.New(errorCodeNotionImportFileCopyRequired)
		}
		// An embed of a file Notion kept stays an embed where one can show the file.
		if metadata.Block == graphBlockTypeEmbed &&
			notionManagedFileContentCompatible(graphBlockTypeEmbed, canonicalGraphFileContent(file)) {
			blockType = graphBlockTypeEmbed
		}
		content, err = json.Marshal(map[string]any{
			"fileId": file.Id, "name": file.GetString("original_name"),
			"size": int64(file.GetFloat("size")), "contentType": file.GetString("content_type"),
			"sha256": file.GetString("sha256"), "caption": caption,
		})
	}
	if err != nil {
		return false, false, false, err
	}
	blockID := uuid.NewString()
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return false, false, false, err
	}
	afterID, err := placeNotionImportBlock(app, job, payload, source, batch, parentID, blockID, mappings)
	if err != nil {
		return false, false, false, err
	}
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertBlock,
		Block: &graphBlock{
			ID: blockID, ParentID: optionalNotionImportParentID(parentID), AfterID: afterID,
			Position: fmt.Sprintf("notion:%010d", source.Position),
			Type:     blockType, Content: content,
		},
	}}
	if err := applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
	); err != nil {
		return false, false, false, err
	}
	if err := mappings.insert(source.SourceID, blockID, "block"); err != nil {
		return false, false, false, err
	}
	return true, false, false, nil
}
