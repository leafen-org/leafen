package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// applyNotionContentBlock makes a block whose whole content was staged: a
// table, a table row, a bookmark or a link preview; one a run made before
// takes what the source changed since (rewriteNotionMigrationContentBlock),
// true when it did.
func applyNotionContentBlock(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
) (bool, error) {
	changed, err := false, error(nil)
	if blockID, found := mappings.nativeID(source.SourceID, "block"); found {
		changed, err = rewriteNotionMigrationContentBlock(app, job, payload, source, blockID, structuralDocuments)
	} else {
		err = makeNotionContentBlock(app, job, payload, source, structuralDocuments, mappings)
	}
	refused := &notionImportRefusedError{}
	if errors.As(err, &refused) {
		// A table, its row or a link Leafen won't take is left out and reported.
		return false, recordNotionImportApplyIssue(app, job.ID, source.SourceID, source.Kind, notionImportIssueInvalidBlock)
	}
	return changed, err
}

func makeNotionContentBlock(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
) error {
	if source.Kind == graphBlockTypeTableRow && source.ParentKind == "block" {
		if _, found := mappings.nativeID(source.ParentSourceID, "block"); !found {
			// The table it belongs to was left out.
			return nil
		}
	}
	pageID, parentID, err := notionImportBlockParent(app, source, mappings)
	if err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return err
	}
	blockID := uuid.NewString()
	var afterID *string
	if source.Kind == graphBlockTypeTableRow && source.Position == 0 {
		blockID = nativeStructuredTableRowID(parentID, 0)
	} else if afterID, err = placeNotionImportBlock(app, job, payload, source, batch, parentID, blockID, mappings); err != nil {
		return err
	}
	content := json.RawMessage(source.Text)
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertBlock,
		Block: &graphBlock{
			ID: blockID, ParentID: optionalNotionImportParentID(parentID), AfterID: afterID,
			Position: fmt.Sprintf("notion:%010d", source.Position),
			Type:     source.Kind, Content: content,
		},
	}}
	// A table stands with its first row's place kept, which its first row
	// takes; one without rows stands as it is.
	rowed := false
	if source.Kind == graphBlockTypeTable {
		if rowed, err = notionImportTableRowed(app, job.ID, source.SourceID); err != nil {
			return err
		}
	}
	if rowed {
		table, err := parseGraphTableBlockContent(content)
		if err != nil {
			return err
		}
		cells := make([][]graphTextSpan, table.TableWidth)
		for index := range cells {
			cells[index] = []graphTextSpan{}
		}
		rowContent, err := json.Marshal(graphTableRowBlockContent{Cells: cells})
		if err != nil {
			return err
		}
		tableID := blockID
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: nativeStructuredTableRowID(blockID, 0), ParentID: &tableID,
				Position: "notion:0000000000", Type: graphBlockTypeTableRow, Content: rowContent,
			},
		})
	}
	if err := applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
	); err != nil {
		return err
	}
	return mappings.insert(source.SourceID, blockID, "block")
}

// notionImportTableRowed tells whether a staged table has rows staged under
// it; all are staged before any is made.
func notionImportTableRowed(app core.App, jobID, sourceID string) (bool, error) {
	count := 0
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[parent_source_id]]={:sourceId} AND [[parent_kind]]='block'
			AND [[kind]]={:kind} LIMIT 1)
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID, "kind": graphBlockTypeTableRow}).Row(&count)
	return count > 0, err
}
