package app

import (
	"database/sql"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func appendWorkspaceImportArchiveTemplates(
	w *workspaceImportArchiveChunkWriter,
	db *sql.DB,
) error {
	rows, err := db.Query(`
		SELECT value_json FROM entities WHERE kind = ? ORDER BY sequence
	`, workspaceExportItemTemplate)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		item, err := workspaceImportArchiveTemplateItem(db, encoded)
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

func workspaceImportArchiveTemplateItem(
	db *sql.DB,
	encoded string,
) (workspaceImportStagedItem, error) {
	value := archiveTemplateValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemTemplate)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	pageID, err := workspaceImportArchiveHostReference(db, value.PageID)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	dataSourceID, err := workspaceImportArchiveReference(db, value.DataSourceID, workspaceExportItemDataSource)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	icon, err := workspaceImportArchiveTemplateIcon(db, value.Icon, value.CustomEmoji)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	properties, err := rewriteWorkspaceImportArchiveTemplateProperties(db, value.Properties)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	spans, err := rewriteWorkspaceImportArchiveTemplateSpans(db, value.PropertySpans)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	blocks, err := rewriteWorkspaceImportArchiveJSON(db, value.Blocks)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	return workspaceImportStagedItem{
		PageID: pageID,
		Template: &workspaceImportStagedTemplate{
			ID: id, PageID: pageID, DataSourceID: dataSourceID,
			Name: value.Name, Icon: icon, Title: value.Title,
			Properties: properties, PropertySpans: spans, Blocks: blocks, IsDefault: value.IsDefault,
			Position: value.Position, CreatedAt: value.CreatedAt,
		},
	}, nil
}

// workspaceImportArchiveTemplateIcon is an archived template's icon here. A
// template's icon is one value of any kind its picker sets — a built-in icon
// ("name:color"), an emoji, a workspace emoji ("custom_emoji:<id>"), a
// picture's address — read by its form (storedIconType on the screen, the
// template page triggers for its page), so it comes back as it was.
// A workspace emoji names the imported emoji, as a page's icon does, or
// shows its picture where the emoji didn't come with it
// (workspace_archive_custom_emoji_icon.go). An uploaded picture, which the
// archive carries as a file, shows the imported file at its address, the
// form read as a picture. A file that didn't come with it is no icon, as a
// teamspace's picture (withoutHeldIconFile).
func workspaceImportArchiveTemplateIcon(
	db *sql.DB, icon string, customEmoji *archiveCustomEmojiPicture,
) (string, error) {
	if id, custom := databaseTemplateCustomEmojiID(icon); custom {
		target, iconType, err := workspaceImportArchiveCustomEmojiIcon(db, id, customEmoji)
		switch {
		case err != nil:
			return "", err
		case iconType == "custom_emoji":
			return databaseTemplateCustomEmojiPrefix + target, nil
		case iconType == "image":
			return productFilePresentationPrefix + target, nil
		}
		return "", nil
	}
	if strings.HasPrefix(icon, workspaceImportArchiveFileMarkerPrefix) {
		fileID, err := workspaceImportArchiveMarkedFileReference(db, icon)
		if err != nil {
			return "", err
		}
		return productFilePresentationPrefix + fileID, nil
	}
	if len(icon) > maxDatabaseTemplateIconBytes || !utf8.ValidString(icon) {
		return "", workspaceImportInputError()
	}
	if _, managed := productFilePresentationID(icon); managed {
		return "", nil
	}
	return icon, nil
}

// rewriteWorkspaceImportArchiveTemplateSpans keys a template's formatting by
// the imported properties, as its values are; formatting of a property the
// archive doesn't hold is left out.
func rewriteWorkspaceImportArchiveTemplateSpans(db *sql.DB, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	spans := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &spans) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten := make(map[string]json.RawMessage, len(spans))
	for sourceID, value := range spans {
		targetID, err := workspaceImportArchiveReference(db, sourceID, workspaceExportItemProperty)
		if err != nil {
			continue
		}
		targetValue, err := rewriteWorkspaceImportArchiveJSON(db, value)
		if err != nil {
			return nil, err
		}
		rewritten[targetID] = targetValue
	}
	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// rewriteWorkspaceImportArchiveTemplateProperties keys a template's values
// by the imported properties; the value of a property the archive doesn't
// hold is left out.
func rewriteWorkspaceImportArchiveTemplateProperties(
	db *sql.DB,
	raw json.RawMessage,
) (json.RawMessage, error) {
	properties := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &properties) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten := make(map[string]json.RawMessage, len(properties))
	for sourceID, value := range properties {
		targetID, err := workspaceImportArchiveReference(db, sourceID, workspaceExportItemProperty)
		if err != nil {
			continue
		}
		targetValue, err := rewriteWorkspaceImportArchiveJSON(db, value)
		if err != nil {
			return nil, err
		}
		rewritten[targetID] = targetValue
	}
	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	return encoded, nil
}
