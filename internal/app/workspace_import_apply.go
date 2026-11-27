package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func (r *durableJobRunner) applyWorkspaceImportItems(job storedJob, payload workspaceImportJobPayload) error {
	rootPageID := workspaceImportPrimaryRootPageID(payload)
	if r.ygo == nil || rootPageID == "" {
		return workspaceImportStateError()
	}
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil || source.Format != payload.Format {
		return workspaceImportStateError()
	}
	offset, err := strconv.ParseInt(job.Cursor, 10, 64)
	if err != nil {
		return workspaceImportStateError()
	}
	chunk, nextOffset, err := loadWorkspaceImportStagedChunk(r.app.DataDir(), source, offset, payload.ChunkIndex)
	if err != nil || payload.ItemOffset < 0 || payload.ItemOffset >= len(chunk.Items) {
		if err != nil {
			return err
		}
		return workspaceImportStateError()
	}
	end := min(payload.ItemOffset+workspaceImportMutationTurnItems, len(chunk.Items))
	// A CSV merge's new rows of a related database go to that database's
	// page, a turn of its own.
	targetPageID := rootPageID
	if payload.MergeDataSourceID != "" {
		if page := chunk.Items[payload.ItemOffset].PageID; page != "" {
			targetPageID = page
		}
		for index := payload.ItemOffset + 1; index < end; index++ {
			if chunk.Items[index].PageID != chunk.Items[payload.ItemOffset].PageID {
				end = index
				break
			}
		}
	}
	items := chunk.Items[payload.ItemOffset:end]
	operations, err := workspaceImportGraphOperations(payload.Format, items)
	if err != nil {
		return err
	}
	mutationID := fmt.Sprintf("workspace-import-%s-%d-%d", job.ID, payload.ChunkIndex, payload.ItemOffset)
	hooks := contentGraphMutationHooks{
		Authorize: workspaceImportGraphAuthorization(job, rootPageID), AllowTrashedPage: true,
	}
	if payload.ReplaceExistingPage {
		hooks, err = workspaceMarkdownReplaceMutationHooks(job, operations)
		if err != nil {
			return err
		}
	} else if payload.MergeDataSourceID != "" {
		hooks, err = workspaceCSVMergeMutationHooks(job, targetPageID, operations)
		if err != nil {
			return err
		}
	} else if payload.AppendToExistingPage {
		hooks, err = workspaceMarkdownAppendMutationHooks(job, rootPageID, operations)
		if err != nil {
			return err
		}
	}
	if payload.Format == workspaceImportFormatMarkdown {
		hooks.AfterApply = workspaceImportMarkdownTextApply(rootPageID, items)
	}
	hooks.SuppressNotifications = true
	input := graphMutationInput{MutationID: mutationID, Operations: operations}
	if payload.Format == workspaceImportFormatCSV && !payload.AppendToExistingPage {
		if input.PageKind, err = workspaceImportCSVPageKind(r.app, rootPageID, operations); err != nil {
			return err
		}
	}
	_, err = r.ygo.applyContentGraphMutation(job.Owner, targetPageID, input, hooks)
	if err != nil {
		return err
	}
	// The page's title and the sub-pages hanging on its fields follow its
	// text (workspace_import_field_pages.go).
	if (payload.Format == workspaceImportFormatMarkdown || payload.Format == workspaceImportFormatCSV) && workspaceImportItemsHangPages(items) {
		if err := r.app.RunInTransaction(func(txApp core.App) error {
			return hangWorkspaceImportSubPages(txApp, job.Owner, targetPageID, items)
		}); err != nil {
			return err
		}
	}
	return r.advanceWorkspaceImportCursor(job, payload, len(items), len(chunk.Items), end, nextOffset, chunk.Last)
}

// workspaceImportCSVPageKind makes a CSV's page a full-page database with
// the turn that gives it its database's block, as the body of a full-page
// database is its block from then on. A page within a wiki keeps its kind
// and shows the database inline.
func workspaceImportCSVPageKind(app core.App, pageID string, operations []graphOperation) (*string, error) {
	if !slices.ContainsFunc(operations, func(operation graphOperation) bool {
		return operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil &&
			operation.Block.Type == graphBlockTypeInlineDatabase
	}) {
		return nil, nil
	}
	within, err := pageWithinWiki(app, pageID)
	if err != nil || within {
		return nil, err
	}
	kind := pageKindDatabase
	return &kind, nil
}

func workspaceMarkdownAppendMutationHooks(
	job storedJob,
	pageID string,
	operations []graphOperation,
) (contentGraphMutationHooks, error) {
	canonical, err := json.Marshal(operations)
	if err != nil {
		return contentGraphMutationHooks{}, err
	}
	digest := sha256.Sum256(canonical)
	return contentGraphMutationHooks{
		Authorize:        workspaceImportGraphAuthorization(job, pageID),
		StructuralAppend: true,
		AllowTrashedPage: true,
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
			return hex.EncodeToString(digest[:]), nil
		},
		Prepare: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
			prepared := append([]graphOperation(nil), operations...)
			// The text follows the page's last block but the sub-pages' blocks
			// it puts in place, itself or inside a block of it (2026-11-13:
			// one following a sub-page's block the text then put in a toggle
			// was refused, and the page's whole text with it).
			placed := map[string]bool{}
			for _, operation := range prepared {
				if operation.Block != nil {
					placed[operation.Block.ID] = true
				}
			}
			for index := range prepared {
				block := prepared[index].Block
				if block == nil || block.ParentID != nil || block.AfterID != nil {
					continue
				}
				last := []string{}
				err := txApp.DB().NewQuery(`
					SELECT [[id]] FROM {{leafen_blocks}}
					WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] = ''
					ORDER BY [[position]] DESC, [[id]] DESC LIMIT {:limit}
				`).Bind(dbx.Params{
					"owner": authorization.Workspace.OwnerID, "pageId": pageID, "limit": len(placed) + 1,
				}).Column(&last)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				tailID := ""
				for _, id := range last {
					if !placed[id] {
						tailID = id
						break
					}
				}
				if tailID != "" {
					copy := *block
					copy.AfterID = &tailID
					prepared[index].Block = &copy
				}
				break
			}
			return contentGraphMutationPreparation{Operations: prepared}, nil
		},
	}, nil
}

func workspaceImportGraphOperations(format string, items []workspaceImportStagedItem) ([]graphOperation, error) {
	operations := make([]graphOperation, 0, len(items))
	for _, item := range items {
		if format == workspaceImportFormatCSV && item.Head != nil && item.ID == "" && item.Operation == nil {
			// The database page's icon and cover, written once its rows are in.
			continue
		}
		if format == workspaceImportFormatCSV || format == workspaceImportFormatNativeArchiveV2 {
			if item.Operation == nil || item.ID != "" || item.Type != "" {
				return nil, workspaceImportStateError()
			}
			operation := *item.Operation
			if format == workspaceImportFormatNativeArchiveV2 && operation.Kind == graphOperationKindUpsertBlock &&
				operation.Block != nil && operation.Block.Type == legacySimpleTableBlockType {
				expanded, err := workspaceImportLegacySimpleTableOperations(operation)
				if err != nil {
					return nil, err
				}
				operations = append(operations, expanded...)
			} else {
				operations = append(operations, operation)
			}
			continue
		}
		if format == workspaceImportFormatMarkdown && (item.Title != nil || item.Head != nil) && item.ID == "" {
			// The page's title, icon and cover, written once the text is in.
			continue
		}
		if format == workspaceImportFormatMarkdown && item.ID == "" && item.Type == "" && workspaceImportMarkdownDatabaseOperation(item.Operation) {
			// An inline database's data source, property, view or row
			// (workspace_import_markdown_database.go).
			operations = append(operations, *item.Operation)
			continue
		}
		if format != workspaceImportFormatMarkdown || item.Operation != nil || item.ID == "" {
			return nil, workspaceImportStateError()
		}
		var parentID *string
		if item.ParentID != "" {
			value := item.ParentID
			parentID = &value
		}
		var afterID *string
		if item.AfterID != "" {
			value := item.AfterID
			afterID = &value
		}
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: item.ID, ParentID: parentID, AfterID: afterID,
				Position: item.Position, Type: item.Type, Content: item.Content,
			},
		})
	}
	return operations, nil
}

func workspaceImportLegacySimpleTableOperations(operation graphOperation) ([]graphOperation, error) {
	tableContent, rows, err := nativeStructuredTableContents(operation.Block.Content)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	encodedTable, err := json.Marshal(tableContent)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	parent := *operation.Block
	parent.Type = graphBlockTypeTable
	parent.Content = encodedTable
	result := []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &parent}}
	var afterID *string
	for index, content := range rows {
		encodedRow, err := json.Marshal(content)
		if err != nil {
			return nil, workspaceImportInputError()
		}
		rowID := nativeStructuredTableRowID(parent.ID, index)
		tableID := parent.ID
		result = append(result, graphOperation{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: rowID, ParentID: &tableID, AfterID: afterID,
				Position: fmt.Sprintf("archive-table:%010d", index),
				Type:     graphBlockTypeTableRow, Content: encodedRow,
			},
		})
		current := rowID
		afterID = &current
	}
	return result, nil
}

func workspaceImportMarkdownTextApply(pageID string, items []workspaceImportStagedItem) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(txApp core.App, authorization pageAuthorization, doc *crdt.Doc) error {
		if err := alignWorkspaceImportSubPages(txApp, authorization, doc, pageID, items); err != nil {
			return err
		}
		documents := map[string]*notionYgoDocument{}
		document, err := loadNotionYgoDocument(txApp, pageID, documents)
		if err != nil {
			return err
		}
		document.doc.Transact(func(txn *crdt.Transaction) {
			for _, item := range items {
				if len(item.Spans) > 0 {
					txn.GetText(ygoBlockTextRootPrefix+item.ID).ApplyDelta(txn, notionGraphTextSpansDelta(item.Spans))
				} else if item.Text != "" {
					txn.GetText(ygoBlockTextRootPrefix+item.ID).Insert(txn, 0, item.Text, nil)
				}
			}
		})
		return saveNotionYgoDocuments(txApp, authorization.Workspace.PrincipalID, documents)
	}
}

func workspaceImportGraphAuthorization(job storedJob, pageID string) func(core.App) (pageAuthorization, error) {
	return func(txApp core.App) (pageAuthorization, error) {
		payload, payloadErr := decodeWorkspaceImportPayload(job)
		if payloadErr != nil {
			return pageAuthorization{}, payloadErr
		}
		if payload.ReplaceExistingPage {
			authorization, _, err := authorizeWorkspaceMarkdownReplace(txApp, job)
			return authorization, err
		}
		if payload.AppendToExistingPage {
			return authorizeWorkspaceImportTarget(txApp, job.Owner, job.WorkspaceID, pageID)
		}
		authorization, err := authorizeWorkspace(txApp, job.Owner, job.WorkspaceID, permissionWorkspaceWrite)
		if err != nil {
			return pageAuthorization{}, err
		}
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil || page.GetString("workspace") != authorization.WorkspaceID ||
			page.GetString("owner") != authorization.OwnerID ||
			!page.GetBool("notion_import_staging") && !workspaceImportStagedRowPage(txApp, pageID) {
			if err == nil {
				err = permissionDenied()
			}
			return pageAuthorization{}, err
		}
		return pageAuthorization{Workspace: authorization, Page: page}, nil
	}
}

// workspaceImportStagedRowPage reports whether a database page belongs to a
// page an import is still making: its database's page is one, or a database
// page that is, however deep (each host once, so a cycle ends).
func workspaceImportStagedRowPage(app core.App, pageID string) bool {
	var staged int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[hosts]]([[page_id]]) AS (
			SELECT [[row]].[[page_id]] FROM {{leafen_database_rows}} AS [[row]] WHERE [[row]].[[id]] = {:pageId}
			UNION
			SELECT [[row]].[[page_id]] FROM {{leafen_database_rows}} AS [[row]]
			JOIN [[hosts]] ON [[row]].[[id]] = [[hosts]].[[page_id]]
		)
		SELECT EXISTS (
			-- From the hosts by key, not every page (2026-10-18: SQLite's own
			-- order, given the nightly statistics).
			SELECT 1 FROM [[hosts]] CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[hosts]].[[page_id]]
			WHERE [[page]].[[notion_import_staging]] = TRUE
		)
	`).Bind(dbx.Params{"pageId": pageID}).Row(&staged)
	return err == nil && staged == 1
}

func (r *durableJobRunner) advanceWorkspaceImportCursor(job storedJob, payload workspaceImportJobPayload, applied, chunkItems, end int, nextOffset int64, last bool) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex ||
			currentPayload.ItemOffset != payload.ItemOffset || current.Cursor != job.Cursor {
			if err != nil {
				return err
			}
			return workspaceImportStateError()
		}
		cursor := current.Cursor
		currentPayload.ItemOffset = end
		if end == chunkItems {
			cursor = strconv.FormatInt(nextOffset, 10)
			currentPayload.ItemOffset = 0
			currentPayload.ChunkIndex++
			currentPayload.ReadyToPublish = last
		}
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, cursor, current.Processed+applied, true,
		)
	})
}
