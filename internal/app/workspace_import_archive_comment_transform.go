package app

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

func appendWorkspaceImportArchiveComments(
	w *workspaceImportArchiveChunkWriter,
	db *sql.DB,
) error {
	rows, err := db.Query(`
		SELECT value_json FROM entities WHERE kind = ? ORDER BY sequence
	`, workspaceExportItemComment)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		item, err := workspaceImportArchiveCommentItem(db, encoded)
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

func workspaceImportArchiveCommentItem(
	db *sql.DB,
	encoded string,
) (workspaceImportStagedItem, error) {
	value := archiveCommentValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemComment)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	pageID, err := workspaceImportArchiveHostReference(db, value.PageID)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	targetKind := value.TargetKind
	if targetKind == "" {
		targetKind = commentTargetPage
	}
	targetID := value.TargetID
	if targetID == "" && targetKind == commentTargetPage {
		targetID = value.PageID
	}
	switch targetKind {
	case commentTargetPage:
		targetID, err = workspaceImportArchiveReference(db, targetID, workspaceExportItemPage)
	case commentTargetDatabaseRow:
		targetID, err = workspaceImportArchiveReference(db, targetID, workspaceExportItemRow)
	default:
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	blockID := ""
	if value.BlockID != "" {
		blockKind := workspaceExportItemBlock
		if targetKind == commentTargetDatabaseRow {
			blockKind = workspaceExportItemRowPageBlock
		}
		blockID, err = workspaceImportArchiveReference(db, value.BlockID, blockKind)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	parentID := ""
	if value.ParentID != "" {
		parentID, err = workspaceImportArchiveReference(db, value.ParentID, workspaceExportItemComment)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	body, err := rewriteWorkspaceImportArchiveJSON(db, value.Body)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	if author := strings.TrimSpace(value.Author); author != "" {
		if body, err = withCommentAuthorLine(body, author, value.CreatedAt, workspaceImportArchiveClock(db)); err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	// A moved workspace's comments keep their authors and reactions where
	// the people were paired (workspace_import_archive_people.go).
	authorID, err := workspaceImportArchiveAccount(db, value.AuthorID)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	if err := workspaceImportArchiveReactions(db, value.Reactions); err != nil {
		return workspaceImportStagedItem{}, err
	}
	return workspaceImportStagedItem{
		PageID: pageID,
		Comment: &workspaceImportStagedComment{
			ID: id, PageID: pageID, TargetKind: targetKind, TargetID: targetID,
			BlockID: blockID, ParentID: parentID,
			Body: body, Resolved: value.Resolved,
			CreatedAt: value.CreatedAt, EditedAt: value.EditedAt,
			Reactions: value.Reactions, ThreadState: value.ThreadState,
			AuthorID: authorID,
		},
	}, nil
}

// withCommentAuthorLine opens a comment another tool's person wrote with
// their name and when, as a moved Notion comment opens.
func withCommentAuthorLine(body json.RawMessage, author, written string, location *time.Location) (json.RawMessage, error) {
	if utf8.RuneCountInString(author) > 255 {
		return nil, workspaceImportInvalid("a comment's author is longer than 255 characters")
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(body, &fields) != nil {
		return nil, workspaceImportInvalid("a comment's body isn't an object")
	}
	runs := []json.RawMessage{}
	if rich, found := fields["rich"]; found && json.Unmarshal(rich, &runs) != nil {
		return nil, workspaceImportInvalid("a comment's rich isn't a list of runs")
	}
	line, err := json.Marshal(map[string]string{"text": commentAuthorLine(author, written, location) + "\n"})
	if err != nil {
		return nil, err
	}
	if fields["rich"], err = json.Marshal(append([]json.RawMessage{line}, runs...)); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
