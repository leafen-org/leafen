package app

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func inventoryWorkspaceImportArchiveEntities(
	descriptor workspaceExportArchiveDescriptor,
	entry *zip.File,
	idSalt string,
	mapping *sql.DB,
) error {
	reader, err := entry.Open()
	if err != nil {
		return workspaceImportInputError()
	}
	defer reader.Close()
	hasher := sha256.New()
	buffered := bufio.NewReaderSize(io.TeeReader(reader, hasher), workspaceExportReaderWindowBytes)
	counts := emptyWorkspaceExportCounts()
	sequence := 0
	var tx *sql.Tx
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	for {
		line, readErr := buffered.ReadBytes('\n')
		if len(line) > 0 {
			place := fmt.Sprintf("%s line %d", workspaceExportEntitiesPath, sequence+1)
			if line[len(line)-1] != '\n' || len(line) == 1 || !utf8.Valid(line[:len(line)-1]) {
				return workspaceImportAt(workspaceImportInvalid(workspaceImportArchiveLineShape), place)
			}
			entity := workspaceImportArchiveEntity{}
			if json.Unmarshal(line[:len(line)-1], &entity) != nil || entity.Value == nil {
				return workspaceImportAt(workspaceImportInvalid("not an object with kind and value"), place)
			}
			if _, ok := counts[entity.Kind]; !ok {
				return workspaceImportAt(workspaceImportInvalid("its kind isn't one a bundle holds"), place)
			}
			place += " (" + entity.Kind + ")"
			if tx == nil {
				tx, err = mapping.Begin()
				if err != nil {
					return workspaceImportStorageError(err)
				}
			}
			if err := inventoryWorkspaceImportArchiveFileReferences(tx, "", entity.Value); err != nil {
				return workspaceImportAt(err, place)
			}
			if err := storeWorkspaceImportArchiveEntity(tx, idSalt, sequence, descriptor.FormatVersion, entity); err != nil {
				return workspaceImportAt(err, place)
			}
			counts[entity.Kind]++
			sequence++
			if sequence%workspaceImportMutationTurnItems == 0 {
				if err := tx.Commit(); err != nil {
					return workspaceImportStorageError(err)
				}
				tx = nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return workspaceImportInputError()
		}
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return workspaceImportStorageError(err)
		}
		tx = nil
	}
	stream := descriptor.Streams["entities"]
	if sequence != stream.Records {
		return workspaceImportInvalid(fmt.Sprintf("%s has %d lines; %s's entities stream says %d records",
			workspaceExportEntitiesPath, sequence, workspaceExportDescriptorPath, stream.Records))
	}
	if hex.EncodeToString(hasher.Sum(nil)) != stream.SHA256 {
		return workspaceImportInvalid(fmt.Sprintf("%s's SHA-256 isn't the one %s gives", workspaceExportEntitiesPath, workspaceExportDescriptorPath))
	}
	if counts[workspaceExportItemPage] != descriptor.Counts["pages"] ||
		counts[workspaceExportItemBlock] != descriptor.Counts["blocks"] ||
		counts[workspaceExportItemDataSource] != descriptor.Counts["dataSources"] ||
		counts[workspaceExportItemView] != descriptor.Counts["dbViews"] ||
		counts[workspaceExportItemProperty] != descriptor.Counts["dbProperties"] ||
		counts[workspaceExportItemRow] != descriptor.Counts["databaseRows"] ||
		counts[workspaceExportItemYgo] != descriptor.Counts["ygoDocuments"] ||
		counts[workspaceExportItemTemplate] != descriptor.Counts["dbTemplates"] ||
		counts[workspaceExportItemComment] != descriptor.Counts["comments"] ||
		counts[workspaceExportItemDatabaseContainer] != descriptor.Counts["databaseContainers"] ||
		counts[workspaceExportItemBlockTrash] != descriptor.Counts["blockTrashTombstones"] ||
		counts[workspaceExportItemCustomEmoji] != descriptor.Counts["customEmojis"] ||
		counts[workspaceExportItemRowPageBlock] != descriptor.Counts["rowPageBlocks"] ||
		counts[workspaceExportItemRowBlockTrash] != descriptor.Counts["rowBlockTrashTombstones"] ||
		counts[workspaceExportItemTeamspace] != descriptor.Counts["teamspaces"] ||
		counts[workspaceExportItemDatabaseAutomation] != descriptor.Counts["dbAutomations"] {
		return workspaceImportInvalid(fmt.Sprintf("the lines of each kind in %s aren't the counts %s gives (pages, blocks, …)",
			workspaceExportEntitiesPath, workspaceExportDescriptorPath))
	}
	return nil
}

// workspaceImportArchiveLineShape is what a bundle's lines are.
const workspaceImportArchiveLineShape = "not one JSON object on a line of its own, in UTF-8, ending with a line break"

func storeWorkspaceImportArchiveEntity(
	tx *sql.Tx,
	idSalt string,
	sequence, formatVersion int,
	entity workspaceImportArchiveEntity,
) error {
	nameWorkspaceImportArchiveViewsAsNow(formatVersion, entity)
	if err := readWorkspaceImportArchiveRowTrashAsNow(formatVersion, entity); err != nil {
		return err
	}
	legacyText, textPresent, textOK := workspaceImportArchiveLegacyText(entity.Kind, entity.Value)
	if !textOK {
		return workspaceImportInputError()
	}
	delete(entity.Value, "legacyText")
	authoredText, authoredDeltas, authoredPresent, authoredOK := workspaceImportArchiveAuthoredText(entity.Kind, entity.Value)
	if !authoredOK || (authoredPresent && textPresent) {
		return workspaceImportInputError()
	}
	delete(entity.Value, "text")
	sourceID, pageID, err := workspaceImportArchiveEntityIdentity(entity.Kind, entity.Value)
	if err != nil {
		return err
	}
	if entity.Kind == workspaceExportItemYgo {
		if _, err := tx.Exec(`INSERT INTO ygo_pages (page_id) VALUES (?)`, pageID); err != nil {
			return workspaceImportInputError()
		}
	} else if entity.Kind == workspaceExportItemBlockTrash {
		value, _, err := decodeWorkspaceImportArchiveBlockTrash(entity.Value)
		if err != nil {
			return err
		}
		for _, block := range value.Snapshot.Blocks {
			result, insertErr := tx.Exec(
				`INSERT OR IGNORE INTO mappings (source_id, target_id, kind) VALUES (?, ?, ?)`,
				block.ID, workspaceImportArchiveMappedID(idSalt, entity.Kind, block.ID), entity.Kind,
			)
			if insertErr != nil {
				return workspaceImportInputError()
			}
			inserted, _ := result.RowsAffected()
			if inserted != 1 {
				return workspaceImportInputError()
			}
		}
	} else if entity.Kind == workspaceExportItemRowBlockTrash {
		value, err := decodeWorkspaceImportArchiveRowTrash(entity.Value)
		if err != nil {
			return err
		}
		for _, block := range value.Snapshot.Blocks {
			result, insertErr := tx.Exec(
				`INSERT OR IGNORE INTO mappings (source_id, target_id, kind) VALUES (?, ?, ?)`,
				block.ID, workspaceImportArchiveMappedID(idSalt, workspaceExportItemRowPageBlock, block.ID),
				workspaceExportItemRowPageBlock,
			)
			if insertErr != nil {
				return workspaceImportInputError()
			}
			inserted, _ := result.RowsAffected()
			if inserted != 1 {
				return workspaceImportInputError()
			}
		}
	} else if entity.Kind != workspaceExportItemDatabaseContainer {
		result, insertErr := tx.Exec(
			`INSERT OR IGNORE INTO mappings (source_id, target_id, kind) VALUES (?, ?, ?)`,
			sourceID, workspaceImportArchiveMappedID(idSalt, entity.Kind, sourceID), entity.Kind,
		)
		if insertErr != nil {
			return workspaceImportInputError()
		}
		inserted, _ := result.RowsAffected()
		if inserted != 1 {
			return workspaceImportInputError()
		}
	}
	if textPresent {
		if _, err := tx.Exec(`INSERT INTO texts (block_id, value) VALUES (?, ?)`, sourceID, legacyText); err != nil {
			return workspaceImportInputError()
		}
	}
	if authoredPresent {
		if err := stageWorkspaceImportArchiveAuthoredText(tx, sourceID, authoredText, authoredDeltas); err != nil {
			return workspaceImportInputError()
		}
	}
	parentID, position := "", ""
	if entity.Kind == workspaceExportItemPage {
		// A page keeps its parent so that a database page's sub-pages are
		// found.
		if rawParent, ok := entity.Value["parentId"].(string); ok && len(rawParent) <= maxGraphIdentifierLength {
			parentID = rawParent
		}
	}
	if entity.Kind == workspaceExportItemRowPageBlock {
		// A database page's body block keeps its place so that its body
		// comes back in order.
		rawParent, parentOK := entity.Value["parentId"].(string)
		rawPosition, positionOK := entity.Value["position"].(string)
		if !parentOK || !positionOK || len(rawParent) > maxGraphIdentifierLength || len(rawPosition) > maxGraphPositionLength {
			return workspaceImportInputError()
		}
		parentID, position = rawParent, rawPosition
	}
	if entity.Kind == workspaceExportItemBlock {
		if rawParent := entity.Value["parentId"]; rawParent != nil {
			var ok bool
			parentID, ok = rawParent.(string)
			if !ok || strings.TrimSpace(parentID) != parentID || len(parentID) > maxGraphIdentifierLength {
				return workspaceImportInputError()
			}
		}
		var ok bool
		position, ok = entity.Value["position"].(string)
		if !ok || strings.TrimSpace(position) != position || position == "" || len(position) > maxGraphPositionLength {
			return workspaceImportInputError()
		}
	}
	encoded, err := json.Marshal(entity.Value)
	if err != nil {
		return workspaceImportInputError()
	}
	if _, err := tx.Exec(
		`INSERT INTO entities (sequence, kind, source_id, page_id, parent_id, position, value_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sequence, entity.Kind, sourceID, pageID, parentID, position, string(encoded),
	); err != nil {
		return workspaceImportInputError()
	}
	return nil
}

func workspaceImportArchiveLegacyText(kind string, value map[string]any) (string, bool, bool) {
	raw, exists := value["legacyText"]
	if !exists {
		return "", false, true
	}
	text, ok := raw.(string)
	return text, kind == workspaceExportItemBlock && text != "", ok && len(text) <= workspaceImportArchiveEntityJSONBytes
}

func workspaceImportArchiveEntityIdentity(kind string, value map[string]any) (string, string, error) {
	key := "id"
	if kind == workspaceExportItemYgo {
		key = "pageId"
	} else if kind == workspaceExportItemBlockTrash || kind == workspaceExportItemRowBlockTrash {
		key = "rootBlockId"
	}
	sourceID, _ := value[key].(string)
	pageID, _ := value["pageId"].(string)
	if kind == workspaceExportItemPage {
		pageID = sourceID
	} else if kind == workspaceExportItemCustomEmoji || kind == workspaceExportItemTeamspace {
		pageID = sourceID
	}
	if strings.TrimSpace(sourceID) != sourceID || sourceID == "" || len(sourceID) > maxGraphIdentifierLength ||
		strings.TrimSpace(pageID) != pageID || pageID == "" || len(pageID) > maxGraphIdentifierLength {
		return "", "", workspaceImportInputError()
	}
	return sourceID, pageID, nil
}
