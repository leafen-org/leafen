package app

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

func validateWorkspaceImportArchiveFileEnvelope(
	descriptor workspaceExportArchiveDescriptor,
	entries map[string]*zip.File,
) error {
	included, _ := descriptor.Files["included"].(bool)
	count, countOK := jsonNumberInt(descriptor.Files["count"])
	bytesCount, bytesOK := jsonNumberInt(descriptor.Files["bytes"])
	stripped, strippedOK := jsonNumberInt(descriptor.Files["strippedReferences"])
	indexStream := descriptor.Streams["fileIndex"]
	dataStream := descriptor.Streams["fileData"]
	indexEntry := entries[workspaceExportFilesIndexPath]
	dataEntry := entries[workspaceExportFilesDataPath]
	if !included || !countOK || !bytesOK || !strippedOK || stripped != 0 ||
		len(descriptor.Files) != 4 || indexStream.Records != count ||
		dataStream.Records != 0 || indexEntry == nil || dataEntry == nil ||
		int64(dataEntry.UncompressedSize64) != int64(bytesCount) {
		return workspaceImportFilesMismatch()
	}
	if count == 0 {
		if bytesCount != 0 || indexStream.Bytes != 0 || dataStream.Bytes != 0 {
			return workspaceImportFilesMismatch()
		}
		return nil
	}
	if bytesCount < 1 || indexStream.Bytes < 1 || dataStream.Bytes != int64(bytesCount) {
		return workspaceImportFilesMismatch()
	}
	return nil
}

// workspaceImportFilesMismatch is a descriptor whose files don't fit the
// bundle's file entries.
func workspaceImportFilesMismatch() error {
	return workspaceImportInvalid(workspaceExportDescriptorPath + "'s files (included, count, bytes, strippedReferences 0) and its " +
		"fileIndex and fileData streams don't match " + workspaceExportFilesIndexPath + " and " + workspaceExportFilesDataPath)
}

func (r *durableJobRunner) inventoryWorkspaceArchiveFilesTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
	source storedWorkspaceImportSource,
) error {
	archivePath := filepath.Join(r.app.DataDir(), workspaceImportDirectory, source.ArchiveStorageKey)
	archive, entries, _, err := openWorkspaceImportArchiveEnvelope(archivePath)
	if archive != nil {
		defer archive.Close()
	}
	if err != nil {
		return workspaceImportInputError()
	}
	indexEntry := entries[workspaceExportFilesIndexPath]
	dataEntry := entries[workspaceExportFilesDataPath]
	count, _ := jsonNumberInt(state.Descriptor.Files["count"])
	bytesCount, _ := jsonNumberInt(state.Descriptor.Files["bytes"])
	if count == 0 {
		if err := verifyEmptyWorkspaceImportArchiveFileStreams(
			indexEntry, dataEntry,
			state.Descriptor.Streams["fileIndex"], state.Descriptor.Streams["fileData"],
		); err != nil {
			return err
		}
		state.Phase = workspaceImportArchivePreparePhaseEntityInventory
		return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
	}
	if state.FileCount >= count || state.FileDataOffset >= int64(bytesCount) {
		return workspaceImportInputError()
	}
	indexFile, indexOffset, err := openWorkspaceImportArchiveStoredEntry(archivePath, indexEntry)
	if err != nil {
		return err
	}
	defer indexFile.Close()
	place := fmt.Sprintf("%s line %d", workspaceExportFilesIndexPath, state.FileCount+1)
	line, err := readWorkspaceImportArchiveLine(
		indexFile, indexOffset, int64(indexEntry.UncompressedSize64), state.FileIndexOffset,
		workspaceImportArchiveFileIndexLineBytes,
	)
	if err != nil {
		return workspaceImportAt(err, place)
	}
	entry, err := decodeWorkspaceImportArchiveFile(
		line[:len(line)-1], workspaceImportArchiveFileSalt(job.ID, payload), state.FileDataOffset,
	)
	if err != nil {
		return workspaceImportAt(err, place)
	}
	fileHasher, err := workspaceImportArchivePreparationHasher(state.FileHashState)
	if err != nil {
		return err
	}
	dataHasher, err := workspaceImportArchivePreparationHasher(state.FileDataHashState)
	if err != nil {
		return err
	}
	remaining := entry.Bytes - state.FilePartOffset
	if remaining <= 0 {
		return workspaceImportStateErrorAt("archive-file-inventory-part")
	}
	turnBytes := min(remaining, fileTransferPartBytes)
	dataFile, dataOffset, err := openWorkspaceImportArchiveStoredEntry(archivePath, dataEntry)
	if err != nil {
		return err
	}
	reader := io.NewSectionReader(
		dataFile,
		dataOffset+state.FileDataOffset+state.FilePartOffset,
		turnBytes,
	)
	written, copyErr := io.CopyBuffer(
		io.MultiWriter(fileHasher, dataHasher), reader, make([]byte, productFileDigestBufferSize),
	)
	closeErr := dataFile.Close()
	if copyErr != nil || closeErr != nil || written != turnBytes {
		return workspaceImportStorageErrorAt(errors.Join(copyErr, closeErr), "archive-file-inventory-read")
	}
	state.FilePartOffset += written
	state.FileHashState, err = marshalWorkspaceImportArchiveHash(fileHasher)
	if err != nil {
		return err
	}
	state.FileDataHashState, err = marshalWorkspaceImportArchiveHash(dataHasher)
	if err != nil {
		return err
	}
	if state.FilePartOffset < entry.Bytes {
		return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
	}
	if hex.EncodeToString(fileHasher.Sum(nil)) != entry.SHA256 {
		return workspaceImportAt(workspaceImportInvalid("the file's bytes in "+workspaceExportFilesDataPath+" don't have its sha256"), place)
	}
	indexHasher, err := workspaceImportArchivePreparationHasher(state.FileIndexHashState)
	if err != nil {
		return err
	}
	if _, err := indexHasher.Write(line); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-file-index-hash")
	}
	state.FileIndexHashState, err = marshalWorkspaceImportArchiveHash(indexHasher)
	if err != nil {
		return err
	}
	outputHasher, err := workspaceImportArchivePreparationHasher(state.FileOutputHashState)
	if err != nil {
		return err
	}
	canonical, err := json.Marshal(entry)
	if err != nil {
		return workspaceImportInputError()
	}
	chainDigest := sha256.Sum256(append([]byte(state.FileChain), canonical...))
	state.FileChain = hex.EncodeToString(chainDigest[:])
	entry.ChainSHA256 = state.FileChain
	encoded, err := json.Marshal(entry)
	if err != nil {
		return workspaceImportInputError()
	}
	encoded = append(encoded, '\n')
	directory := filepath.Join(r.app.DataDir(), workspaceImportDirectory)
	outputPath := workspaceImportArchivePreparationIndexPath(directory, job.ID)
	if err := appendWorkspaceImportArchivePreparationFile(
		outputPath, state.FileOutputBytes, outputHasher, encoded,
	); err != nil {
		return err
	}
	state.FileOutputBytes += int64(len(encoded))
	state.FileOutputHashState, err = marshalWorkspaceImportArchiveHash(outputHasher)
	if err != nil {
		return err
	}
	state.FileIndexOffset += int64(len(line))
	state.FileDataOffset += entry.Bytes
	state.FilePartOffset = 0
	state.FileHashState = ""
	state.FileCount++
	tx, err := db.Begin()
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-file-map-begin")
	}
	defer tx.Rollback()
	if err := storeWorkspaceImportArchiveFileTx(tx, entry); err != nil {
		return err
	}
	if state.FileCount == count {
		if state.FileIndexOffset != state.Descriptor.Streams["fileIndex"].Bytes ||
			state.FileDataOffset != int64(bytesCount) ||
			hex.EncodeToString(indexHasher.Sum(nil)) != state.Descriptor.Streams["fileIndex"].SHA256 ||
			hex.EncodeToString(dataHasher.Sum(nil)) != state.Descriptor.Streams["fileData"].SHA256 {
			return workspaceImportInvalid(fmt.Sprintf("%s and %s aren't the bytes and SHA-256 %s gives for them",
				workspaceExportFilesIndexPath, workspaceExportFilesDataPath, workspaceExportDescriptorPath))
		}
		state.Files = storedWorkspaceImportSource{
			FileIndexStorageKey: job.ID + ".files.ndjson",
			FileIndexSHA256:     hex.EncodeToString(outputHasher.Sum(nil)),
			FileCount:           state.FileCount, FileBytes: state.FileDataOffset,
		}
		state.Phase = workspaceImportArchivePreparePhaseFileFinalize
	}
	if err := saveWorkspaceImportArchivePreparationState(tx, state); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-file-map-commit")
	}
	return r.advanceWorkspaceArchivePreparationJob(job, payload)
}

func storeWorkspaceImportArchiveFileTx(tx *sql.Tx, file workspaceImportArchiveFile) error {
	if _, err := tx.Exec(
		`INSERT INTO mappings (source_id, target_id, kind) VALUES (?, ?, ?)`,
		file.SourceID, file.TargetID, workspaceImportArchiveFileKind,
	); err != nil {
		return workspaceImportInputError()
	}
	if _, err := tx.Exec(
		`INSERT INTO files (source_id, target_id, offset, name, content_type, scope, bytes, sha256)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		file.SourceID, file.TargetID, file.Offset, file.Name, file.ContentType,
		file.Scope, file.Bytes, file.SHA256,
	); err != nil {
		return workspaceImportInputError()
	}
	return nil
}

func (r *durableJobRunner) finalizeWorkspaceArchiveFileIndexTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
) error {
	directory := filepath.Join(r.app.DataDir(), workspaceImportDirectory)
	temporaryPath := workspaceImportArchivePreparationIndexPath(directory, job.ID)
	finalPath := filepath.Join(directory, state.Files.FileIndexStorageKey)
	if err := finalizeWorkspaceImportArchivePreparationFile(
		temporaryPath, finalPath, state.FileOutputBytes,
	); err != nil {
		return err
	}
	state.Phase = workspaceImportArchivePreparePhaseEntityInventory
	return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
}

func (r *durableJobRunner) inventoryWorkspaceArchiveEntitiesTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
	source storedWorkspaceImportSource,
) error {
	archivePath := filepath.Join(r.app.DataDir(), workspaceImportDirectory, source.ArchiveStorageKey)
	archive, entries, _, err := openWorkspaceImportArchiveEnvelope(archivePath)
	if archive != nil {
		defer archive.Close()
	}
	if err != nil {
		return workspaceImportInputError()
	}
	entry := entries[workspaceExportEntitiesPath]
	file, dataOffset, err := openWorkspaceImportArchiveStoredEntry(archivePath, entry)
	if err != nil {
		return err
	}
	defer file.Close()
	hasher, err := workspaceImportArchivePreparationHasher(state.EntityHashState)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-entity-map-begin")
	}
	defer tx.Rollback()
	turnItems := 0
	turnBytes := int64(0)
	for state.EntityOffset < int64(entry.UncompressedSize64) &&
		turnItems < workspaceImportMutationTurnItems && turnBytes < workspaceImportArchiveDirectBytes {
		place := fmt.Sprintf("%s line %d", workspaceExportEntitiesPath, state.EntitySequence+1)
		line, err := readWorkspaceImportArchiveLine(
			file, dataOffset, int64(entry.UncompressedSize64), state.EntityOffset,
			workspaceImportArchiveDirectBytes,
		)
		if err != nil {
			return workspaceImportAt(err, place)
		}
		if turnItems > 0 && turnBytes+int64(len(line)) > workspaceImportArchiveDirectBytes {
			break
		}
		entity := workspaceImportArchiveEntity{}
		if json.Unmarshal(line[:len(line)-1], &entity) != nil || entity.Value == nil {
			return workspaceImportAt(workspaceImportInvalid("not an object with kind and value"), place)
		}
		if _, ok := state.EntityCounts[entity.Kind]; !ok {
			return workspaceImportAt(workspaceImportInvalid("its kind isn't one a bundle holds"), place)
		}
		place += " (" + entity.Kind + ")"
		if err := inventoryWorkspaceImportArchiveFileReferences(tx, "", entity.Value); err != nil {
			return workspaceImportAt(err, place)
		}
		if err := storeWorkspaceImportArchiveEntity(
			tx, workspaceImportArchiveIDSalt(job.ID, payload), state.EntitySequence, state.Descriptor.FormatVersion, entity,
		); err != nil {
			return workspaceImportAt(err, place)
		}
		if _, err := hasher.Write(line); err != nil {
			return workspaceImportStorageErrorAt(err, "archive-entity-hash")
		}
		state.EntityCounts[entity.Kind]++
		state.EntitySequence++
		state.EntityOffset += int64(len(line))
		turnItems++
		turnBytes += int64(len(line))
	}
	state.EntityHashState, err = marshalWorkspaceImportArchiveHash(hasher)
	if err != nil {
		return err
	}
	if state.EntityOffset == int64(entry.UncompressedSize64) {
		if !validWorkspaceImportArchiveEntityCompletion(state, hasher.Sum(nil)) {
			stream := state.Descriptor.Streams["entities"]
			if state.EntitySequence != stream.Records {
				return workspaceImportInvalid(fmt.Sprintf("%s has %d lines; %s's entities stream says %d records",
					workspaceExportEntitiesPath, state.EntitySequence, workspaceExportDescriptorPath, stream.Records))
			}
			return workspaceImportInvalid(fmt.Sprintf("%s's SHA-256 or its lines of each kind (pages, blocks, …) aren't what %s gives",
				workspaceExportEntitiesPath, workspaceExportDescriptorPath))
		}
		state.Phase = workspaceImportArchivePreparePhaseTextMaterialize
	}
	if turnItems == 0 && state.EntityOffset < int64(entry.UncompressedSize64) {
		return workspaceImportStateErrorAt("archive-entity-inventory-frontier")
	}
	if err := saveWorkspaceImportArchivePreparationState(tx, state); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-entity-map-commit")
	}
	return r.advanceWorkspaceArchivePreparationJob(job, payload)
}

func validWorkspaceImportArchiveEntityCompletion(
	state workspaceImportArchivePreparationState,
	digest []byte,
) bool {
	descriptor := state.Descriptor
	stream := descriptor.Streams["entities"]
	return state.EntitySequence == stream.Records && hex.EncodeToString(digest) == stream.SHA256 &&
		state.EntityCounts[workspaceExportItemPage] == descriptor.Counts["pages"] &&
		state.EntityCounts[workspaceExportItemBlock] == descriptor.Counts["blocks"] &&
		state.EntityCounts[workspaceExportItemDataSource] == descriptor.Counts["dataSources"] &&
		state.EntityCounts[workspaceExportItemView] == descriptor.Counts["dbViews"] &&
		state.EntityCounts[workspaceExportItemProperty] == descriptor.Counts["dbProperties"] &&
		state.EntityCounts[workspaceExportItemRow] == descriptor.Counts["databaseRows"] &&
		state.EntityCounts[workspaceExportItemYgo] == descriptor.Counts["ygoDocuments"] &&
		state.EntityCounts[workspaceExportItemTemplate] == descriptor.Counts["dbTemplates"] &&
		state.EntityCounts[workspaceExportItemComment] == descriptor.Counts["comments"] &&
		state.EntityCounts[workspaceExportItemDatabaseContainer] == descriptor.Counts["databaseContainers"] &&
		state.EntityCounts[workspaceExportItemBlockTrash] == descriptor.Counts["blockTrashTombstones"] &&
		state.EntityCounts[workspaceExportItemCustomEmoji] == descriptor.Counts["customEmojis"] &&
		state.EntityCounts[workspaceExportItemRowPageBlock] == descriptor.Counts["rowPageBlocks"] &&
		state.EntityCounts[workspaceExportItemRowBlockTrash] == descriptor.Counts["rowBlockTrashTombstones"] &&
		state.EntityCounts[workspaceExportItemTeamspace] == descriptor.Counts["teamspaces"] &&
		state.EntityCounts[workspaceExportItemDatabaseAutomation] == descriptor.Counts["dbAutomations"]
}

func openWorkspaceImportArchiveStoredEntry(
	archivePath string,
	entry *zip.File,
) (*os.File, int64, error) {
	if entry == nil || entry.Method != zip.Store {
		return nil, 0, workspaceImportInputError()
	}
	offset, err := entry.DataOffset()
	if err != nil {
		return nil, 0, workspaceImportInputError()
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, 0, workspaceImportStorageErrorAt(err, "archive-entry-open")
	}
	return file, offset, nil
}

func readWorkspaceImportArchiveLine(
	file *os.File,
	dataOffset, entryBytes, logicalOffset, limit int64,
) ([]byte, error) {
	remaining := entryBytes - logicalOffset
	if remaining <= 0 || limit < 2 {
		return nil, workspaceImportInputError()
	}
	reader := bufio.NewReaderSize(
		io.NewSectionReader(file, dataOffset+logicalOffset, min(remaining, limit+1)),
		workspaceExportReaderWindowBytes,
	)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, workspaceImportInputError()
	}
	if int64(len(line)) > limit {
		return nil, workspaceImportInvalid(fmt.Sprintf("the line is longer than %d bytes", limit))
	}
	if len(line) < 2 || line[len(line)-1] != '\n' || !utf8.Valid(line[:len(line)-1]) {
		return nil, workspaceImportInvalid(workspaceImportArchiveLineShape)
	}
	return line, nil
}

func marshalWorkspaceImportArchiveHash(hasher interface{ Sum([]byte) []byte }) (string, error) {
	state, err := hasher.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return "", workspaceImportStateErrorAt("archive-prepare-hash-state")
	}
	return hex.EncodeToString(state), nil
}

func appendWorkspaceImportArchivePreparationFile(
	path string,
	frontier int64,
	hasher interface{ io.Writer },
	encoded []byte,
) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-output-open")
	}
	info, statErr := file.Stat()
	if statErr != nil || info.Size() < frontier {
		_ = file.Close()
		return workspaceImportStorageErrorAt(statErr, "archive-prepare-output-size")
	}
	if info.Size() > frontier {
		if err := file.Truncate(frontier); err != nil {
			_ = file.Close()
			return workspaceImportStorageErrorAt(err, "archive-prepare-output-truncate")
		}
	}
	if _, err := file.Seek(frontier, io.SeekStart); err != nil {
		_ = file.Close()
		return workspaceImportStorageErrorAt(err, "archive-prepare-output-seek")
	}
	written, writeErr := io.MultiWriter(file, hasher).Write(encoded)
	closeErr := errors.Join(file.Sync(), file.Close())
	if writeErr != nil || closeErr != nil || written != len(encoded) {
		return workspaceImportStorageErrorAt(errors.Join(writeErr, closeErr), "archive-prepare-output-write")
	}
	return nil
}

func finalizeWorkspaceImportArchivePreparationFile(
	temporaryPath, finalPath string,
	expectedBytes int64,
) error {
	if info, err := os.Stat(temporaryPath); err == nil {
		if info.Size() != expectedBytes {
			return workspaceImportStateErrorAt("archive-prepare-output-finalize")
		}
		if err := os.Rename(temporaryPath, finalPath); err != nil {
			return workspaceImportStorageErrorAt(err, "archive-prepare-output-finalize")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		info, finalErr := os.Stat(finalPath)
		if finalErr != nil || info.Size() != expectedBytes {
			return workspaceImportStateErrorAt("archive-prepare-output-finalize")
		}
	} else {
		return workspaceImportStorageErrorAt(err, "archive-prepare-output-finalize")
	}
	return syncDirectory(filepath.Dir(finalPath))
}
