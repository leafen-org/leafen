package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// rewriteNotionMigrationContentBlock gives a block whose whole content was
// staged — a table, a table row, a bookmark or a link preview — and that a
// run made before the content the source changed since, by the rule a block
// of words follows (rewriteNotionMigrationBlock): only where the source's
// now differs from what the run that last took it was given, so one the
// source left as it was keeps what a person changed in Leafen. A row's
// cells then mention the pages made on them since, which hang on the row
// (2026-11-18: a cell's new page hung on a row whose cells, never written
// again, didn't mention it, for its next edit to send it to the trash).
// Its mentions point at what they name when the run publishes the page
// (resolveNotionImportPageReferences). True when the source had changed it.
func rewriteNotionMigrationContentBlock(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, blockID string,
	structuralDocuments map[string]*structuralMutationBatch,
) (bool, error) {
	plan, err := decodeNotionParagraphSourceText(source.Text)
	if err != nil {
		return false, err
	}
	if changed, err := notionMigrationBlockChanged(app, payload, source.SourceID, plan); err != nil || !changed {
		return false, err
	}
	stored, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil || !found {
		return false, err
	}
	if err := writeNotionMigrationBlock(app, job, payload, stored, source.Kind, json.RawMessage(source.Text), structuralDocuments); err != nil {
		return false, err
	}
	// Its new cells are searched for once the turn's save keeps them
	// (saveStructuralMutationBatches).
	return true, keepNotionMigrationBlockRewritten(app, payload.MigrationID, source, blockID, stored.PageID)
}

// rewriteNotionMigrationFileBlock gives a file, image, PDF, video or audio
// block a run made before the caption the source changed since, and an
// outside file's address, by the same rule; the file it holds stays as the
// run that copied it left it. A caption's mentions then name the pages made
// on it since, which hang on the block. True when the source had changed it.
func rewriteNotionMigrationFileBlock(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, blockID string,
	structuralDocuments map[string]*structuralMutationBatch,
) (bool, error) {
	plan, err := decodeNotionParagraphSourceText(source.Text)
	if err != nil {
		return false, err
	}
	if changed, err := notionMigrationBlockChanged(app, payload, source.SourceID, plan); err != nil || !changed {
		return false, err
	}
	metadata, err := decodeNotionFileSourceMetadata(source.Text)
	if err != nil {
		return false, err
	}
	caption, err := notionSourceTextGraphSpans(metadata.Caption)
	if err != nil {
		return false, err
	}
	stored, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil || !found {
		return false, err
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(stored.Content), &content) != nil {
		return false, nil
	}
	content["caption"] = caption
	if _, outside := content["url"]; outside && metadata.ExternalURL != "" {
		content["url"] = metadata.ExternalURL
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	if err := writeNotionMigrationBlock(app, job, payload, stored, stored.Type, encoded, structuralDocuments); err != nil {
		return false, err
	}
	// Its new caption is searched for once the turn's save keeps it
	// (saveStructuralMutationBatches).
	return true, keepNotionMigrationBlockRewritten(app, payload.MigrationID, source, blockID, stored.PageID)
}

// keepNotionMigrationBlockRewritten has the run's end say again how it left a
// block a run again wrote, and keeps the block's source as this run took it.
func keepNotionMigrationBlockRewritten(app core.App, migrationID string, source storedNotionSource, blockID, pageID string) error {
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migration_ledger}} SET [[state]]='', [[page_id]]={:pageId}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]='block'
	`).Bind(dbx.Params{"migrationId": migrationID, "sourceId": source.SourceID, "pageId": pageID}).Execute(); err != nil {
		return err
	}
	return keepNotionMigrationBlockSourceText(app, migrationID, source.SourceID, blockID, source.Text)
}

// writeNotionMigrationBlock gives a block a run made before a kind and
// content where it stands, when either differs from what it holds.
func writeNotionMigrationBlock(
	app core.App, job storedJob, payload notionImportJobPayload, stored storedNotionBlock, blockType string,
	content json.RawMessage, structuralDocuments map[string]*structuralMutationBatch,
) error {
	if string(content) == stored.Content && blockType == stored.Type {
		return nil
	}
	batch, err := loadStructuralMutationBatch(app, stored.PageID, structuralDocuments)
	if err != nil {
		return err
	}
	block := &graphBlock{ID: stored.ID, Position: stored.Position, Type: blockType, Content: content}
	if stored.ParentID != "" {
		parentID := stored.ParentID
		block.ParentID = &parentID
	}
	return applyNotionGraphOperations(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, stored.PageID, job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}})
}
