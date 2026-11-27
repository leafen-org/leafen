package app

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

const workspaceImportArchiveFileIndexLineBytes = 16 << 10

func loadWorkspaceImportArchiveFile(
	appDir string,
	source storedWorkspaceImportSource,
	payload workspaceImportJobPayload,
) (workspaceImportArchiveFile, int64, error) {
	if source.FileCount < 1 || payload.ArchiveFileIndex < 0 ||
		payload.ArchiveFileIndex >= source.FileCount || payload.ArchiveFileCursor < 0 {
		return workspaceImportArchiveFile{}, 0, workspaceImportStateErrorAt("archive-file-index-state")
	}
	path := filepath.Join(appDir, workspaceImportDirectory, source.FileIndexStorageKey)
	file, err := os.Open(path)
	if err != nil {
		return workspaceImportArchiveFile{}, 0, workspaceImportStorageError(err)
	}
	defer file.Close()
	if _, err := file.Seek(payload.ArchiveFileCursor, io.SeekStart); err != nil {
		return workspaceImportArchiveFile{}, 0, workspaceImportStorageError(err)
	}
	reader := bufio.NewReader(io.LimitReader(file, workspaceImportArchiveFileIndexLineBytes+1))
	line, err := reader.ReadBytes('\n')
	if err != nil || len(line) < 2 || len(line) > workspaceImportArchiveFileIndexLineBytes || line[len(line)-1] != '\n' {
		return workspaceImportArchiveFile{}, 0, workspaceImportStateErrorAt("archive-file-index-line")
	}
	record := workspaceImportArchiveFile{}
	if json.Unmarshal(line[:len(line)-1], &record) != nil ||
		record.TargetID != workspaceImportArchiveMappedID(
			workspaceImportArchiveFileSalt(source.JobID, payload), workspaceImportArchiveFileKind, record.SourceID,
		) || record.Offset < 0 || record.Bytes < 1 || !sha256Pattern.MatchString(record.SHA256) ||
		!sha256Pattern.MatchString(record.ChainSHA256) {
		return workspaceImportArchiveFile{}, 0, workspaceImportStateErrorAt("archive-file-index-record")
	}
	expectedChain := record.ChainSHA256
	record.ChainSHA256 = ""
	canonical, err := json.Marshal(record)
	if err != nil {
		return workspaceImportArchiveFile{}, 0, workspaceImportStateErrorAt("archive-file-index-record")
	}
	digest := sha256.Sum256(append([]byte(payload.ArchiveFileChain), canonical...))
	if hex.EncodeToString(digest[:]) != expectedChain {
		return workspaceImportArchiveFile{}, 0, workspaceImportStateErrorAt("archive-file-index-chain")
	}
	nextCursor := payload.ArchiveFileCursor + int64(len(line))
	if payload.ArchiveFileIndex == source.FileCount-1 {
		info, statErr := file.Stat()
		if statErr != nil {
			return workspaceImportArchiveFile{}, 0, workspaceImportStorageError(statErr)
		}
		if info.Size() != nextCursor {
			return workspaceImportArchiveFile{}, 0, workspaceImportStateErrorAt("archive-file-index-tail")
		}
	}
	record.ChainSHA256 = expectedChain
	return record, nextCursor, nil
}

func openWorkspaceImportArchiveFileData(
	appDir string,
	source storedWorkspaceImportSource,
	record workspaceImportArchiveFile,
) (filesystem.FileReader, error) {
	archivePath := filepath.Join(appDir, workspaceImportDirectory, source.ArchiveStorageKey)
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, workspaceImportStateErrorAt("archive-file-data-open")
	}
	var dataEntry *zip.File
	for _, entry := range archive.File {
		if entry.Name == workspaceExportFilesDataPath {
			dataEntry = entry
			break
		}
	}
	if dataEntry == nil || dataEntry.Method != zip.Store ||
		record.Offset > int64(dataEntry.UncompressedSize64) ||
		record.Bytes > int64(dataEntry.UncompressedSize64)-record.Offset {
		archive.Close()
		return nil, workspaceImportStateErrorAt("archive-file-data-range")
	}
	dataOffset, err := dataEntry.DataOffset()
	if err != nil {
		archive.Close()
		return nil, workspaceImportStateErrorAt("archive-file-data-offset")
	}
	if err := archive.Close(); err != nil {
		return nil, workspaceImportStorageError(err)
	}
	return workspaceImportArchiveFileReader{
		path: archivePath, offset: dataOffset + record.Offset, size: record.Bytes,
	}, nil
}

type workspaceImportArchiveFileReader struct {
	path   string
	offset int64
	size   int64
}

func (r workspaceImportArchiveFileReader) Open() (io.ReadSeekCloser, error) {
	file, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	return &workspaceImportArchiveSectionReader{
		SectionReader: io.NewSectionReader(file, r.offset, r.size), file: file,
	}, nil
}

type workspaceImportArchiveSectionReader struct {
	*io.SectionReader
	file *os.File
}

func (r *workspaceImportArchiveSectionReader) Close() error {
	return r.file.Close()
}

func (r *durableJobRunner) publishWorkspaceArchiveImportFile(
	job storedJob,
	payload workspaceImportJobPayload,
	source storedWorkspaceImportSource,
) error {
	record, nextCursor, err := loadWorkspaceImportArchiveFile(r.app.DataDir(), source, payload)
	if err != nil {
		return err
	}
	input := fileUploadPrepareInput{
		RequestID:   "workspace-archive-file-" + job.ID + "-" + strconv.Itoa(payload.ArchiveFileIndex),
		WorkspaceID: job.WorkspaceID,
		Name:        record.Name,
		Size:        record.Bytes,
		ContentType: record.ContentType,
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return workspaceImportStateErrorAt("archive-file-metadata")
	}
	upload, _, err := admitDirectProductFileUploadWithID(
		r.app, job.Owner, input, record.SHA256, record.TargetID,
	)
	if err != nil {
		return workspaceImportFailureAt(err, "archive-file-admit")
	}
	var stored *core.Record
	if upload.Status == fileUploadStatusCompleted {
		stored, err = completedDirectProductFileUpload(r.app, upload)
	} else {
		reader, openErr := openWorkspaceImportArchiveFileData(r.app.DataDir(), source, record)
		if openErr != nil {
			return openErr
		}
		file := &filesystem.File{
			Reader: reader, Name: "payload.bin", OriginalName: record.Name, Size: record.Bytes,
		}
		stored, err = publishDirectProductFileUpload(r.app, upload, file, record.SHA256)
	}
	if err != nil || stored == nil || stored.Id != record.TargetID ||
		stored.GetString("sha256") != record.SHA256 ||
		!workspaceFileRecordBelongsTo(stored, job.WorkspaceID) {
		return workspaceImportFailureAt(errors.Join(err, errProductFileIntegrity), "archive-file-publish")
	}
	previousIndex := payload.ArchiveFileIndex
	previousCursor := payload.ArchiveFileCursor
	previousChain := payload.ArchiveFileChain
	payload.SourceVerified = true
	payload.ArchiveFileIndex++
	payload.ArchiveFileCursor = nextCursor
	payload.ArchiveFileChain = record.ChainSHA256
	payload.ArchiveFilesReady = payload.ArchiveFileIndex == source.FileCount
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ArchiveFileIndex != previousIndex ||
			currentPayload.ArchiveFileCursor != previousCursor ||
			currentPayload.ArchiveFileChain != previousChain {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-file-frontier")
		}
		payload.CancelRequested = currentPayload.CancelRequested
		return r.updateWorkspaceImportPayload(
			txApp, current, payload, current.Cursor, current.Processed, true,
		)
	})
}
