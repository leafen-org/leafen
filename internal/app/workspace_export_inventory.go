package app

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

var workspaceExportFileRecordIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{15}$`)

const (
	workspaceExportItemPage              = "page"
	workspaceExportItemBlock             = "block"
	workspaceExportItemDataSource        = "dataSource"
	workspaceExportItemView              = "view"
	workspaceExportItemProperty          = "property"
	workspaceExportItemRow               = "row"
	workspaceExportItemYgo               = "ygoDocument"
	workspaceExportItemComment           = "comment"
	workspaceExportItemTemplate          = "template"
	workspaceExportItemDatabaseContainer = "databaseContainer"
	workspaceExportItemBlockTrash        = "blockTrash"
	workspaceExportItemCustomEmoji       = "customEmoji"
	workspaceExportItemRowPageBlock      = "rowPageBlock"
	workspaceExportItemRowBlockTrash     = "rowBlockTrash"
	workspaceExportItemTeamspace         = "teamspace"
)

type storedWorkspaceExportInventoryItem struct {
	Kind    string `db:"kind"`
	SortKey string `db:"sort_key"`
	PageID  string `db:"page_id"`
	Payload string `db:"payload"`
}

type workspaceExportInventoryItem struct {
	Kind  string         `json:"kind"`
	Value map[string]any `json:"value"`
}

type workspaceExportFileSnapshot struct {
	RecordID    string `db:"record_id" json:"recordId"`
	StorageKey  string `db:"storage_key" json:"storageKey"`
	Name        string `db:"name" json:"name"`
	ContentType string `db:"content_type" json:"contentType"`
	Bytes       int64  `db:"bytes" json:"bytes"`
	SHA256      string `db:"sha256" json:"sha256"`
	Scope       string `db:"scope" json:"scope"`
}

type workspaceExportInventoryChunk struct {
	Items []workspaceExportInventoryItem `json:"items"`
	Files []workspaceExportFileSnapshot  `json:"files"`
	// OmittedFiles counts the files its pages name that are still arriving or gone.
	OmittedFiles int `json:"omittedFiles,omitzero"`
}

func loadWorkspaceExportInventory(
	app core.App,
	authorization workspaceAuthorization,
	cursor string,
	limit int,
) (workspaceExportInventoryChunk, string, bool, error) {
	return loadWorkspaceExportInventoryForJob(
		app, authorization, storedJob{},
		workspaceExportJobPayload{Scope: workspaceExportScope{Kind: workspaceExportScopeWorkspace}},
		cursor, limit,
	)
}

// loadWorkspaceExportInventoryForJob reads the export's next window. An
// archive's window may hold nothing once what names a database out of sight
// is left out (workspace_export_gone_links.go); the one after it goes on.
func loadWorkspaceExportInventoryForJob(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	payload workspaceExportJobPayload,
	cursor string,
	limit int,
) (workspaceExportInventoryChunk, string, bool, error) {
	for {
		chunk, next, done, err := loadWorkspaceExportInventoryWindowForJob(app, authorization, job, payload, cursor, limit)
		if err != nil || done || len(chunk.Items) > 0 || next == cursor {
			return chunk, next, done, err
		}
		cursor = next
	}
}

func loadWorkspaceExportInventoryWindowForJob(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	payload workspaceExportJobPayload,
	cursor string,
	limit int,
) (workspaceExportInventoryChunk, string, bool, error) {
	var stored []storedWorkspaceExportInventoryItem
	var done bool
	var err error
	if payload.Scope.Kind == workspaceExportScopeWorkspace {
		stored, done, err = loadWorkspaceExportInventoryWindow(
			app, authorization, newWorkspaceExportPageFilter(job, payload), cursor, limit,
		)
	} else {
		stored, done, err = loadWorkspaceExportScopedInventoryWindow(app, authorization, job, payload, cursor, limit)
	}
	if err != nil {
		return workspaceExportInventoryChunk{}, "", false, err
	}
	stored, byteBounded, err := materializeWorkspaceExportYgoRowsWithinBudget(app, stored)
	if err != nil {
		return workspaceExportInventoryChunk{}, "", false, err
	}
	if byteBounded {
		done = false
	}
	items := make([]workspaceExportInventoryItem, 0, len(stored))
	rowIDsByPage := map[string][]string{}
	rowDocuments := map[string]*crdt.Doc{}
	for _, row := range stored {
		value := map[string]any{}
		if err := json.Unmarshal([]byte(row.Payload), &value); err != nil {
			return workspaceExportInventoryChunk{}, "", false, err
		}
		if row.Kind == workspaceExportItemRowPageBlock {
			if err := fillWorkspaceExportRowPageBlockText(app, value, rowDocuments); err != nil {
				return workspaceExportInventoryChunk{}, "", false, err
			}
		}
		if row.Kind == workspaceExportItemRow {
			rowID, _ := value["id"].(string)
			rowIDsByPage[row.PageID] = append(rowIDsByPage[row.PageID], rowID)
		}
		items = append(items, workspaceExportInventoryItem{Kind: row.Kind, Value: value})
	}
	if err := attachWorkspaceExportRowValues(app, authorization.OwnerID, items, rowIDsByPage); err != nil {
		return workspaceExportInventoryChunk{}, "", false, err
	}
	archive := payload.Format == workspaceExportFormatNativeArchive || payload.Format == workspaceExportFormatPageNativeArchive
	if items, err = withoutWorkspaceExportGoneLinks(app, authorization.WorkspaceID, archive, items); err != nil {
		return workspaceExportInventoryChunk{}, "", false, err
	}
	if payload.Format == workspaceExportFormatPageNativeArchive {
		if err := attachWorkspaceExportCustomEmojiPictures(app, authorization.WorkspaceID, items); err != nil {
			return workspaceExportInventoryChunk{}, "", false, err
		}
	}
	files, omitted, err := snapshotWorkspaceExportFiles(app, authorization.WorkspaceID, items)
	if err != nil {
		return workspaceExportInventoryChunk{}, "", false, err
	}
	nextCursor := cursor
	if len(stored) > 0 {
		nextCursor = stored[len(stored)-1].SortKey
	}
	return workspaceExportInventoryChunk{Items: items, Files: files, OmittedFiles: omitted}, nextCursor, done, nil
}

func materializeWorkspaceExportYgoRowsWithinBudget(
	app core.App,
	rows []storedWorkspaceExportInventoryItem,
) ([]storedWorkspaceExportInventoryItem, bool, error) {
	estimatedBytes := 0
	for index := range rows {
		if index > 0 && estimatedBytes >= workspaceExportChunkTargetBytes {
			return rows[:index], true, nil
		}
		if rows[index].Kind == workspaceExportItemYgo {
			state, found, err := loadMaterializedYgoDocument(app, rows[index].PageID)
			if err != nil {
				return nil, false, err
			}
			structural, structuralFound, err := loadMaterializedYgoStructuralDocument(app, rows[index].PageID)
			if err != nil {
				return nil, false, err
			}
			value := map[string]any{
				"pageId": rows[index].PageID, "formatVersion": ygoDocumentFormatVersion,
			}
			if found {
				value["stateHex"] = hex.EncodeToString(state.State)
			}
			if structuralFound {
				value["structuralStateHex"] = hex.EncodeToString(structural.State)
			}
			payload, err := json.Marshal(value)
			if err != nil {
				return nil, false, err
			}
			rows[index].Payload = string(payload)
		}
		if rows[index].Kind == workspaceExportItemTemplate {
			payload, err := workspaceExportTemplatePayloadWithBody(app, rows[index].Payload)
			if err != nil {
				return nil, false, err
			}
			rows[index].Payload = payload
		}
		nextBytes := len(rows[index].Kind) + len(rows[index].Payload) + 32
		if index > 0 && estimatedBytes+nextBytes > workspaceExportChunkTargetBytes {
			rows[index].Payload = ""
			return rows[:index], true, nil
		}
		estimatedBytes += nextBytes
	}
	return rows, false, nil
}

func attachWorkspaceExportRowValues(
	app core.App,
	owner string,
	items []workspaceExportInventoryItem,
	rowIDsByPage map[string][]string,
) error {
	valuesByRow := map[string]map[string]any{}
	for pageID, rowIDs := range rowIDsByPage {
		values, err := loadGraphRowValues(app, owner, pageID, rowIDs)
		if err != nil {
			return err
		}
		for _, value := range values {
			decoded := any(nil)
			if err := json.Unmarshal([]byte(value.Value), &decoded); err != nil {
				return err
			}
			if valuesByRow[value.RowID] == nil {
				valuesByRow[value.RowID] = map[string]any{}
			}
			cell := map[string]any{"type": value.Type, "value": decoded}
			// A text value's formatting goes with it, as the archive and the
			// document writers read JSON.
			if spans := databaseStoredSpans(value.Spans); len(spans) > 0 {
				decodedSpans := any(nil)
				if json.Unmarshal([]byte(value.Spans), &decodedSpans) == nil {
					cell["spans"] = decodedSpans
				}
			}
			valuesByRow[value.RowID][value.PropertyID] = cell
		}
	}
	for _, item := range items {
		if item.Kind == workspaceExportItemRow {
			rowID, _ := item.Value["id"].(string)
			item.Value["values"] = valuesByRow[rowID]
			if err := workspaceExportTemplateRowValues(app, item.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotWorkspaceExportFiles(
	app core.App,
	workspaceID string,
	items []workspaceExportInventoryItem,
) ([]workspaceExportFileSnapshot, int, error) {
	references := map[string]string{}
	for _, item := range items {
		if item.Kind == workspaceExportItemCustomEmoji {
			addWorkspaceExportFileReference(references, item.Value["fileId"], "icons")
			continue
		}
		if item.Kind == workspaceExportItemPage {
			if item.Value["iconType"] == "image" {
				addWorkspaceExportPresentationFileReference(references, item.Value["icon"], "icons")
			}
			addWorkspaceExportPresentationFileReference(references, item.Value["cover"], "covers")
		}
		if (item.Kind == workspaceExportItemRow || item.Kind == workspaceExportItemDataSource) &&
			item.Value["iconType"] == "image" {
			addWorkspaceExportPresentationFileReference(references, item.Value["icon"], "icons")
		}
		if item.Kind == workspaceExportItemRow || item.Kind == workspaceExportItemDataSource {
			addWorkspaceExportPresentationFileReference(references, item.Value["cover"], "covers")
		}
		// A template's icon has no kind of its own: an uploaded picture is
		// the one naming a file.
		if item.Kind == workspaceExportItemTemplate {
			addWorkspaceExportPresentationFileReference(references, item.Value["icon"], "icons")
		}
		// A workspace emoji icon's picture, in a page's archive.
		if picture, ok := item.Value["customEmoji"].(map[string]any); ok {
			addWorkspaceExportPresentationFileReference(references, picture["icon"], "icons")
		}
		collectWorkspaceExportFileReferences(item.Value, "", references)
	}
	ids := make([]string, 0, len(references))
	for id := range references {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	incompleteUploads, err := incompleteProductFileUploadIDs(app, ids)
	if err != nil {
		return nil, 0, err
	}
	files := make([]workspaceExportFileSnapshot, 0, len(ids))
	omitted := map[string]struct{}{}
	for _, id := range ids {
		// A file still arriving, or gone since its page was written, stays out,
		// and the finished export says how many.
		if _, incomplete := incompleteUploads[id]; incomplete {
			omitted[id] = struct{}{}
			continue
		}
		record, err := app.FindRecordById(filesCollection, id)
		if errors.Is(err, sql.ErrNoRows) {
			omitted[id] = struct{}{}
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		bytes := int64(record.GetFloat("size"))
		if !workspaceFileRecordBelongsTo(record, workspaceID) || bytes <= 0 {
			return nil, 0, newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
		}
		files = append(files, workspaceExportFileSnapshot{
			RecordID: id, StorageKey: record.BaseFilesPath() + "/" + record.GetString("blob"),
			Name: record.GetString("original_name"), ContentType: record.GetString("content_type"),
			Bytes: bytes, SHA256: record.GetString("sha256"), Scope: references[id],
		})
	}
	// What names them goes too: an archive naming a file it doesn't hold can't be brought back.
	if len(omitted) > 0 {
		for index := range items {
			dropWorkspaceExportOmittedFiles(items[index].Value, "", omitted)
		}
	}
	return files, len(omitted), nil
}

// dropWorkspaceExportOmittedFiles empties what names a left-out file, in the
// fields snapshotWorkspaceExportFiles reads its files from.
func dropWorkspaceExportOmittedFiles(value any, field string, omitted map[string]struct{}) any {
	switch typed := value.(type) {
	case string:
		id := strings.TrimSpace(typed)
		if field == "icon" || field == "cover" {
			id, _ = productFilePresentationID(typed)
		} else if field != "fileId" && field != "uploadId" && field != "fileUploadId" && field != "attachmentIds" {
			return value
		}
		if _, gone := omitted[id]; gone {
			return ""
		}
	case []any:
		kept := typed[:0]
		for _, child := range typed {
			next := dropWorkspaceExportOmittedFiles(child, field, omitted)
			if text, isText := next.(string); isText && text == "" && child != "" {
				continue
			}
			kept = append(kept, next)
		}
		return kept
	case map[string]any:
		_, namedFile := typed["fileId"].(string)
		for key, child := range typed {
			typed[key] = dropWorkspaceExportOmittedFiles(child, key, omitted)
		}
		if typed["iconType"] == "image" && typed["icon"] == "" {
			typed["iconType"] = "none"
		}
		// A block whose file stays out keeps its caption and is an empty one.
		if namedFile && typed["fileId"] == "" {
			for key := range typed {
				if key != "caption" {
					delete(typed, key)
				}
			}
		}
	}
	return value
}

func addWorkspaceExportFileReference(references map[string]string, value any, scope string) {
	id, ok := value.(string)
	if !ok || !workspaceExportFileRecordIDPattern.MatchString(strings.TrimSpace(id)) {
		return
	}
	if prior, exists := references[id]; exists && prior != scope {
		references[id] = "uploads"
		return
	}
	references[id] = scope
}

// addWorkspaceExportPresentationFileReference takes an icon's or a cover's
// file in any form it is kept in — its ID or the path that serves it — as
// the archive writes it (rewriteWorkspaceExportReferencesV2), so the file
// the archive points at travels with it.
func addWorkspaceExportPresentationFileReference(references map[string]string, value any, scope string) {
	text, ok := value.(string)
	if !ok {
		return
	}
	if fileID, managed := productFilePresentationID(text); managed {
		addWorkspaceExportFileReference(references, fileID, scope)
	}
}

func collectWorkspaceExportFileReferences(value any, field string, references map[string]string) {
	switch typed := value.(type) {
	case string:
		if field == "fileId" || field == "uploadId" || field == "fileUploadId" || field == "attachmentIds" {
			addWorkspaceExportFileReference(references, typed, "uploads")
		}
	case []any:
		for _, child := range typed {
			collectWorkspaceExportFileReferences(child, field, references)
		}
	case map[string]any:
		// A callout's uploaded icon, or its workspace emoji's picture,
		// wherever a block is carried: a page's blocks, a trashed block's
		// snapshot, a template's body.
		if typed["type"] == graphBlockTypeCallout {
			if content, ok := typed["content"].(map[string]any); ok &&
				(content["iconType"] == graphCalloutIconTypeImage || content["iconType"] == "custom_emoji") {
				addWorkspaceExportPresentationFileReference(references, content["icon"], "icons")
			}
		}
		for key, child := range typed {
			collectWorkspaceExportFileReferences(child, key, references)
		}
	}
}

func workspaceExportRevision(app core.App, workspaceID string) (int64, int, error) {
	var summary struct {
		Revision  int64 `db:"revision"`
		ItemCount int   `db:"item_count"`
	}
	err := app.DB().NewQuery(`
		SELECT [[revision]], [[item_count]] FROM {{leafen_workspace_content_revisions}}
		WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).One(&summary)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("%s: missing workspace revision", errorCodeWorkspaceExportSourceChanged)
	}
	return summary.Revision, summary.ItemCount, err
}
