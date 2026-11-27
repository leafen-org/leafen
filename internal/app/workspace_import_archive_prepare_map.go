package app

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	workspaceImportArchivePreparationSchemaVersion = 1
	workspaceImportArchivePreparationMapSuffix     = ".archive-map.sqlite"
	workspaceImportArchivePreparationOutputSuffix  = ".ndjson.prepare"
	workspaceImportArchivePreparationIndexSuffix   = ".files.ndjson.prepare"

	workspaceImportArchivePreparePhaseFileInventory   = "file_inventory"
	workspaceImportArchivePreparePhaseFileFinalize    = "file_finalize"
	workspaceImportArchivePreparePhaseEntityInventory = "entity_inventory"
	workspaceImportArchivePreparePhaseTextMaterialize = "text_materialize"
	workspaceImportArchivePreparePhaseTransform       = "transform"
	workspaceImportArchivePreparePhaseTransformFinish = "transform_finalize"
	workspaceImportArchivePreparePhaseDLP             = "dlp"
	workspaceImportArchivePreparePhasePublish         = "publish"
)

type workspaceImportArchivePreparationState struct {
	SchemaVersion int                              `json:"schemaVersion"`
	Phase         string                           `json:"phase"`
	Descriptor    workspaceExportArchiveDescriptor `json:"descriptor"`
	Files         storedWorkspaceImportSource      `json:"files"`

	FileIndexOffset     int64  `json:"fileIndexOffset"`
	FileDataOffset      int64  `json:"fileDataOffset"`
	FilePartOffset      int64  `json:"filePartOffset"`
	FileIndexHashState  string `json:"fileIndexHashState"`
	FileDataHashState   string `json:"fileDataHashState"`
	FileHashState       string `json:"fileHashState"`
	FileOutputHashState string `json:"fileOutputHashState"`
	FileOutputBytes     int64  `json:"fileOutputBytes"`
	FileChain           string `json:"fileChain"`
	FileCount           int    `json:"fileCount"`

	EntityOffset    int64          `json:"entityOffset"`
	EntitySequence  int            `json:"entitySequence"`
	EntityHashState string         `json:"entityHashState"`
	EntityCounts    map[string]int `json:"entityCounts"`

	TextYgoSequence   int `json:"textYgoSequence"`
	TextBlockSequence int `json:"textBlockSequence"`

	TransformPass       int      `json:"transformPass"`
	TransformPageCursor string   `json:"transformPageCursor"`
	TransformSequence   int      `json:"transformSequence"`
	OutputBytes         int64    `json:"outputBytes"`
	OutputSHA256        string   `json:"outputSha256"`
	OutputHashState     string   `json:"outputHashState"`
	OutputChunkHashes   []string `json:"outputChunkHashes"`
	OutputChunks        int      `json:"outputChunks"`
	Title               string   `json:"title"`
	TotalItems          int      `json:"totalItems"`

	DLPChunk    int   `json:"dlpChunk"`
	DLPOffset   int64 `json:"dlpOffset"`
	DLPRevision int   `json:"dlpRevision"`
}

func workspaceImportArchivePreparationMapKey(jobID string) string {
	return jobID + workspaceImportArchivePreparationMapSuffix
}

func workspaceImportArchivePreparationMapPath(appDir, jobID string) string {
	return filepath.Join(appDir, workspaceImportDirectory, workspaceImportArchivePreparationMapKey(jobID))
}

func workspaceImportArchivePreparationMapTemporaryKey(jobID string) string {
	return "." + workspaceImportArchivePreparationMapKey(jobID) + ".tmp"
}

func workspaceImportArchivePreparationMapTemporaryPath(appDir, jobID string) string {
	return filepath.Join(
		appDir, workspaceImportDirectory, workspaceImportArchivePreparationMapTemporaryKey(jobID),
	)
}

func workspaceImportArchivePreparationOutputPath(directory, jobID string) string {
	return filepath.Join(directory, "."+jobID+workspaceImportArchivePreparationOutputSuffix)
}

func workspaceImportArchivePreparationIndexPath(directory, jobID string) string {
	return filepath.Join(directory, "."+jobID+workspaceImportArchivePreparationIndexSuffix)
}

func createWorkspaceImportArchivePreparationMap(
	appDir, jobID, archivePath string,
	people []workspaceImportPerson,
	keepIDs bool,
	clockZone string,
) error {
	reader, entries, descriptor, err := openWorkspaceImportArchiveEnvelope(archivePath)
	if reader != nil {
		defer reader.Close()
	}
	if err != nil {
		return err
	}
	if err := validateWorkspaceImportArchiveFileEnvelope(descriptor, entries); err != nil {
		return err
	}
	directory := filepath.Join(appDir, workspaceImportDirectory)
	finalPath := workspaceImportArchivePreparationMapPath(appDir, jobID)
	temporaryPath := workspaceImportArchivePreparationMapTemporaryPath(appDir, jobID)
	_ = os.Remove(temporaryPath)
	db, err := sql.Open("sqlite", temporaryPath)
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-open")
	}
	db.SetMaxOpenConns(1)
	remove := true
	defer func() {
		_ = db.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA synchronous = FULL;`); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-options")
	}
	if err := createWorkspaceImportArchiveMap(db, keepIDs); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-create")
	}
	if err := fillWorkspaceImportArchivePeople(db, people); err != nil {
		return workspaceImportFailureAt(err, "archive-prepare-map-people")
	}
	if err := keepWorkspaceImportArchiveClock(db, clockZone); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-clock")
	}
	if _, err := db.Exec(`
		CREATE TABLE archive_preparation (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			schema_version INTEGER NOT NULL CHECK (schema_version = 1),
			state_json TEXT NOT NULL
		)
	`); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-schema")
	}
	state := workspaceImportArchivePreparationState{
		SchemaVersion:     workspaceImportArchivePreparationSchemaVersion,
		Phase:             workspaceImportArchivePreparePhaseFileInventory,
		Descriptor:        descriptor,
		EntityCounts:      emptyWorkspaceExportCounts(),
		TextYgoSequence:   -1,
		TextBlockSequence: -1,
		TransformSequence: -1,
		Title:             "Leafen archive",
		DLPChunk:          -1,
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return workspaceImportInputError()
	}
	if _, err := db.Exec(
		`INSERT INTO archive_preparation (id, schema_version, state_json) VALUES (1, ?, ?)`,
		workspaceImportArchivePreparationSchemaVersion, string(encoded),
	); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-state")
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-sync")
	}
	if err := db.Close(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-close")
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-finalize")
	}
	remove = false
	if err := syncDirectory(directory); err != nil {
		return err
	}
	return nil
}

func openWorkspaceImportArchiveEnvelope(
	path string,
) (*zip.ReadCloser, map[string]*zip.File, workspaceExportArchiveDescriptor, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, workspaceExportArchiveDescriptor{}, workspaceImportInvalid("the bundle isn't a ZIP file")
	}
	expected := []string{
		workspaceExportEntitiesPath, workspaceExportFilesIndexPath,
		workspaceExportFilesDataPath, workspaceExportDescriptorPath,
	}
	layout := workspaceImportInvalid("the bundle holds exactly " + strings.Join(expected, ", ") +
		", in that order, each stored (not compressed) and not encrypted")
	if len(archive.File) != len(expected) {
		archive.Close()
		return nil, nil, workspaceExportArchiveDescriptor{}, layout
	}
	entries := make(map[string]*zip.File, len(expected))
	for index, entry := range archive.File {
		if entry.Name != expected[index] || entry.Method != zip.Store ||
			entry.FileInfo().IsDir() || entry.Flags&1 != 0 {
			archive.Close()
			return nil, nil, workspaceExportArchiveDescriptor{}, layout
		}
		entries[entry.Name] = entry
	}
	descriptorBytes, err := readWorkspaceImportArchiveEntry(
		entries[workspaceExportDescriptorPath], workspaceImportArchiveDescriptorBytes,
	)
	descriptor := workspaceExportArchiveDescriptor{}
	if err != nil || json.Unmarshal(descriptorBytes, &descriptor) != nil ||
		!validWorkspaceImportArchiveDescriptor(descriptor, entries) {
		archive.Close()
		return nil, nil, workspaceExportArchiveDescriptor{}, workspaceImportInvalid(workspaceExportDescriptorPath +
			" isn't a descriptor this Leafen reads: its format, version, streams (records, bytes and SHA-256 of each entry) or counts")
	}
	return archive, entries, descriptor, nil
}

func openWorkspaceImportArchivePreparationMap(
	appDir, jobID string,
) (*sql.DB, workspaceImportArchivePreparationState, error) {
	path := workspaceImportArchivePreparationMapPath(appDir, jobID)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, workspaceImportArchivePreparationState{}, workspaceImportStorageErrorAt(err, "archive-prepare-map-open")
	}
	db.SetMaxOpenConns(1)
	// A map created before text deltas were staged gains the table here, and
	// one created before views were found by their container the index.
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA synchronous = FULL;` + workspaceImportArchiveTextDeltaTable +
		workspaceImportArchiveViewContainerIndex); err != nil {
		_ = db.Close()
		return nil, workspaceImportArchivePreparationState{}, workspaceImportStorageErrorAt(err, "archive-prepare-map-options")
	}
	state, err := loadWorkspaceImportArchivePreparationState(db)
	if err != nil {
		_ = db.Close()
		return nil, workspaceImportArchivePreparationState{}, err
	}
	return db, state, nil
}

func loadWorkspaceImportArchivePreparationState(
	query interface{ QueryRow(string, ...any) *sql.Row },
) (workspaceImportArchivePreparationState, error) {
	var version int
	var encoded string
	if err := query.QueryRow(
		`SELECT schema_version, state_json FROM archive_preparation WHERE id = 1`,
	).Scan(&version, &encoded); err != nil {
		return workspaceImportArchivePreparationState{}, workspaceImportStateErrorAt("archive-prepare-map-state")
	}
	state := workspaceImportArchivePreparationState{}
	if version != workspaceImportArchivePreparationSchemaVersion ||
		json.Unmarshal([]byte(encoded), &state) != nil || state.SchemaVersion != version || state.Phase == "" {
		return workspaceImportArchivePreparationState{}, workspaceImportStateErrorAt("archive-prepare-map-version")
	}
	return state, nil
}

func saveWorkspaceImportArchivePreparationState(
	tx *sql.Tx,
	state workspaceImportArchivePreparationState,
) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return workspaceImportInputError()
	}
	result, err := tx.Exec(
		`UPDATE archive_preparation SET state_json = ? WHERE id = 1 AND schema_version = ?`,
		string(encoded), workspaceImportArchivePreparationSchemaVersion,
	)
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-update")
	}
	updated, _ := result.RowsAffected()
	if updated != 1 {
		return workspaceImportStateErrorAt("archive-prepare-map-version")
	}
	return nil
}

func workspaceImportArchivePreparationMapExists(appDir, jobID string) (bool, error) {
	_, err := os.Stat(workspaceImportArchivePreparationMapPath(appDir, jobID))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, workspaceImportStorageErrorAt(err, "archive-prepare-map-stat")
}
