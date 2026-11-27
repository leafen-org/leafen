package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

const workspaceImportDirectory = "workspace-imports"

type workspaceImportStagedChunk struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Index         int                         `json:"index"`
	Last          bool                        `json:"last"`
	Items         []workspaceImportStagedItem `json:"items"`
}

type storedWorkspaceImportSource struct {
	JobID               string `db:"job_id"`
	SchemaVersion       int    `db:"schema_version"`
	Format              string `db:"format"`
	StorageKey          string `db:"storage_key"`
	SHA256              string `db:"sha256"`
	SourceBytes         int64  `db:"source_bytes"`
	ChunkCount          int    `db:"chunk_count"`
	ChunkHashesJSON     string `db:"chunk_hashes_json"`
	ArchiveStorageKey   string `db:"archive_storage_key"`
	ArchiveSHA256       string `db:"archive_sha256"`
	ArchiveBytes        int64  `db:"archive_bytes"`
	FileIndexStorageKey string `db:"file_index_storage_key"`
	FileIndexSHA256     string `db:"file_index_sha256"`
	FileCount           int    `db:"file_count"`
	FileBytes           int64  `db:"file_bytes"`
	// Items is how many items staging wrote; it isn't kept.
	Items int `db:"-"`
}

// workspaceImportItemSource gives an import's staged items one after
// another, so the items of a large file aren't all held at once (2026-10-13:
// a 100,050-row CSV took 600 MB).
type workspaceImportItemSource func(emit func(workspaceImportStagedItem) error) error

func workspaceImportItemsOf(items []workspaceImportStagedItem) workspaceImportItemSource {
	return func(emit func(workspaceImportStagedItem) error) error {
		for _, item := range items {
			if err := emit(item); err != nil {
				return err
			}
		}
		return nil
	}
}

func stageWorkspaceImportSource(appDir, jobID, format string, items workspaceImportItemSource) (storedWorkspaceImportSource, error) {
	schemaVersion := workspaceImportSourceSchemaVersion(format)
	if schemaVersion == 0 {
		return storedWorkspaceImportSource{}, workspaceImportInputError()
	}
	directory := filepath.Join(appDir, workspaceImportDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageError(err)
	}
	storageKey := jobID + ".ndjson"
	finalPath := filepath.Join(directory, storageKey)
	temporaryPath := filepath.Join(directory, "."+storageKey+".tmp")
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return storedWorkspaceImportSource{}, workspaceImportStorageError(err)
	}
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageError(err)
	}
	defer os.Remove(temporaryPath)
	buffered := bufio.NewWriter(file)
	hasher := sha256.New()
	writer := &workspaceImportChunkWriter{schemaVersion: schemaVersion, out: io.MultiWriter(buffered, hasher)}
	if err := items(writer.add); err != nil {
		_ = file.Close()
		return storedWorkspaceImportSource{}, err
	}
	if err := writer.finish(); err != nil {
		_ = file.Close()
		return storedWorkspaceImportSource{}, err
	}
	if err := errors.Join(buffered.Flush(), file.Sync(), file.Close()); err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageError(err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageError(err)
	}
	if err := syncDirectory(directory); err != nil {
		_ = os.Remove(finalPath)
		return storedWorkspaceImportSource{}, workspaceImportStorageError(err)
	}
	encodedChunkHashes, err := json.Marshal(writer.hashes)
	if err != nil {
		_ = os.Remove(finalPath)
		return storedWorkspaceImportSource{}, err
	}
	return storedWorkspaceImportSource{
		JobID: jobID, SchemaVersion: schemaVersion, Format: format,
		StorageKey: storageKey, SHA256: hex.EncodeToString(hasher.Sum(nil)),
		SourceBytes: writer.written, ChunkCount: len(writer.hashes), ChunkHashesJSON: string(encodedChunkHashes),
		Items: writer.items,
	}, nil
}

// workspaceImportChunkWriter writes staged items as chunk lines as they
// come: each item encoded once, a chunk as many as a turn applies and fit
// its bytes. A full chunk waits for the next to begin, which tells whether
// it is the last. A table its chunk ends with goes to the next one with its
// first row, as a turn's table without rows is refused (2026-11-13: a table
// just before a turn's end failed the whole document's import); so does a
// document's inline database's data source, properties and view, with its
// block, so no turn leaves a database no block shows.
type workspaceImportChunkWriter struct {
	schemaVersion int
	out           io.Writer
	open, held    []json.RawMessage
	openBytes     int
	hashes        []string
	written       int64
	items         int
	// tableID is the open chunk's last item's when it is a table.
	tableID string
	// setup is where in the open chunk a database's data source begins, one
	// past it, while its block is still to come; 0 for none.
	setup int
}

func (writer *workspaceImportChunkWriter) add(item workspaceImportStagedItem) error {
	encoded, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if workspaceImportChunkOverhead+len(encoded)+1 > workspaceImportStagedChunkBytes {
		return workspaceImportInputError()
	}
	if len(writer.open) == min(workspaceImportStagedRecordsPerChunk, workspaceImportMutationTurnItems) ||
		workspaceImportChunkOverhead+writer.openBytes+len(writer.open)+len(encoded)+1 > workspaceImportStagedChunkBytes {
		if writer.held != nil {
			if err := writer.write(writer.held, false); err != nil {
				return err
			}
		}
		var carried []json.RawMessage
		if last := len(writer.open) - 1; last > 0 && writer.tableID != "" && item.Operation == nil &&
			item.Type == graphBlockTypeTableRow && item.ParentID == writer.tableID {
			carried = []json.RawMessage{writer.open[last]}
			writer.open = writer.open[:last]
		} else if start := writer.setup - 1; start > 0 {
			carried = slices.Clone(writer.open[start:])
			writer.open = writer.open[:start]
			writer.setup = 1
		} else {
			writer.setup = 0
		}
		writer.held, writer.open, writer.openBytes = writer.open, carried, 0
		for _, encoded := range carried {
			writer.openBytes += len(encoded)
		}
	}
	writer.open = append(writer.open, encoded)
	writer.openBytes += len(encoded)
	writer.items++
	writer.tableID = ""
	if item.Operation == nil && item.Type == graphBlockTypeTable {
		writer.tableID = item.ID
	}
	switch {
	case item.Operation != nil && item.Operation.Kind == graphOperationKindUpsertDataSource && item.ID == "" && item.Type == "":
		writer.setup = len(writer.open)
	case item.Operation == nil && item.Type == graphBlockTypeInlineDatabase:
		writer.setup = 0
	}
	return nil
}

func (writer *workspaceImportChunkWriter) finish() error {
	switch {
	case len(writer.open) > 0 && writer.held != nil:
		if err := writer.write(writer.held, false); err != nil {
			return err
		}
		return writer.write(writer.open, true)
	case len(writer.open) > 0:
		return writer.write(writer.open, true)
	case writer.held != nil:
		return writer.write(writer.held, true)
	}
	return workspaceImportInputError()
}

// workspaceImportChunkOverhead is a chunk line's bytes besides its items:
// its fields with the largest index, the brackets and the newline.
const workspaceImportChunkOverhead = len(`{"schemaVersion":999,"index":9999999999,"last":false,"items":[]}`) + 1

func (writer *workspaceImportChunkWriter) write(items []json.RawMessage, last bool) error {
	size := workspaceImportChunkOverhead + len(items)
	for _, item := range items {
		size += len(item)
	}
	line := make([]byte, 0, size)
	line = fmt.Appendf(line, `{"schemaVersion":%d,"index":%d,"last":%t,"items":[`, writer.schemaVersion, len(writer.hashes), last)
	for index, item := range items {
		if index > 0 {
			line = append(line, ',')
		}
		line = append(line, item...)
	}
	line = append(line, "]}\n"...)
	if len(line) > workspaceImportStagedChunkBytes {
		return workspaceImportInputError()
	}
	digest := sha256.Sum256(line)
	writer.hashes = append(writer.hashes, hex.EncodeToString(digest[:]))
	count, err := writer.out.Write(line)
	writer.written += int64(count)
	if err != nil || count != len(line) {
		return workspaceImportStorageError(err)
	}
	return nil
}

func loadWorkspaceImportStagedChunk(appDir string, source storedWorkspaceImportSource, offset int64, expectedIndex int) (workspaceImportStagedChunk, int64, error) {
	if !validWorkspaceImportSourceVersion(source.SchemaVersion, source.Format) ||
		filepath.Base(source.StorageKey) != source.StorageKey || offset < 0 || offset >= source.SourceBytes {
		return workspaceImportStagedChunk{}, 0, workspaceImportStateError()
	}
	file, err := os.Open(filepath.Join(appDir, workspaceImportDirectory, source.StorageKey))
	if err != nil {
		return workspaceImportStagedChunk{}, 0, workspaceImportStorageError(err)
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return workspaceImportStagedChunk{}, 0, workspaceImportStorageError(err)
	}
	reader := bufio.NewReader(io.LimitReader(file, workspaceImportStagedChunkBytes+1))
	line, err := reader.ReadBytes('\n')
	if err != nil || len(line) < 2 || len(line) > workspaceImportStagedChunkBytes || line[len(line)-1] != '\n' {
		return workspaceImportStagedChunk{}, 0, workspaceImportStateError()
	}
	chunkHashes := []string{}
	if json.Unmarshal([]byte(source.ChunkHashesJSON), &chunkHashes) != nil ||
		len(chunkHashes) != source.ChunkCount || expectedIndex >= len(chunkHashes) {
		return workspaceImportStagedChunk{}, 0, workspaceImportStateError()
	}
	digest := sha256.Sum256(line)
	if hex.EncodeToString(digest[:]) != chunkHashes[expectedIndex] {
		return workspaceImportStagedChunk{}, 0, workspaceImportStateError()
	}
	chunk := workspaceImportStagedChunk{}
	if err := json.Unmarshal(line[:len(line)-1], &chunk); err != nil || chunk.SchemaVersion != source.SchemaVersion ||
		chunk.Index != expectedIndex || (len(chunk.Items) < 1 && !(chunk.Last && source.ChunkCount == 1)) ||
		len(chunk.Items) > workspaceImportStagedRecordsPerChunk {
		return workspaceImportStagedChunk{}, 0, workspaceImportStateError()
	}
	return chunk, offset + int64(len(line)), nil
}

func verifyWorkspaceImportSource(appDir string, source storedWorkspaceImportSource) error {
	if !validWorkspaceImportSourceVersion(source.SchemaVersion, source.Format) ||
		filepath.Base(source.StorageKey) != source.StorageKey || source.SourceBytes < 1 || source.ChunkCount < 1 {
		return workspaceImportStateError()
	}
	chunkHashes := []string{}
	if json.Unmarshal([]byte(source.ChunkHashesJSON), &chunkHashes) != nil || len(chunkHashes) != source.ChunkCount {
		return workspaceImportStateError()
	}
	for _, chunkHash := range chunkHashes {
		if len(chunkHash) != 64 {
			return workspaceImportStateError()
		}
	}
	digest, bytes, err := fileSHA256(filepath.Join(appDir, workspaceImportDirectory, source.StorageKey))
	if err != nil {
		return workspaceImportStorageError(err)
	}
	if digest != source.SHA256 || bytes != source.SourceBytes {
		return workspaceImportStateError()
	}
	if source.Format == workspaceImportFormatNativeArchiveV2 {
		if filepath.Base(source.ArchiveStorageKey) != source.ArchiveStorageKey || source.ArchiveBytes < 1 || len(source.ArchiveSHA256) != 64 {
			return workspaceImportStateError()
		}
		archiveDigest, archiveBytes, err := fileSHA256(filepath.Join(appDir, workspaceImportDirectory, source.ArchiveStorageKey))
		if err != nil {
			return workspaceImportStorageError(err)
		}
		if archiveDigest != source.ArchiveSHA256 || archiveBytes != source.ArchiveBytes {
			return workspaceImportStateError()
		}
		if source.FileCount == 0 {
			if source.FileBytes != 0 || source.FileIndexStorageKey != "" || source.FileIndexSHA256 != "" {
				return workspaceImportStateError()
			}
		} else {
			if filepath.Base(source.FileIndexStorageKey) != source.FileIndexStorageKey ||
				source.FileCount < 1 || source.FileBytes < 1 || len(source.FileIndexSHA256) != 64 {
				return workspaceImportStateError()
			}
			indexDigest, _, err := fileSHA256(filepath.Join(
				appDir, workspaceImportDirectory, source.FileIndexStorageKey,
			))
			if err != nil {
				return workspaceImportStorageError(err)
			}
			if indexDigest != source.FileIndexSHA256 {
				return workspaceImportStateError()
			}
		}
	}
	return nil
}

func workspaceImportSourceSchemaVersion(format string) int {
	if format == workspaceImportFormatMarkdown {
		return 1
	}
	if format == workspaceImportFormatCSV {
		return 2
	}
	if format == workspaceImportFormatNativeArchiveV2 {
		return 3
	}
	return 0
}

func validWorkspaceImportSourceVersion(version int, format string) bool {
	return workspaceImportSourceSchemaVersion(format) == version
}

func removeWorkspaceImportSourceFile(appDir, storageKey string) error {
	if storageKey == "" || filepath.Base(storageKey) != storageKey {
		return nil
	}
	path := filepath.Join(appDir, workspaceImportDirectory, storageKey)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return workspaceImportStorageError(err)
	}
	return syncDirectory(filepath.Dir(path))
}
