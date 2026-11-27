package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceImportStagedRowPageBlock struct {
	ID        string          `json:"id"`
	RowID     string          `json:"rowId"`
	ParentID  string          `json:"parentId"`
	Position  string          `json:"position"`
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

// workspaceImportStagedRowBlockTrash is a database page's trash entry: in the
// page form (Trash), or in the row form an import staged before archives held
// the page form (workspace_import_archive_row_trash.go).
type workspaceImportStagedRowBlockTrash struct {
	RootBlockID   string                           `json:"rootBlockId"`
	RowID         string                           `json:"rowId"`
	ParentID      string                           `json:"parentId"`
	PredecessorID string                           `json:"predecessorId"`
	SuccessorID   string                           `json:"successorId"`
	Snapshot      databaseRowBlockTrashWire        `json:"snapshot"`
	DeletedAt     int64                            `json:"deletedAt"`
	ExpiresAt     int64                            `json:"expiresAt"`
	Trash         *workspaceImportStagedBlockTrash `json:"trash,omitempty"`
}

func decodeWorkspaceImportArchiveRowPageBlock(raw map[string]any) (archiveRowPageBlockValue, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return archiveRowPageBlockValue{}, workspaceImportInputError()
	}
	value := archiveRowPageBlockValue{}
	if json.Unmarshal(encoded, &value) != nil || !validWorkspaceImportArchiveRowBlockID(value.ID) ||
		!validWorkspaceImportArchiveRowBlockID(value.PageID) ||
		!validWorkspaceImportArchiveRowBlockID(value.RowID) ||
		!validWorkspaceImportArchiveOptionalRowBlockID(value.ParentID) ||
		value.ID == value.ParentID || value.Position == "" || strings.TrimSpace(value.Position) != value.Position ||
		len(value.Position) > maxGraphPositionLength || !workspaceImportArchiveRowBlockType(value.Type) ||
		value.CreatedAt < 1 || value.UpdatedAt < value.CreatedAt || len(value.Content) == 0 {
		return archiveRowPageBlockValue{}, workspaceImportInputError()
	}
	content := map[string]any{}
	if json.Unmarshal(value.Content, &content) != nil || content == nil ||
		validateWorkspaceImportArchiveRowBlockContent(value.Type, value.Content) != nil {
		return archiveRowPageBlockValue{}, workspaceImportInputError()
	}
	return value, nil
}

func decodeWorkspaceImportArchiveRowBlockTrash(raw map[string]any) (archiveRowBlockTrashValue, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return archiveRowBlockTrashValue{}, workspaceImportInputError()
	}
	value := archiveRowBlockTrashValue{}
	if json.Unmarshal(encoded, &value) != nil ||
		!validWorkspaceImportArchiveRowBlockID(value.RootBlockID) ||
		!validWorkspaceImportArchiveRowBlockID(value.PageID) ||
		!validWorkspaceImportArchiveRowBlockID(value.RowID) ||
		!validWorkspaceImportArchiveOptionalRowBlockID(value.ParentID) ||
		!validWorkspaceImportArchiveOptionalRowBlockID(value.PredecessorID) ||
		!validWorkspaceImportArchiveOptionalRowBlockID(value.SuccessorID) ||
		value.DeletedAt < 0 || value.ExpiresAt <= value.DeletedAt ||
		value.BlockCount < 1 ||
		value.Snapshot.Format != 1 || value.Snapshot.RootID != value.RootBlockID ||
		len(value.Snapshot.Blocks) != value.BlockCount {
		return archiveRowBlockTrashValue{}, workspaceImportInputError()
	}
	seen := map[string]struct{}{}
	for index, block := range value.Snapshot.Blocks {
		if !validWorkspaceImportArchiveRowBlockID(block.ID) ||
			!validWorkspaceImportArchiveOptionalRowBlockID(block.ParentID) ||
			block.Position == "" || strings.TrimSpace(block.Position) != block.Position ||
			len(block.Position) > maxGraphPositionLength || !workspaceImportArchiveRowBlockType(block.Type) ||
			block.CreatedAt < 1 || block.UpdatedAt < block.CreatedAt || len(block.Content) == 0 {
			return archiveRowBlockTrashValue{}, workspaceImportInputError()
		}
		if _, duplicate := seen[block.ID]; duplicate {
			return archiveRowBlockTrashValue{}, workspaceImportInputError()
		}
		if index == 0 {
			if block.ID != value.RootBlockID || block.ParentID != value.ParentID {
				return archiveRowBlockTrashValue{}, workspaceImportInputError()
			}
		} else if _, parent := seen[block.ParentID]; !parent {
			return archiveRowBlockTrashValue{}, workspaceImportInputError()
		}
		content := map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil || content == nil ||
			validateWorkspaceImportArchiveRowBlockContent(block.Type, block.Content) != nil {
			return archiveRowBlockTrashValue{}, workspaceImportInputError()
		}
		seen[block.ID] = struct{}{}
	}
	if len(encoded) > workspaceImportArchiveEntityJSONBytes {
		return archiveRowBlockTrashValue{}, workspaceImportInputError()
	}
	return value, nil
}

func validateWorkspaceImportArchiveRowBlockContent(blockType string, content json.RawMessage) error {
	block := &graphBlock{Type: blockType, Content: content}
	if !graphBlockCarriesManagedFileReference(block) {
		return nil
	}
	fields := map[string]any{}
	if json.Unmarshal(content, &fields) != nil {
		return workspaceImportInputError()
	}
	marker, ok := fields["fileId"].(string)
	if !ok || !strings.HasPrefix(marker, workspaceImportArchiveFileMarkerPrefix) {
		return workspaceImportInputError()
	}
	sourceID := strings.TrimPrefix(marker, workspaceImportArchiveFileMarkerPrefix)
	if !workspaceExportFileRecordIDPattern.MatchString(sourceID) {
		return workspaceImportInputError()
	}
	fields["fileId"] = sourceID
	canonical, err := json.Marshal(fields)
	if err != nil {
		return workspaceImportInputError()
	}
	if _, err := parseGraphFileBlockContent(canonical); err != nil {
		return workspaceImportInputError()
	}
	return nil
}

// workspaceImportArchiveRowBlockType admits the blocks a database page's body
// holds in an archive: a page's blocks, with its sub-pages, links and
// databases, but not a full-page database.
func workspaceImportArchiveRowBlockType(blockType string) bool {
	_, known := graphBlockTypes[blockType]
	return known && !databaseRowBodyRejectsType(blockType)
}

// workspaceImportArchiveRowBlockOperation makes an archived database page's
// body block a change to the restored database page, as a page's block is,
// so its databases, sub-pages and links come back as a page's do. As for a
// page, blocks first come back at the top and then under their parents with
// their text.
func workspaceImportArchiveRowBlockOperation(db *sql.DB, mode, encoded string) (workspaceImportStagedItem, error) {
	raw := map[string]any{}
	if json.Unmarshal([]byte(encoded), &raw) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	value, err := decodeWorkspaceImportArchiveRowPageBlock(raw)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	baseMode, _, validMode := workspaceImportArchiveBlockMode(mode)
	if !validMode {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	rowID, err := workspaceImportArchiveReference(db, value.RowID, workspaceExportItemRow)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemRowPageBlock)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	var parent, afterID *string
	start := false
	if baseMode == "full" {
		if value.ParentID != "" {
			mapped, err := workspaceImportArchiveReference(db, value.ParentID, workspaceExportItemRowPageBlock)
			if err != nil {
				return workspaceImportStagedItem{}, err
			}
			parent = &mapped
		}
		var predecessor string
		err := db.QueryRow(`
			SELECT source_id FROM entities INDEXED BY entities_row_block_order
			WHERE kind = ? AND json_extract(value_json, '$.rowId') = ? AND parent_id = ?
				AND (position < ? OR (position = ? AND source_id < ?))
			ORDER BY position DESC, source_id DESC LIMIT 1
		`, workspaceExportItemRowPageBlock, value.RowID, value.ParentID, value.Position, value.Position, value.ID).
			Scan(&predecessor)
		if errors.Is(err, sql.ErrNoRows) {
			start = true
		} else if err != nil {
			return workspaceImportStagedItem{}, workspaceImportStorageError(err)
		} else {
			mapped, err := workspaceImportArchiveReference(db, predecessor, workspaceExportItemRowPageBlock)
			if err != nil {
				return workspaceImportStagedItem{}, err
			}
			afterID = &mapped
		}
	}
	rewritten, err := rewriteWorkspaceImportArchiveJSON(db, value.Content)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	// An archive carries a database page's text in its blocks' content; it
	// comes back as the page's text.
	fields := map[string]any{}
	if json.Unmarshal(rewritten, &fields) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	text, _ := fields["text"].(string)
	spansValue, hasSpans := fields["notionRichTextSpans"]
	delete(fields, "text")
	delete(fields, "notionRichTextSpans")
	content, err := json.Marshal(fields)
	if err != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	block := archiveBlockValue{ID: value.ID, PageID: value.RowID, Position: value.Position, Type: value.Type, Content: value.Content}
	if _, selected, err := workspaceImportArchiveLinkedViewSelection(db, block, id); err != nil {
		return workspaceImportStagedItem{}, err
	} else if selected != "" {
		if content, err = workspaceImportArchiveLinkedBlockContent(content, selected); err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	var delta json.RawMessage
	if baseMode != "full" {
		text = ""
	} else if !hasSpans && text == "" {
		// Another tool's bundle gives the text as runs, as a page's block does.
		if text, delta, err = workspaceImportArchiveBlockText(db, value.ID); err != nil {
			return workspaceImportStagedItem{}, err
		}
	} else if hasSpans {
		encodedSpans, err := json.Marshal(spansValue)
		if err != nil {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		spans := []graphTextSpan{}
		if json.Unmarshal(encodedSpans, &spans) != nil {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		if deltas := notionGraphTextSpansDelta(spans); len(deltas) > 0 {
			if delta, err = json.Marshal(deltas); err != nil {
				return workspaceImportStagedItem{}, workspaceImportInputError()
			}
			if text == "" {
				for _, span := range spans {
					text += span.Text
				}
			}
		}
	}
	return workspaceImportStagedItem{PageID: rowID, Text: text, TextDelta: delta, Start: start, Operation: &graphOperation{
		Kind:  graphOperationKindUpsertBlock,
		Block: &graphBlock{ID: id, ParentID: parent, AfterID: afterID, Position: value.Position, Type: value.Type, Content: content},
	}}, nil
}

func validWorkspaceImportArchiveRowBlockID(value string) bool {
	return validWorkspaceImportArchiveGraphID(value)
}

// validWorkspaceImportArchiveGraphID is any graph identifier an archive
// carries.
func validWorkspaceImportArchiveGraphID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maxGraphIdentifierLength
}

func validWorkspaceImportArchiveOptionalRowBlockID(value string) bool {
	return value == "" || validWorkspaceImportArchiveRowBlockID(value)
}

func appendWorkspaceImportArchiveRowBlocks(w *workspaceImportArchiveChunkWriter, db *sql.DB, kind string) error {
	if kind != workspaceExportItemRowBlockTrash {
		return workspaceImportInputError()
	}
	rows, err := db.Query(`SELECT value_json FROM entities WHERE kind = ? ORDER BY page_id, sequence`, kind)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		item, err := workspaceImportArchiveRowBlockTrashItem(db, encoded)
		if err != nil {
			return err
		}
		if err := w.append(item); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

func (r *durableJobRunner) applyWorkspaceArchiveRowPageBlocks(job storedJob, payload workspaceImportJobPayload, chunk workspaceImportStagedChunk, nextOffset int64) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, currentPayload, authorization, err := loadWorkspaceImportRowBlockTurn(txApp, job, payload)
		if err != nil {
			return err
		}
		// An archived database page body becomes the row page's body, one
		// part per chunk.
		bodies := map[string][]newRowPageBlock{}
		order := []string{}
		for _, item := range chunk.Items {
			block := item.RowPageBlock
			if block == nil || item.PageID == "" || block.ID == "" || block.RowID == "" {
				return workspaceImportStateError()
			}
			if err := validateWorkspaceImportRowBlockOwner(txApp, current, authorization, item.PageID, block.RowID); err != nil {
				return err
			}
			content := map[string]any{}
			if json.Unmarshal(block.Content, &content) != nil {
				return workspaceImportInputError()
			}
			if _, started := bodies[block.RowID]; !started {
				order = append(order, block.RowID)
			}
			bodies[block.RowID] = append(bodies[block.RowID], newRowPageBlock{
				ID: block.ID, ParentID: block.ParentID, Position: block.Position, Type: block.Type,
				Content: content, Created: block.CreatedAt, Updated: block.UpdatedAt,
			})
		}
		now := r.clock.now().UnixMilli()
		for _, rowID := range order {
			if err := writeImportedRowPageBody(txApp, rowID, current.Owner, bodies[rowID], now); err != nil {
				return err
			}
		}
		return finishWorkspaceImportRowBlockTurn(r, txApp, current, currentPayload, chunk, nextOffset)
	})
}

func loadWorkspaceImportRowBlockTurn(app core.App, job storedJob, payload workspaceImportJobPayload) (storedJob, workspaceImportJobPayload, workspaceAuthorization, error) {
	current, err := loadStoredJob(app, job.ID, job.Owner)
	if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
		return storedJob{}, workspaceImportJobPayload{}, workspaceAuthorization{}, err
	}
	currentPayload, err := decodeWorkspaceImportPayload(current)
	if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex || currentPayload.ItemOffset != 0 {
		if err == nil {
			err = workspaceImportStateError()
		}
		return storedJob{}, workspaceImportJobPayload{}, workspaceAuthorization{}, err
	}
	authorization, err := authorizeWorkspace(app, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
	return current, currentPayload, authorization, err
}

func validateWorkspaceImportRowBlockOwner(app core.App, job storedJob, authorization workspaceAuthorization, pageID, rowID string) error {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]] ON [[owned]].[[page_id]]=[[row]].[[page_id]]
		WHERE [[owned]].[[job_id]]={:jobId} AND [[row]].[[id]]={:rowId} AND [[row]].[[page_id]]={:pageId}
			AND [[row]].[[workspace_id]]={:workspaceId} AND [[row]].[[owner]]={:ownerId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "rowId": rowID, "pageId": pageID,
		"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
	}).Row(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return workspaceImportStateErrorAt("archive-apply-row-block-owner")
	}
	return nil
}

func finishWorkspaceImportRowBlockTurn(r *durableJobRunner, app core.App, current storedJob, payload workspaceImportJobPayload, chunk workspaceImportStagedChunk, nextOffset int64) error {
	payload.SourceVerified = true
	payload.ItemOffset = 0
	payload.ChunkIndex++
	payload.ReadyToPublish = chunk.Last
	payload.Phase = workspaceImportPhaseApply
	return r.updateWorkspaceImportPayload(
		app, current, payload, strconv.FormatInt(nextOffset, 10),
		current.Processed+len(chunk.Items), true,
	)
}
