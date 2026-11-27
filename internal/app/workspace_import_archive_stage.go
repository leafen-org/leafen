package app

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/reearth/ygo/crdt"
)

const workspaceImportArchiveDescriptorBytes = 64 << 10

func stageWorkspaceImportArchiveUpload(appDir, jobID string, body io.Reader) (workspaceImportArchiveUpload, error) {
	directory := filepath.Join(appDir, workspaceImportDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return workspaceImportArchiveUpload{}, workspaceImportStorageError(err)
	}
	archiveKey := jobID + ".leafen.zip"
	archiveSHA, archiveBytes, err := stageWorkspaceImportArchiveBytes(directory, archiveKey, body)
	if err != nil {
		return workspaceImportArchiveUpload{}, err
	}
	return workspaceImportArchiveUpload{
		StorageKey: archiveKey,
		SHA256:     archiveSHA,
		Bytes:      archiveBytes,
	}, nil
}

func stageWorkspaceImportArchive(
	appDir, jobID, idSalt string,
	upload workspaceImportArchiveUpload,
	people []workspaceImportPerson,
	clockZone string,
) (workspaceImportArchiveStage, error) {
	directory := filepath.Join(appDir, workspaceImportDirectory)
	archivePath := filepath.Join(directory, upload.StorageKey)
	mappingPath := filepath.Join(directory, "."+jobID+".archive-map.tmp.sqlite")
	_ = os.Remove(mappingPath)
	mapping, err := sql.Open("sqlite", mappingPath)
	if err != nil {
		return workspaceImportArchiveStage{}, workspaceImportStorageError(err)
	}
	defer func() {
		_ = mapping.Close()
		_ = os.Remove(mappingPath)
	}()
	if err := createWorkspaceImportArchiveMap(mapping, idSalt == workspaceImportKeepIDsSalt); err != nil {
		return workspaceImportArchiveStage{}, workspaceImportFailureAt(
			workspaceImportStorageError(err), "archive-map-create",
		)
	}
	if err := fillWorkspaceImportArchivePeople(mapping, people); err != nil {
		return workspaceImportArchiveStage{}, workspaceImportFailureAt(err, "archive-map-people")
	}
	if err := keepWorkspaceImportArchiveClock(mapping, clockZone); err != nil {
		return workspaceImportArchiveStage{}, workspaceImportFailureAt(workspaceImportStorageError(err), "archive-map-clock")
	}
	descriptor, files, err := validateAndInventoryWorkspaceImportArchive(archivePath, jobID, idSalt, mapping)
	if err != nil {
		return workspaceImportArchiveStage{}, workspaceImportFailureAt(err, "archive-inventory")
	}
	if err := materializeWorkspaceImportArchiveTexts(mapping); err != nil {
		_ = removeWorkspaceImportSourceFile(appDir, files.FileIndexStorageKey)
		return workspaceImportArchiveStage{}, workspaceImportFailureAt(err, "archive-text-materialize")
	}
	source, title, total, err := writeWorkspaceImportArchiveChunks(directory, jobID, mapping)
	if err != nil {
		_ = removeWorkspaceImportSourceFile(appDir, files.FileIndexStorageKey)
		return workspaceImportArchiveStage{}, workspaceImportFailureAt(err, "archive-transform")
	}
	source.ArchiveStorageKey = upload.StorageKey
	source.ArchiveSHA256 = upload.SHA256
	source.ArchiveBytes = upload.Bytes
	if files.ArchiveSHA256 != "" {
		source.ArchiveSHA256 = files.ArchiveSHA256
		source.ArchiveBytes = files.ArchiveBytes
	}
	source.FileIndexStorageKey = files.FileIndexStorageKey
	source.FileIndexSHA256 = files.FileIndexSHA256
	source.FileCount = files.FileCount
	source.FileBytes = files.FileBytes
	if descriptor.Streams["entities"].Records < 1 || total < 1 {
		_ = removeWorkspaceImportSourceFile(appDir, source.StorageKey)
		_ = removeWorkspaceImportSourceFile(appDir, source.FileIndexStorageKey)
		return workspaceImportArchiveStage{}, workspaceImportInputError()
	}
	return workspaceImportArchiveStage{
		Source: source, Title: title, TotalItems: total, Pages: workspaceImportArchivePages(descriptor),
		Losses: descriptor.Losses,
	}, nil
}

func stageWorkspaceImportArchiveBytes(directory, storageKey string, body io.Reader) (string, int64, error) {
	finalPath := filepath.Join(directory, storageKey)
	temporaryPath := filepath.Join(directory, "."+storageKey+".tmp")
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", 0, workspaceImportStorageError(err)
	}
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, workspaceImportStorageError(err)
	}
	defer os.Remove(temporaryPath)
	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(io.MultiWriter(file, hasher), body, make([]byte, workspaceExportReaderWindowBytes))
	closeErr := errors.Join(file.Sync(), file.Close())
	if copyErr != nil || closeErr != nil || written < 1 {
		return "", 0, workspaceImportStorageError(errors.Join(copyErr, closeErr))
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", 0, workspaceImportStorageError(err)
	}
	if err := syncDirectory(directory); err != nil {
		_ = os.Remove(finalPath)
		return "", 0, workspaceImportStorageError(err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

// workspaceImportArchiveViewContainer is a staged view's container: the
// database block that owns it, or empty for a view of its source's own.
const workspaceImportArchiveViewContainer = `COALESCE(json_extract(value_json, '$.containerId'), '')`

// workspaceImportArchiveViewContainerIndex finds a container's views, of one
// data source or all, in their order, without reading the other views.
const workspaceImportArchiveViewContainerIndex = `
	CREATE INDEX IF NOT EXISTS entities_view_container ON entities (kind, ` + workspaceImportArchiveViewContainer + `,
		json_extract(value_json, '$.dataSourceId'), json_extract(value_json, '$.position'), source_id);
`

// createWorkspaceImportArchiveMap makes an import's map; one that keeps the
// bundle's IDs maps a page and its document to the same ID.
func createWorkspaceImportArchiveMap(db *sql.DB, keepIDs bool) error {
	targetUnique := " UNIQUE"
	if keepIDs {
		targetUnique = ""
	}
	_, err := db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE entities (
			sequence INTEGER PRIMARY KEY,
			kind TEXT NOT NULL,
			source_id TEXT NOT NULL,
			page_id TEXT NOT NULL,
			parent_id TEXT NOT NULL,
			position TEXT NOT NULL,
			value_json TEXT NOT NULL
		);
		CREATE INDEX entities_kind_page ON entities (kind, page_id, sequence);
		CREATE INDEX entities_kind_sequence ON entities (kind, sequence);
		CREATE INDEX entities_block_order ON entities (kind, page_id, parent_id, position, source_id);
		CREATE INDEX entities_kind_parent ON entities (kind, parent_id, sequence);
		CREATE INDEX entities_kind_source ON entities (kind, source_id);
		CREATE INDEX entities_row_block_order
			ON entities (kind, json_extract(value_json, '$.rowId'), parent_id, position, source_id);
		` + workspaceImportArchiveViewContainerIndex + `
		CREATE TABLE mappings (
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL` + targetUnique + `,
			kind TEXT NOT NULL,
			PRIMARY KEY (kind, source_id)
		);
		CREATE TABLE ygo_pages (page_id TEXT PRIMARY KEY);
		CREATE TABLE texts (block_id TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE text_deltas (block_id TEXT PRIMARY KEY, delta_json TEXT NOT NULL);
		` + workspaceImportArchivePeopleTable + `
		CREATE TABLE files (
			source_id TEXT PRIMARY KEY,
			target_id TEXT NOT NULL UNIQUE,
			offset INTEGER NOT NULL UNIQUE,
			name TEXT NOT NULL,
			content_type TEXT NOT NULL,
			scope TEXT NOT NULL,
			bytes INTEGER NOT NULL,
			sha256 TEXT NOT NULL,
			reference_count INTEGER NOT NULL DEFAULT 0
		);
	`)
	return err
}

// workspaceImportArchiveIDSalt is what an import makes its new IDs from:
// the job, or for another tool's bundle going in for a migration the
// migration connection, so the tool's next bundle, put in a new workspace
// for the same migration, makes the same IDs. A connection whose source was
// made before pages took page IDs (its job's payload doesn't mark PageIDs)
// keeps the salt and the 15 digits it gave its pages then.
func workspaceImportArchiveIDSalt(jobID string, payload workspaceImportJobPayload) string {
	if payload.KeepIDs {
		return workspaceImportKeepIDsSalt
	}
	if payload.BundleConnectionID != "" && payload.PageIDs {
		return workspaceImportBundlePageIDsSaltPrefix + payload.BundleConnectionID
	}
	if payload.BundleConnectionID != "" {
		return workspaceImportOlderBundleSaltPrefix + payload.BundleConnectionID
	}
	return jobID
}

const (
	workspaceImportOlderBundleSaltPrefix   = "bundle:"
	workspaceImportBundlePageIDsSaltPrefix = "bundle-page-ids:"
)

// workspaceImportArchivePageKind is whether an archive's item of the kind
// is a page.
func workspaceImportArchivePageKind(kind string) bool {
	return kind == workspaceExportItemPage || kind == workspaceExportItemRow || kind == workspaceExportItemTemplate
}

// workspaceImportKeepIDsSalt is the salt of an import that keeps the
// bundle's IDs, a workspace moved from another Leafen server.
const workspaceImportKeepIDsSalt = "\x00keep-ids"

// workspaceImportArchiveFileSalt is what an import makes its files' IDs from:
// the job, or none when it keeps them.
func workspaceImportArchiveFileSalt(jobID string, payload workspaceImportJobPayload) string {
	if payload.KeepIDs {
		return workspaceImportKeepIDsSalt
	}
	return jobID
}

func workspaceImportArchiveMappedID(jobID, kind, sourceID string) string {
	if jobID == workspaceImportKeepIDsSalt {
		return sourceID
	}
	if kind == workspaceExportItemBlock || kind == workspaceExportItemBlockTrash ||
		kind == workspaceExportItemCustomEmoji || kind == workspaceExportItemRowPageBlock {
		result := uuid.NewSHA1(uuid.NameSpaceOID, []byte(jobID+"\x00"+kind+"\x00"+sourceID)).String()
		if result == sourceID {
			result = uuid.NewSHA1(uuid.NameSpaceOID, []byte(jobID+"\x00remap\x00"+kind+"\x00"+sourceID)).String()
		}
		return result
	}
	// A page, a database's row and a template are pages, whose IDs are a
	// page's (derivedPageID). A migration connection that began before keeps
	// the 15 digits its pages had, by which the migration run again finds
	// them in the tool's next bundle (workspaceImportArchiveIDSalt).
	if workspaceImportArchivePageKind(kind) && !strings.HasPrefix(jobID, workspaceImportOlderBundleSaltPrefix) {
		result := derivedPageID("workspace-import-archive", jobID, kind, sourceID)
		if result == sourceID {
			result = derivedPageID("workspace-import-archive", jobID, "remap", kind, sourceID)
		}
		return result
	}
	digest := sha256.Sum256([]byte(jobID + "\x00" + kind + "\x00" + sourceID))
	result := hex.EncodeToString(digest[:])[:15]
	if result == sourceID {
		digest = sha256.Sum256([]byte(jobID + "\x00remap\x00" + kind + "\x00" + sourceID))
		result = hex.EncodeToString(digest[:])[:15]
	}
	return result
}

func validateAndInventoryWorkspaceImportArchive(
	path, jobID, idSalt string,
	mapping *sql.DB,
) (workspaceExportArchiveDescriptor, storedWorkspaceImportSource, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, workspaceImportInvalid("the bundle isn't a ZIP file")
	}
	defer archive.Close()
	expected := []string{
		workspaceExportEntitiesPath, workspaceExportFilesIndexPath,
		workspaceExportFilesDataPath, workspaceExportDescriptorPath,
	}
	layout := workspaceImportInvalid("the bundle holds exactly " + strings.Join(expected, ", ") +
		", in that order, each stored (not compressed) and not encrypted")
	if len(archive.File) != len(expected) {
		return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, layout
	}
	entries := map[string]*zip.File{}
	for index, entry := range archive.File {
		if entry.Name != expected[index] || entry.Method != zip.Store || entry.FileInfo().IsDir() || entry.Flags&1 != 0 {
			return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, layout
		}
		entries[entry.Name] = entry
	}
	descriptorBytes, err := readWorkspaceImportArchiveEntry(entries[workspaceExportDescriptorPath], workspaceImportArchiveDescriptorBytes)
	if err != nil {
		return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, err
	}
	descriptor := workspaceExportArchiveDescriptor{}
	if json.Unmarshal(descriptorBytes, &descriptor) != nil || !validWorkspaceImportArchiveDescriptor(descriptor, entries) {
		return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, workspaceImportInvalid(workspaceExportDescriptorPath +
			" isn't a descriptor this Leafen reads: its format, version, streams (records, bytes and SHA-256 of each entry) or counts")
	}
	files, err := inventoryWorkspaceImportArchiveFiles(
		descriptor,
		entries[workspaceExportFilesIndexPath],
		entries[workspaceExportFilesDataPath],
		jobID, workspaceImportArchiveStageFileSalt(jobID, idSalt),
		filepath.Dir(path),
		mapping,
	)
	if err != nil {
		return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, err
	}
	if err := inventoryWorkspaceImportArchiveEntities(descriptor, entries[workspaceExportEntitiesPath], idSalt, mapping); err != nil {
		_ = removeWorkspaceImportSourceFile(filepath.Dir(filepath.Dir(path)), files.FileIndexStorageKey)
		return workspaceExportArchiveDescriptor{}, storedWorkspaceImportSource{}, err
	}
	return descriptor, files, nil
}

// workspaceImportArchivePreviousFormatVersion is the archive made before
// database automations went into it, which still comes back without them.
const workspaceImportArchivePreviousFormatVersion = 10

func workspaceImportArchiveInputFormat(version int) string {
	switch version {
	case workspaceExportArchiveFormatVersion, workspaceImportArchiveRowTrashRowFormFormatVersion,
		workspaceImportArchiveLayoutNamedViewsFormatVersion, workspaceImportArchivePreviousFormatVersion:
		return workspaceArchiveInputFormatV2
	default:
		return ""
	}
}

func readWorkspaceImportArchiveEntry(entry *zip.File, limit int64) ([]byte, error) {
	if entry == nil || entry.UncompressedSize64 > uint64(limit) {
		return nil, workspaceImportInputError()
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, workspaceImportInputError()
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit || uint64(len(data)) != entry.UncompressedSize64 {
		return nil, workspaceImportInputError()
	}
	return data, nil
}

func validWorkspaceImportArchiveDescriptor(descriptor workspaceExportArchiveDescriptor, entries map[string]*zip.File) bool {
	if descriptor.Format != workspaceExportArchiveFormat ||
		workspaceImportArchiveInputFormat(descriptor.FormatVersion) != workspaceArchiveInputFormatV2 ||
		descriptor.GeneratedAt == "" || !validWorkspaceExportArchiveScope(descriptor.Scope) || descriptor.Source.WorkspaceID == "" ||
		len(descriptor.Source.WorkspaceName) > maxGraphTitleRunes || descriptor.Counts == nil || descriptor.Streams == nil ||
		!validWorkspaceBundleLosses(descriptor.Losses) {
		return false
	}
	countKeys := []string{
		"pages", "blocks", "databases", "dataSources", "dbProperties", "dbViews", "databaseRows", "ygoDocuments", "dbTemplates",
		"comments", "databaseContainers", "blockTrashTombstones", "customEmojis", "rowPageBlocks", "rowBlockTrashTombstones",
		"teamspaces",
	}
	if descriptor.FormatVersion > workspaceImportArchivePreviousFormatVersion {
		countKeys = append(countKeys, "dbAutomations")
	}
	for _, key := range countKeys {
		if descriptor.Counts[key] < 0 {
			return false
		}
	}
	if len(descriptor.Counts) != len(countKeys) || descriptor.Counts["databases"] != descriptor.Counts["dataSources"] {
		return false
	}
	for name, path := range map[string]string{
		"entities": workspaceExportEntitiesPath, "fileIndex": workspaceExportFilesIndexPath, "fileData": workspaceExportFilesDataPath,
	} {
		stream, ok := descriptor.Streams[name]
		entry := entries[path]
		if !ok || entry == nil || stream.Path != path || stream.Records < 0 || stream.Bytes < 0 ||
			uint64(stream.Bytes) != entry.UncompressedSize64 || len(stream.SHA256) != 64 {
			return false
		}
		if _, err := hex.DecodeString(stream.SHA256); err != nil {
			return false
		}
	}
	return len(descriptor.Streams) == 3
}

func validWorkspaceExportArchiveScope(scope workspaceExportArchiveScope) bool {
	if scope.Kind == workspaceExportScopeWorkspace {
		return true
	}
	return scope.Kind == "subtree" && len(scope.RootIDs) == 1 && strings.TrimSpace(scope.RootIDs[0]) != ""
}

func jsonNumberInt(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number != float64(int(number)) {
		return 0, false
	}
	return int(number), true
}

func materializeWorkspaceImportArchiveTexts(db *sql.DB) error {
	rows, err := db.Query(`SELECT value_json FROM entities WHERE kind = ? ORDER BY sequence`, workspaceExportItemYgo)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		value := archiveYgoValue{}
		if json.Unmarshal([]byte(encoded), &value) != nil || value.FormatVersion != ygoDocumentFormatVersion {
			return workspaceImportInputError()
		}
		document := crdt.New()
		if value.StateHex != "" {
			state, decodeErr := hex.DecodeString(value.StateHex)
			if decodeErr != nil || crdt.ApplyUpdateV1(document, state, nil) != nil {
				return workspaceImportInputError()
			}
		}
		if value.StructuralStateHex != "" {
			state, decodeErr := hex.DecodeString(value.StructuralStateHex)
			if decodeErr != nil || crdt.ApplyUpdateV1(crdt.New(), state, nil) != nil {
				return workspaceImportInputError()
			}
		}
		blocks, queryErr := db.Query(
			`SELECT source_id FROM entities WHERE kind = ? AND page_id = ? ORDER BY sequence`,
			workspaceExportItemBlock, value.PageID,
		)
		if queryErr != nil {
			return workspaceImportStorageError(queryErr)
		}
		for blocks.Next() {
			var blockID string
			if blocks.Scan(&blockID) != nil {
				blocks.Close()
				return workspaceImportInputError()
			}
			if err := stageWorkspaceImportArchiveBlockText(db, document, blockID); err != nil {
				blocks.Close()
				return workspaceImportStorageError(err)
			}
		}
		if err := blocks.Close(); err != nil {
			return workspaceImportStorageError(err)
		}
	}
	return rows.Err()
}

// workspaceImportArchiveStageFileSalt is the files' salt of a stage made with
// idSalt.
func workspaceImportArchiveStageFileSalt(jobID, idSalt string) string {
	if idSalt == workspaceImportKeepIDsSalt {
		return idSalt
	}
	return jobID
}
