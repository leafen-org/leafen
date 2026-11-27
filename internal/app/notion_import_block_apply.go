package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// applyNotionStagedTextBlock makes the native block for a staged paragraph,
// template or converted block, with its rich text in the page's text
// document. A block made before takes what the source changed since, and
// says true when it did.
func applyNotionStagedTextBlock(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	documents map[string]*notionYgoDocument,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
) (bool, error) {
	plan, err := decodeNotionParagraphSourceText(source.Text)
	if err != nil {
		return false, err
	}
	if blockID, found := mappings.nativeID(source.SourceID, "block"); found {
		if plan.LinkedDatabase {
			return false, nil
		}
		return rewriteNotionMigrationBlock(app, job, payload, source, blockID, documents, structuralDocuments)
	}
	blockType := notionStagedBlockType(source, &plan)
	if _, supported := graphBlockTypes[blockType]; !supported {
		return false, errors.New(errorCodeNotionImportCreateFailed)
	}
	if err := applyNotionImportBlockIcon(app, job, source.SourceID, plan.Content); err != nil {
		return false, err
	}
	content := json.RawMessage(`{}`)
	if len(plan.Content) > 0 {
		if content, err = json.Marshal(plan.Content); err != nil {
			return false, err
		}
	}
	pageID, parentID, err := notionImportBlockParent(app, source, mappings)
	if err != nil {
		return false, err
	}
	blockID := uuid.NewString()
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return false, err
	}
	afterID, err := placeNotionImportBlock(app, job, payload, source, batch, parentID, blockID, mappings)
	if err != nil {
		return false, err
	}
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertBlock,
		Block: &graphBlock{
			ID: blockID, ParentID: optionalNotionImportParentID(parentID), AfterID: afterID,
			Position: fmt.Sprintf("notion:%010d", source.Position),
			Type:     blockType, Content: content,
		},
	}}
	err = applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
	)
	refused := &notionImportRefusedError{}
	if errors.As(err, &refused) {
		// A block Leafen won't take as it stands keeps its words and what is
		// inside it as a paragraph, and is reported.
		blockType = graphBlockTypeParagraph
		operations[0].Block.Type, operations[0].Block.Content = blockType, json.RawMessage(`{}`)
		err = applyNotionGraphOperations(
			app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
		)
		if err == nil {
			err = recordNotionImportApplyIssue(app, job.ID, source.SourceID, notionImportStagedIssueType(source, plan), notionImportIssueSettingOmitted)
		}
	}
	if err != nil {
		return false, err
	}
	if err := mappings.insert(source.SourceID, blockID, "block"); err != nil {
		return false, err
	}
	if err := keepNotionMigrationBlockSourceText(app, payload.MigrationID, source.SourceID, blockID, source.Text); err != nil {
		return false, err
	}
	if _, text := graphTextBlockTypes[blockType]; !text && blockType != graphBlockTypeTemplate {
		return false, nil
	}
	return false, applyNotionSourceText(app, pageID, blockID, source.Text, documents)
}

// notionImportStagedIssueType names a staged block in a report: its type, as it
// was staged.
func notionImportStagedIssueType(source storedNotionSource, plan notionParagraphSourcePlan) string {
	if source.Kind == "block" && plan.BlockType != "" {
		return plan.BlockType
	}
	return source.Kind
}

// notionStagedBlockType is the native type a staged block takes: a template's,
// or a converted block's own; a paragraph or template staged before
// conversion existed names none, and a template keeps no content.
func notionStagedBlockType(source storedNotionSource, plan *notionParagraphSourcePlan) string {
	switch source.Kind {
	case "template":
		plan.Content = nil
		return graphBlockTypeTemplate
	case "block":
		return plan.BlockType
	}
	return source.Kind
}
