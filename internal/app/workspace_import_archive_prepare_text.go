package app

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/reearth/ygo/crdt"
)

func (r *durableJobRunner) materializeWorkspaceArchiveTextsTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
) error {
	tx, err := db.Begin()
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-text-map-begin")
	}
	defer tx.Rollback()
	var ygoSequence int
	var encoded string
	err = tx.QueryRow(`
		SELECT sequence, value_json FROM entities
		WHERE kind = ? AND sequence > ? ORDER BY sequence LIMIT 1
	`, workspaceExportItemYgo, state.TextYgoSequence).Scan(&ygoSequence, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		if err := validateWorkspaceImportArchiveReferences(tx); err != nil {
			return workspaceImportAt(err, workspaceExportEntitiesPath+": a reference")
		}
		state.Phase = workspaceImportArchivePreparePhaseTransform
		state.TotalItems = workspaceImportArchiveOperationCountFromCounts(state.EntityCounts) +
			state.EntityCounts[workspaceExportItemPage]
		// A moved workspace may have no pages; one put in by Settings ›
		// Import brings some.
		if state.TotalItems < 1 && !payload.KeepIDs {
			return workspaceImportInputError()
		}
		var unreferencedFiles int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM files WHERE reference_count = 0`).Scan(&unreferencedFiles); err != nil {
			return workspaceImportStorageErrorAt(err, "archive-text-file-reference")
		}
		if unreferencedFiles != 0 {
			return workspaceImportInvalid(fmt.Sprintf("%d of %s's files aren't named by any line of %s",
				unreferencedFiles, workspaceExportFilesIndexPath, workspaceExportEntitiesPath))
		}
		if err := saveWorkspaceImportArchivePreparationState(tx, state); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return workspaceImportStorageErrorAt(err, "archive-text-map-commit")
		}
		return r.advanceWorkspaceArchivePreparationJob(job, payload)
	}
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-text-ygo-read")
	}
	place := fmt.Sprintf("%s line %d (%s)", workspaceExportEntitiesPath, ygoSequence+1, workspaceExportItemYgo)
	value := archiveYgoValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil || value.FormatVersion != ygoDocumentFormatVersion {
		return workspaceImportAt(workspaceImportInvalid("formatVersion isn't the text document format this Leafen reads"), place)
	}
	if _, err := workspaceImportArchiveReference(tx, value.PageID, workspaceExportItemPage); err != nil {
		if workspaceImportRefusal(err) == nil {
			return err
		}
		return workspaceImportAt(workspaceImportInvalid("pageId names no page of the bundle"), place)
	}
	document := crdt.New()
	if value.StateHex != "" {
		stateBytes, decodeErr := hex.DecodeString(value.StateHex)
		if decodeErr != nil || crdt.ApplyUpdateV1(document, stateBytes, nil) != nil {
			return workspaceImportAt(workspaceImportInvalid("stateHex isn't a text document update in hexadecimal"), place)
		}
	}
	if value.StructuralStateHex != "" {
		structural, decodeErr := hex.DecodeString(value.StructuralStateHex)
		if decodeErr != nil || crdt.ApplyUpdateV1(crdt.New(), structural, nil) != nil {
			return workspaceImportAt(workspaceImportInvalid("structuralStateHex isn't a text document update in hexadecimal"), place)
		}
	}
	rows, err := tx.Query(`
		SELECT sequence, source_id FROM entities
		WHERE kind = ? AND page_id = ? AND sequence > ?
		ORDER BY sequence LIMIT ?
	`, workspaceExportItemBlock, value.PageID, state.TextBlockSequence, workspaceImportMutationTurnItems)
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-text-block-read")
	}
	count := 0
	lastSequence := state.TextBlockSequence
	for rows.Next() {
		var sequence int
		var blockID string
		if rows.Scan(&sequence, &blockID) != nil {
			rows.Close()
			return workspaceImportInputError()
		}
		if err := stageWorkspaceImportArchiveBlockText(tx, document, blockID); err != nil {
			rows.Close()
			return workspaceImportStorageErrorAt(err, "archive-text-insert")
		}
		lastSequence = sequence
		count++
	}
	if err := rows.Close(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-text-block-close")
	}
	state.TextBlockSequence = lastSequence
	if count < workspaceImportMutationTurnItems {
		state.TextYgoSequence = ygoSequence
		state.TextBlockSequence = -1
	}
	if err := saveWorkspaceImportArchivePreparationState(tx, state); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-text-map-commit")
	}
	return r.advanceWorkspaceArchivePreparationJob(job, payload)
}
