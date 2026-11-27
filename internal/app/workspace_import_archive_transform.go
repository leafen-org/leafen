package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
)

type workspaceImportArchiveChunkWriter struct {
	file        *os.File
	hasher      hash.Hash
	written     int64
	index       int
	chunkHashes []string
	pending     []workspaceImportStagedItem
}

func writeWorkspaceImportArchiveChunks(
	directory, jobID string,
	mapping *sql.DB,
) (storedWorkspaceImportSource, string, int, error) {
	storageKey := jobID + ".ndjson"
	finalPath := filepath.Join(directory, storageKey)
	temporaryPath := filepath.Join(directory, "."+storageKey+".tmp")
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return storedWorkspaceImportSource{}, "", 0, workspaceImportStorageError(err)
	}
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedWorkspaceImportSource{}, "", 0, workspaceImportStorageError(err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = file.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	writer := &workspaceImportArchiveChunkWriter{file: file, hasher: sha256.New()}
	flushPending := func() error {
		if len(writer.pending) == 0 {
			return nil
		}
		return writer.flush(false)
	}
	title, pageItems, err := appendWorkspaceImportArchivePages(writer, mapping)
	if err == nil {
		err = ensureWorkspaceImportArchiveSourceLevels(mapping)
	}
	if err == nil {
		operationsStarted := false
		var passes []workspaceImportArchiveTransformPass
		// The operations go in the order a prepared archive writes them, as
		// many levels as its databases nest.
		passes, err = workspaceImportArchiveTransformPassesOf(mapping)
		for _, pass := range passes {
			if !workspaceImportArchiveLeveledKind(pass.kind) && pass.kind != workspaceExportItemBlock &&
				pass.kind != workspaceExportItemRowPageBlock {
				continue
			}
			if workspaceImportArchiveKindCount(mapping, pass.kind) == 0 {
				continue
			}
			if !operationsStarted {
				err = flushPending()
				if err != nil {
					break
				}
				operationsStarted = true
			}
			err = appendWorkspaceImportArchiveOperations(writer, mapping, pass)
			if err != nil {
				break
			}
		}
	}
	if err == nil && workspaceImportArchiveKindCount(mapping, workspaceExportItemTemplate) > 0 {
		if err = flushPending(); err == nil {
			err = appendWorkspaceImportArchiveTemplates(writer, mapping)
		}
	}
	if err == nil && workspaceImportArchiveKindCount(mapping, workspaceExportItemComment) > 0 {
		if err = flushPending(); err == nil {
			err = appendWorkspaceImportArchiveComments(writer, mapping)
		}
	}
	if err == nil && workspaceImportArchiveKindCount(mapping, workspaceExportItemBlockTrash) > 0 {
		if err = flushPending(); err == nil {
			err = appendWorkspaceImportArchiveBlockTrash(writer, mapping)
		}
	}
	if err == nil && workspaceImportArchiveKindCount(mapping, workspaceExportItemRowBlockTrash) > 0 {
		if err = flushPending(); err == nil {
			err = appendWorkspaceImportArchiveRowBlocks(writer, mapping, workspaceExportItemRowBlockTrash)
		}
	}
	if err == nil && workspaceImportArchiveKindCount(mapping, workspaceExportItemCustomEmoji) > 0 {
		if err = flushPending(); err == nil {
			err = appendWorkspaceImportArchiveCustomEmojis(writer, mapping)
		}
	}
	if err == nil && workspaceImportArchiveKindCount(mapping, workspaceExportItemDatabaseAutomation) > 0 {
		if err = flushPending(); err == nil {
			err = appendWorkspaceImportArchiveAutomations(writer, mapping)
		}
	}
	if err == nil {
		err = validateWorkspaceImportArchiveReferences(mapping)
	}
	if err == nil {
		err = writer.finish()
	}
	if err != nil {
		return storedWorkspaceImportSource{}, "", 0, err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return storedWorkspaceImportSource{}, "", 0, workspaceImportStorageError(err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return storedWorkspaceImportSource{}, "", 0, workspaceImportStorageError(err)
	}
	removeTemporary = false
	if err := syncDirectory(directory); err != nil {
		_ = os.Remove(finalPath)
		return storedWorkspaceImportSource{}, "", 0, workspaceImportStorageError(err)
	}
	encodedHashes, err := json.Marshal(writer.chunkHashes)
	if err != nil {
		_ = os.Remove(finalPath)
		return storedWorkspaceImportSource{}, "", 0, err
	}
	return storedWorkspaceImportSource{
		JobID: jobID, SchemaVersion: 3, Format: workspaceImportFormatNativeArchiveV2,
		StorageKey: storageKey, SHA256: hex.EncodeToString(writer.hasher.Sum(nil)),
		SourceBytes: writer.written, ChunkCount: writer.index,
		ChunkHashesJSON: string(encodedHashes),
	}, title, pageItems + workspaceImportArchiveOperationCount(mapping), nil
}

func (w *workspaceImportArchiveChunkWriter) append(item workspaceImportStagedItem) error {
	limit := min(workspaceImportStagedRecordsPerChunk, workspaceImportMutationTurnItems)
	pairedTableRow := workspaceImportArchiveTableParentAndFirstRow(w.pending, item)
	if len(w.pending) >= limit && !pairedTableRow {
		if err := w.flush(false); err != nil {
			return err
		}
	}
	candidate := append(append([]workspaceImportStagedItem{}, w.pending...), item)
	encoded, err := json.Marshal(workspaceImportStagedChunk{
		SchemaVersion: 3, Index: w.index, Items: candidate,
	})
	if err != nil {
		return err
	}
	if len(encoded)+1 > workspaceImportStagedChunkBytes {
		if len(w.pending) == 0 {
			return workspaceImportInputError()
		}
		if pairedTableRow {
			if len(w.pending) == 1 {
				return workspaceImportInputError()
			}
			parent := w.pending[len(w.pending)-1]
			w.pending = w.pending[:len(w.pending)-1]
			if err := w.flush(false); err != nil {
				return err
			}
			w.pending = []workspaceImportStagedItem{parent}
		} else if err := w.flush(false); err != nil {
			return err
		}
	}
	w.pending = append(w.pending, item)
	return nil
}

func workspaceImportArchiveTableParentAndFirstRow(
	pending []workspaceImportStagedItem,
	item workspaceImportStagedItem,
) bool {
	if len(pending) > 0 && workspaceImportArchiveLinkedPair(pending[len(pending)-1].Operation, item.Operation) {
		return true
	}
	if len(pending) == 0 || item.Operation == nil || item.Operation.Block == nil ||
		item.Operation.Kind != graphOperationKindUpsertBlock ||
		item.Operation.Block.Type != graphBlockTypeTableRow || item.Operation.Block.ParentID == nil {
		return false
	}
	parent := pending[len(pending)-1].Operation
	return parent != nil && parent.Kind == graphOperationKindUpsertBlock && parent.Block != nil &&
		parent.Block.Type == graphBlockTypeTable && parent.Block.ID == *item.Operation.Block.ParentID
}

func (w *workspaceImportArchiveChunkWriter) flush(last bool) error {
	// A bundle with nothing in it, a moved workspace without pages, is one
	// empty last chunk.
	if len(w.pending) == 0 && (!last || w.index != 0) {
		return workspaceImportInputError()
	}
	chunk := workspaceImportStagedChunk{
		SchemaVersion: 3, Index: w.index, Last: last, Items: w.pending,
	}
	encoded, err := json.Marshal(chunk)
	if err != nil || len(encoded)+1 > workspaceImportStagedChunkBytes {
		return workspaceImportInputError()
	}
	encoded = append(encoded, '\n')
	digest := sha256.Sum256(encoded)
	w.chunkHashes = append(w.chunkHashes, hex.EncodeToString(digest[:]))
	written, err := io.MultiWriter(w.file, w.hasher).Write(encoded)
	if err != nil || written != len(encoded) {
		return workspaceImportStorageError(err)
	}
	w.written += int64(written)
	w.index++
	w.pending = nil
	return nil
}

func (w *workspaceImportArchiveChunkWriter) finish() error {
	return w.flush(true)
}
