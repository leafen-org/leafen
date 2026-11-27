package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	recoverableBlockTrashRetention = 30 * 24 * time.Hour
	recoverableBlockTrashPurgeSize = 100
)

type recoverableBlockTrashSnapshot struct {
	Format int                          `json:"format"`
	RootID string                       `json:"rootId"`
	Blocks []recoverableBlockTrashBlock `json:"blocks"`
	// Views are the views owned by linked databases inside the trashed tree.
	Views []graphDatabaseView `json:"views,omitempty"`
}

type recoverableBlockTrashBlock struct {
	ID       string          `json:"id"`
	ParentID string          `json:"parentId"`
	AfterID  string          `json:"afterId"`
	Position string          `json:"position"`
	Type     string          `json:"type"`
	Content  json.RawMessage `json:"content"`
	Text     []crdt.Delta    `json:"text,omitempty"`
	// Who made the block and when, which a read of it in the trash names
	// (notion_trash_reads.go); a tombstone from before has neither.
	CreatedBy string `json:"createdBy,omitempty"`
	Created   int64  `json:"created,omitempty"`
}

type storedRecoverableBlockTrash struct {
	RootBlockID   string `db:"root_block_id"`
	OwnerID       string `db:"owner_id"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	ParentID      string `db:"parent_id"`
	PredecessorID string `db:"predecessor_id"`
	SuccessorID   string `db:"successor_id"`
	SnapshotJSON  string `db:"snapshot_json"`
	BlockCount    int    `db:"block_count"`
	ByteSize      int    `db:"byte_size"`
	DeletedBy     string `db:"deleted_by"`
	DeletedAt     int64  `db:"deleted_at"`
	ExpiresAt     int64  `db:"expires_at"`
}

func captureRecoverableBlockTrash(
	rootBlockID, actorID string,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return captureRecoverableBlockTrashEditing(rootBlockID, actorID, true)
}

// captureRecoverableDatabaseTrash moves an inline database's block to the
// trash through the database's own trash status, which leaves the database's
// last edit as it was (persistRecoverableBlockTrash).
func captureRecoverableDatabaseTrash(
	rootBlockID, actorID string,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return captureRecoverableBlockTrashEditing(rootBlockID, actorID, false)
}

func captureRecoverableBlockTrashEditing(
	rootBlockID, actorID string, editsDatabase bool,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(
		app core.App,
		authorization pageAuthorization,
		structural *crdt.Doc,
		operations []graphOperation,
	) error {
		if len(operations) != 1 || operations[0].Kind != graphOperationKindDeleteBlock ||
			operations[0].Deletion == nil || operations[0].Deletion.BlockID != rootBlockID {
			return graphInputError(errorCodeGraphInvalid, "Check the recoverable block deletion request.")
		}
		return persistRecoverableBlockTrash(
			app, authorization, structural, rootBlockID, actorID, nil, nil, editsDatabase,
		)
	}
}

func captureRecoverableBlockTrashAfterUpdate(
	rootBlockID, actorID string,
	updatedBlock *graphBlock,
	preparedTextSpans func() *[]graphTextSpan,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(
		app core.App,
		authorization pageAuthorization,
		structural *crdt.Doc,
		operations []graphOperation,
	) error {
		if len(operations) != 2 || operations[0].Kind != graphOperationKindUpsertBlock ||
			operations[0].Block != updatedBlock || updatedBlock == nil || updatedBlock.ID != rootBlockID ||
			operations[1].Kind != graphOperationKindDeleteBlock || operations[1].Deletion == nil ||
			operations[1].Deletion.BlockID != rootBlockID {
			return graphInputError(errorCodeGraphInvalid, "Check the recoverable block edit or deletion request.")
		}
		return persistRecoverableBlockTrash(
			app, authorization, structural, rootBlockID, actorID,
			updatedBlock, preparedTextSpans(), true,
		)
	}
}

func persistRecoverableBlockTrash(
	app core.App,
	authorization pageAuthorization,
	structural *crdt.Doc,
	rootBlockID, actorID string,
	updatedBlock *graphBlock,
	updatedTextSpans *[]graphTextSpan,
	editsDatabase bool,
) error {
	if err := purgeExpiredRecoverableBlockTrash(app, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}
	snapshot, parentID, predecessorID, successorID, err :=
		buildRecoverableBlockTrashSnapshot(app, authorization.Page.Id, structural, rootBlockID)
	if err != nil {
		return err
	}
	if updatedBlock != nil {
		if err := applyRecoverableBlockTrashUpdate(
			&snapshot, parentID, updatedBlock, updatedTextSpans,
		); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(encoded) == 0 || len(encoded) > maxYgoUpdateBytes {
		return graphInputError(
			errorCodeGraphChangeTooLarge,
			"These blocks are too large to delete in one change (64 MB with their text). Delete the blocks inside in parts first.",
		)
	}
	now := time.Now().UTC()
	deletedAt := now.UnixMilli()
	expiresAt := now.Add(recoverableBlockTrashRetention).UnixMilli()
	_, err = app.DB().NewQuery(`
			INSERT INTO {{leafen_block_trash_tombstones}}
				([[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]],[[parent_id]],
				 [[predecessor_id]],[[successor_id]],[[snapshot_json]],[[block_count]],[[byte_size]],
				 [[deleted_by]],[[deleted_at]],[[expires_at]])
			VALUES
				({:rootBlockId},{:ownerId},{:workspaceId},{:pageId},{:parentId},
				 {:predecessorId},{:successorId},{:snapshotJSON},{:blockCount},{:byteSize},
				 {:deletedBy},{:deletedAt},{:expiresAt})
	`).Bind(dbx.Params{
		"rootBlockId":   rootBlockID,
		"ownerId":       authorization.Workspace.OwnerID,
		"workspaceId":   authorization.Workspace.WorkspaceID,
		"pageId":        authorization.Page.Id,
		"parentId":      parentID,
		"predecessorId": predecessorID,
		"successorId":   successorID,
		"snapshotJSON":  string(encoded),
		"blockCount":    len(snapshot.Blocks),
		"byteSize":      len(encoded),
		"deletedBy":     actorID,
		"deletedAt":     deletedAt,
		"expiresAt":     expiresAt,
	}).Execute()
	if err == nil {
		err = markRecoverableDatabaseContainerTrash(
			app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
			authorization.Page.Id, actorID, snapshot, deletedAt, expiresAt,
		)
	}
	// Deleting a source database's block is an edit of that database, where
	// the trash places it (deleted_database_containers.go): Notion reads a
	// database's last_edited_time at its block's deletion, through the API or
	// the block menu, but keeps it when the database's own in_trash is set,
	// and its trash lists each at that time (seen 2026-11-13).
	if err == nil && editsDatabase {
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_database_containers}} SET [[updated]] = {:deletedAt}
			WHERE [[id]] = {:rootBlockId} AND [[kind]] = 'source' AND [[updated]] < {:deletedAt}
		`).Bind(dbx.Params{"rootBlockId": rootBlockID, "deletedAt": deletedAt}).Execute()
	}
	if err == nil {
		files, fileErr := recoverableBlockTrashManagedFiles(snapshot)
		if fileErr != nil {
			return fileErr
		}
		err = insertProductFileReferences(
			app, authorization.Workspace.WorkspaceID, authorization.Page.Id,
			productFileReferenceOwnerBlockTrash, rootBlockID, "", files,
		)
	}
	if err == nil {
		notifyRecoverableBlockTrashRetentionAfterCommit(app)
	}
	return err

}

func applyRecoverableBlockTrashUpdate(
	snapshot *recoverableBlockTrashSnapshot,
	parentID string,
	updatedBlock *graphBlock,
	updatedTextSpans *[]graphTextSpan,
) error {
	if snapshot == nil || len(snapshot.Blocks) == 0 || updatedBlock == nil ||
		snapshot.Blocks[0].ID != snapshot.RootID || updatedBlock.ID != snapshot.RootID {
		return graphInputError(errorCodeGraphInvalid, "Check the recoverable block edit details.")
	}
	updatedParentID := ""
	if updatedBlock.ParentID != nil {
		updatedParentID = *updatedBlock.ParentID
	}
	root := &snapshot.Blocks[0]
	if updatedParentID != parentID || updatedParentID != root.ParentID ||
		updatedBlock.Type != root.Type {
		return graphInputError(errorCodeGraphInvalid, "Check the recoverable block edit range.")
	}
	root.Content = append(json.RawMessage(nil), updatedBlock.Content...)
	if updatedTextSpans != nil {
		root.Text = notionGraphTextSpansDelta(*updatedTextSpans)
	}
	if recoverableBlockTrashUsesIndependentLifecycle(*root) {
		return graphInputError(
			errorCodeGraphInvalid,
			"Database and child-page blocks require their own deletion lifecycle.",
		)
	}
	return nil
}

func notionGraphTextSpansDelta(spans []graphTextSpan) []crdt.Delta {
	result := make([]crdt.Delta, 0, len(spans))
	for index, attributes := range graphTextSpansAttributes(spans) {
		if attributes == nil {
			continue
		}
		var insert any = spans[index].Text
		if graphTextEmptyPieceSpan(spans[index]) {
			attributes[graphTextPieceKey] = nil
			insert = graphTextEmptyPieceEmbed(spans[index].EmptyPiece)
		}
		result = append(result, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: insert, Attributes: attributes})
	}
	return result
}

func buildRecoverableBlockTrashSnapshot(
	app core.App,
	pageID string,
	structural *crdt.Doc,
	rootBlockID string,
) (recoverableBlockTrashSnapshot, string, string, string, error) {
	metadata, err := structuralMetadata(structural)
	if err != nil {
		return recoverableBlockTrashSnapshot{}, "", "", "", err
	}
	root, exists := metadata[rootBlockID]
	if !exists {
		return recoverableBlockTrashSnapshot{}, "", "", "", graphNotFoundError()
	}
	rootOrder, err := structuralOrder(structural, root.ParentID)
	if err != nil {
		return recoverableBlockTrashSnapshot{}, "", "", "", err
	}
	rootIndex := structuralIndex(rootOrder, rootBlockID)
	if rootIndex < 0 {
		return recoverableBlockTrashSnapshot{}, "", "", "", graphStructureAuthorityError()
	}
	predecessorID, successorID := "", ""
	if rootIndex > 0 {
		predecessorID = rootOrder[rootIndex-1]
	}
	if rootIndex+1 < len(rootOrder) {
		successorID = rootOrder[rootIndex+1]
	}

	textDocument, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return recoverableBlockTrashSnapshot{}, "", "", "", err
	}

	snapshot := recoverableBlockTrashSnapshot{Format: 1, RootID: rootBlockID}
	var walk func(string) error
	walk = func(id string) error {
		entry, ok := metadata[id]
		if !ok {
			return graphStructureAuthorityError()
		}
		order, err := structuralOrder(structural, entry.ParentID)
		if err != nil {
			return err
		}
		index := structuralIndex(order, id)
		if index < 0 {
			return graphStructureAuthorityError()
		}
		afterID := ""
		if index > 0 {
			afterID = order[index-1]
		}
		content, err := json.Marshal(entry.Content)
		if err != nil {
			return err
		}
		block := recoverableBlockTrashBlock{
			ID: id, ParentID: entry.ParentID, AfterID: afterID,
			Position: fmt.Sprintf("p%010d", index), Type: entry.Type,
			Content: content,
			Text:    textDocument.GetText(ygoBlockTextRootPrefix + id).ToDelta(),
		}
		if recoverableBlockTrashUsesIndependentLifecycle(block) {
			return graphInputError(
				errorCodeGraphInvalid,
				"Database and child-page blocks require their own deletion lifecycle.",
			)
		}
		snapshot.Blocks = append(snapshot.Blocks, block)
		children, err := structuralOrder(structural, id)
		if err != nil {
			return err
		}
		for _, childID := range children {
			if err := walk(childID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootBlockID); err != nil {
		return recoverableBlockTrashSnapshot{}, "", "", "", err
	}
	if err := recordRecoverableBlockTrashMakers(app, pageID, &snapshot); err != nil {
		return recoverableBlockTrashSnapshot{}, "", "", "", err
	}
	if snapshot.Views, err = recoverableLinkedDatabaseViews(app, pageID, snapshot.Blocks); err != nil {
		return recoverableBlockTrashSnapshot{}, "", "", "", err
	}
	return snapshot, root.ParentID, predecessorID, successorID, nil
}

func recoverableBlockTrashUsesIndependentLifecycle(block recoverableBlockTrashBlock) bool {
	switch block.Type {
	case graphBlockTypeInlineDatabase:
		_, _, valid := recoverableDatabaseContainer(block)
		return !valid
	case graphBlockTypeChildPage, graphBlockTypeChildDatabase:
		return true
	}
	return false
}

func recoverableBlockTrashManagedFiles(snapshot recoverableBlockTrashSnapshot) (graphFilesValue, error) {
	files := graphFilesValue{}
	seen := map[string]struct{}{}
	for _, stored := range snapshot.Blocks {
		block := &graphBlock{ID: stored.ID, Type: stored.Type, Content: stored.Content}
		if graphBlockCarriesManagedFileReference(block) {
			if _, err := parseGraphFileBlockContent(stored.Content); err != nil {
				return nil, err
			}
		}
		fileID := graphBlockKeptFileID(block)
		if fileID == "" {
			continue
		}
		if _, duplicate := seen[fileID]; duplicate {
			continue
		}
		seen[fileID] = struct{}{}
		files = append(files, graphFileValueItem{FileID: fileID})
	}
	return files, nil
}

func purgeExpiredRecoverableBlockTrash(app core.App, now int64) error {
	if app.TxInfo() == nil {
		return app.RunInTransaction(func(txApp core.App) error {
			_, err := purgeExpiredRecoverableBlockTrashSlice(txApp, now)
			return err
		})
	}
	_, err := purgeExpiredRecoverableBlockTrashSlice(app, now)
	return err
}

func purgeExpiredRecoverableBlockTrashSlice(app core.App, now int64) (int64, error) {
	databaseRoot, databaseRootFound, err := databaseContainerTrashRootDue(app, now)
	if err != nil {
		return 0, err
	}
	// A block on a page a legal hold keeps stays in the trash while it does.
	selection := `
		SELECT [[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]]
		FROM {{leafen_block_trash_tombstones}}
		WHERE [[expires_at]] <= {:now} AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[database_trash]]
			WHERE [[database_trash]].[[root_block_id]]={{leafen_block_trash_tombstones}}.[[root_block_id]]
		) AND NOT ` + legalHoldRetainsPageSQL(
		"{{leafen_block_trash_tombstones}}.[[page_id]]", "{{leafen_block_trash_tombstones}}.[[workspace_id]]",
	) + `
		ORDER BY [[expires_at]],[[root_block_id]] LIMIT {:limit}`
	limit := recoverableBlockTrashPurgeSize
	params := dbx.Params{
		"now": now, "limit": limit,
		"ownerKind": productFileReferenceOwnerBlockTrash,
	}
	if databaseRootFound {
		if err := deleteRecoverableDatabaseContainerTrashProjections(app, databaseRoot); err != nil {
			return 0, err
		}
		selection = `
			SELECT [[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]]
			FROM {{leafen_block_trash_tombstones}}
			WHERE [[root_block_id]]={:databaseRoot}`
		limit = 1
		params["limit"] = limit
		params["databaseRoot"] = databaseRoot
	}
	expired := []struct {
		RootBlockID string `db:"root_block_id"`
		OwnerID     string `db:"owner_id"`
		WorkspaceID string `db:"workspace_id"`
		PageID      string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(selection).Bind(params).All(&expired); err != nil {
		return 0, err
	}
	affected := []struct {
		WorkspaceID string `db:"workspace_id"`
		FileID      string `db:"file_id"`
	}{}
	if err := app.DB().NewQuery(`
		WITH [[expired]] AS (` + selection + `)
		SELECT DISTINCT [[reference]].[[workspace_id]],[[reference]].[[file_id]]
		FROM [[expired]]
		JOIN {{leafen_file_references}} AS [[reference]]
			ON [[reference]].[[workspace_id]]=[[expired]].[[workspace_id]]
			AND [[reference]].[[page_id]]=[[expired]].[[page_id]]
			AND [[reference]].[[owner_kind]]={:ownerKind}
			AND [[reference]].[[owner_id]]=[[expired]].[[root_block_id]]
	`).Bind(params).All(&affected); err != nil {
		return 0, err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_block_trash_tombstones}}
		WHERE [[root_block_id]] IN (
			SELECT [[root_block_id]] FROM (` + selection + `)
		)
	`).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	byWorkspace := map[string][]string{}
	for _, reference := range affected {
		byWorkspace[reference.WorkspaceID] = append(
			byWorkspace[reference.WorkspaceID], reference.FileID,
		)
	}
	for workspaceID, fileIDs := range byWorkspace {
		if err := reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs); err != nil {
			return 0, err
		}
	}
	return changed, nil
}

func loadRecoverableBlockTrash(
	app core.App, rootBlockID string,
) (storedRecoverableBlockTrash, bool, error) {
	now := time.Now().UTC().UnixMilli()
	if err := purgeExpiredRecoverableBlockTrash(app, now); err != nil {
		return storedRecoverableBlockTrash{}, false, err
	}
	row := storedRecoverableBlockTrash{}
	err := app.DB().NewQuery(`
		SELECT [[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]],[[parent_id]],
			[[predecessor_id]],[[successor_id]],[[snapshot_json]],[[block_count]],[[byte_size]],
			[[deleted_by]],[[deleted_at]],[[expires_at]]
		FROM {{leafen_block_trash_tombstones}}
		WHERE [[root_block_id]]={:rootBlockId} AND [[expires_at]]>{:now}
		LIMIT 2
	`).Bind(dbx.Params{"rootBlockId": rootBlockID, "now": now}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedRecoverableBlockTrash{}, false, nil
	}
	return row, err == nil, err
}

func parseRecoverableBlockTrashSnapshot(
	row storedRecoverableBlockTrash,
) (recoverableBlockTrashSnapshot, error) {
	snapshot := recoverableBlockTrashSnapshot{}
	if len(row.SnapshotJSON) != row.ByteSize || row.BlockCount < 1 ||
		row.ByteSize > maxYgoUpdateBytes ||
		json.Unmarshal([]byte(row.SnapshotJSON), &snapshot) != nil ||
		snapshot.Format != 1 || snapshot.RootID != row.RootBlockID ||
		len(snapshot.Blocks) != row.BlockCount || snapshot.Blocks[0].ID != row.RootBlockID ||
		snapshot.Blocks[0].ParentID != row.ParentID {
		return recoverableBlockTrashSnapshot{}, errors.New("recoverable block trash snapshot is invalid")
	}
	seen := map[string]struct{}{}
	for _, block := range snapshot.Blocks {
		if block.ID == "" || block.Type == "" || len(block.Content) == 0 ||
			recoverableBlockTrashUsesIndependentLifecycle(block) {
			return recoverableBlockTrashSnapshot{}, errors.New("recoverable block trash block is invalid")
		}
		if _, duplicate := seen[block.ID]; duplicate {
			return recoverableBlockTrashSnapshot{}, errors.New("recoverable block trash contains duplicate blocks")
		}
		seen[block.ID] = struct{}{}
		if block.ID != row.RootBlockID {
			if _, parentStored := seen[block.ParentID]; !parentStored {
				return recoverableBlockTrashSnapshot{}, errors.New("recoverable block trash order is invalid")
			}
		}
	}
	return snapshot, nil
}

// recordRecoverableBlockTrashMakers puts who made each block going to the
// trash, and when, with it.
func recordRecoverableBlockTrashMakers(
	app core.App, pageID string, snapshot *recoverableBlockTrashSnapshot,
) error {
	positions := make(map[string]int, len(snapshot.Blocks))
	for position, block := range snapshot.Blocks {
		positions[block.ID] = position
	}
	for start := 0; start < len(snapshot.Blocks); start += 500 {
		end := min(start+500, len(snapshot.Blocks))
		params := dbx.Params{"pageId": pageID}
		placeholders := make([]string, 0, end-start)
		for position := start; position < end; position++ {
			key := "block" + strconv.Itoa(position)
			params[key] = snapshot.Blocks[position].ID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []struct {
			ID        string `db:"id"`
			CreatedBy string `db:"created_by"`
			Created   int64  `db:"created"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[id]],[[created_by]],[[created]] FROM {{leafen_blocks}}
			WHERE [[page_id]]={:pageId} AND [[id]] IN (` + strings.Join(placeholders, ",") + `)
		`).Bind(params).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			if position, found := positions[row.ID]; found {
				snapshot.Blocks[position].CreatedBy = row.CreatedBy
				snapshot.Blocks[position].Created = row.Created
			}
		}
	}
	return nil
}

// validateRecoverableBlockTrashRestore checks the trash entry and the
// restored IDs; anywhere, the tree's place was settled before.
func validateRecoverableBlockTrashRestore(
	row storedRecoverableBlockTrash,
	snapshot recoverableBlockTrashSnapshot,
	anywhere bool,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(
		app core.App,
		authorization pageAuthorization,
		structural *crdt.Doc,
		_ []graphOperation,
	) error {
		current, found, err := loadRecoverableBlockTrash(app, row.RootBlockID)
		if err != nil {
			return err
		}
		if !found || current.SnapshotJSON != row.SnapshotJSON ||
			current.OwnerID != authorization.Workspace.OwnerID ||
			current.WorkspaceID != authorization.Workspace.WorkspaceID ||
			current.PageID != authorization.Page.Id {
			return graphConflictError(errorCodeGraphRevisionConflict, "The block trash entry changed before restore.")
		}
		metadata, err := structuralMetadata(structural)
		if err != nil {
			return err
		}
		for _, block := range snapshot.Blocks {
			if _, exists := metadata[block.ID]; exists {
				return graphConflictError(errorCodeGraphRevisionConflict, "A block ID is already in use.")
			}
		}
		if anywhere {
			return nil
		}
		if row.ParentID != "" {
			if _, exists := metadata[row.ParentID]; !exists {
				return graphConflictError(errorCodeGraphRevisionConflict, "The original block parent no longer exists.")
			}
		}
		order, err := structuralOrder(structural, row.ParentID)
		if err != nil {
			return err
		}
		if !recoverableBlockTrashBoundaryMatches(order, row.PredecessorID, row.SuccessorID) {
			return graphConflictError(errorCodeGraphRevisionConflict, "The original block position has changed.")
		}
		return nil
	}
}

func recoverableBlockTrashBoundaryMatches(order []string, predecessorID, successorID string) bool {
	if predecessorID == "" {
		if successorID == "" {
			return len(order) == 0
		}
		return len(order) > 0 && order[0] == successorID
	}
	index := structuralIndex(order, predecessorID)
	if index < 0 {
		return false
	}
	if successorID == "" {
		return index == len(order)-1
	}
	return index+1 < len(order) && order[index+1] == successorID
}

func applyRecoverableBlockTrashText(
	app core.App,
	pageID, actorID string,
	snapshot recoverableBlockTrashSnapshot,
) (string, error) {
	document, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return "", err
	}
	for _, block := range snapshot.Blocks {
		if document.GetText(ygoBlockTextRootPrefix+block.ID).Len() != 0 {
			return "", graphConflictError(errorCodeGraphTextTargetConflict, "A restored block text ID is already in use.")
		}
	}
	var details crdt.UpdateDetails
	unsubscribe := document.OnUpdateDetails(func(value crdt.UpdateDetails) { details = value })
	document.Transact(func(transaction *crdt.Transaction) {
		for _, block := range snapshot.Blocks {
			if len(block.Text) > 0 {
				transaction.GetText(ygoBlockTextRootPrefix+block.ID).ApplyDelta(transaction, block.Text)
			}
		}
	})
	unsubscribe()
	if len(details.Update) == 0 {
		return "", nil
	}
	if len(details.Update) > maxYgoUpdateBytes {
		return "", graphInputError(errorCodeGraphChangeTooLarge, "The block text to recover is too large for one change (64 MB).")
	}
	if err := appendYgoUpdate(app, pageID, details.Update); err != nil {
		return "", err
	}
	publishStoredYgoUpdateAfterCommit(app, pageID, details.Update)
	if err := recordPageHistoryCollaborationUpdate(
		app, pageID, actorID, details.Update, details.TextChanges,
	); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(details.Update), nil
}

func deleteRecoverableBlockTrash(
	app core.App, rootBlockID, pageID string,
) error {
	references := []struct {
		WorkspaceID string `db:"workspace_id"`
		FileID      string `db:"file_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[workspace_id]],[[file_id]] FROM {{leafen_file_references}}
		WHERE [[page_id]]={:pageId} AND [[owner_kind]]={:ownerKind} AND [[owner_id]]={:rootBlockId}
	`).Bind(dbx.Params{
		"rootBlockId": rootBlockID, "pageId": pageID,
		"ownerKind": productFileReferenceOwnerBlockTrash,
	}).All(&references); err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_block_trash_tombstones}}
		WHERE [[root_block_id]]={:rootBlockId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"rootBlockId": rootBlockID, "pageId": pageID}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return graphConflictError(errorCodeGraphRevisionConflict, "The block trash entry changed before restore.")
	}
	byWorkspace := map[string][]string{}
	for _, reference := range references {
		byWorkspace[reference.WorkspaceID] = append(
			byWorkspace[reference.WorkspaceID], reference.FileID,
		)
	}
	for workspaceID, fileIDs := range byWorkspace {
		if err := reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs); err != nil {
			return err
		}
	}
	return nil
}
