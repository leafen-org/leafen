package app

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	workspaceExportDirectory      = "workspace-exports"
	workspaceExportZipContentType = "application/zip"
)

var workspaceExportIDPattern = regexp.MustCompile(`^workspace-export-[A-Za-z0-9_-]{15}$`)

type storedWorkspaceExportChunk struct {
	Index     int    `db:"chunk_index"`
	ItemCount int    `db:"item_count"`
	DataJSON  string `db:"data_json"`
	SHA256    string `db:"sha256"`
}

func workspaceExportStorageFailure(err error) error {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return newWorkspaceExportFailureWithCause(errorCodeWorkspaceExportStorageUnavailable, err)
	}
	return err
}

func buildWorkspaceExportArtifact(
	app core.App,
	job storedJob,
	payload workspaceExportJobPayload,
) (storedWorkspaceExportArtifact, string, error) {
	if !workspaceExportIDPattern.MatchString(job.ID) {
		return storedWorkspaceExportArtifact{}, "", newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	directory := filepath.Join(app.DataDir(), workspaceExportDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	temporaryPath := filepath.Join(directory, "."+job.ID+".tmp")
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	temporary, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	defer os.Remove(temporaryPath)

	contentType := workspaceExportArchiveMIME
	suffix := ".leafen.zip"
	if payload.Format == workspaceExportFormatNativeArchive {
		err = writeNativeWorkspaceExportV2(app, temporary, job, payload)
	} else if payload.Format == workspaceExportFormatPageNativeArchive {
		err = writeNativeWorkspaceExportV2(app, temporary, job, payload)
	} else if payload.Format == workspaceExportFormatPageMarkdown || payload.Format == workspaceExportFormatNotionMarkdown {
		contentType = "text/markdown; charset=utf-8"
		suffix = ".md"
		err = writeWorkspaceExportMarkdown(app, temporary, job, payload)
	} else if payload.Format == workspaceExportFormatDatabaseCSV {
		contentType = "text/csv; charset=utf-8"
		suffix = ".csv"
		err = writeWorkspaceExportDatabaseCSV(app, temporary, job, payload)
	} else if payload.Format == workspaceExportFormatMarkdownCSV || payload.Format == workspaceExportFormatHTML {
		contentType = workspaceExportZipContentType
		suffix = "-markdown-csv.zip"
		if payload.NotionAdmin != nil && !notionAdminSpaceExportArtifactSupported(*payload.NotionAdmin) {
			err = newWorkspaceExportFailure(errorCodeWorkspaceExportFormatUnsupported)
		} else if workspaceExportDocumentMode(payload) == workspaceExportModeHTML {
			suffix = "-html.zip"
		}
		if err == nil {
			err = writeWorkspaceExportDocuments(app, temporary, job, payload)
		}
	} else if payload.Format == workspaceExportFormatPrintHTML {
		contentType = "text/html; charset=utf-8"
		suffix = "-print.html"
		err = writeWorkspaceExportDocuments(app, temporary, job, payload)
	} else {
		err = newWorkspaceExportFailure(errorCodeWorkspaceExportFormatUnsupported)
	}
	if err != nil {
		_ = temporary.Close()
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	if err := temporary.Close(); err != nil {
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	digest, bytes, err := fileSHA256(temporaryPath)
	if err != nil || bytes <= 0 {
		if err != nil {
			return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
		}
		return storedWorkspaceExportArtifact{}, "", newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	filename := job.ID + suffix
	finalPath := filepath.Join(directory, filename)
	artifact := storedWorkspaceExportArtifact{
		JobID: job.ID, WorkspaceID: job.WorkspaceID, Owner: job.Owner,
		Format: payload.Format, Filename: filename, ContentType: contentType,
		Bytes: bytes, SHA256: digest,
	}
	existingDigest, existingBytes, existingErr := fileSHA256(finalPath)
	if existingErr == nil {
		if existingBytes != bytes || existingDigest != digest {
			return storedWorkspaceExportArtifact{}, "", newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
		}
		if err := os.Remove(temporaryPath); err != nil {
			return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
		}
		if err := syncDirectory(directory); err != nil {
			return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
		}
		return artifact, finalPath, nil
	}
	if !errors.Is(existingErr, os.ErrNotExist) {
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(existingErr)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	if err := syncDirectory(directory); err != nil {
		_ = os.Remove(finalPath)
		return storedWorkspaceExportArtifact{}, "", workspaceExportStorageFailure(err)
	}
	return artifact, finalPath, nil
}

// deleteWorkspaceExportArtifactStorage keeps workspace-export path validation
// and durable directory synchronization inside the artifact storage owner.
// A missing file is already converged after response loss or restart.
func deleteWorkspaceExportArtifactStorage(
	app core.App,
	artifact storedWorkspaceExportArtifact,
) error {
	if filepath.Base(artifact.Filename) != artifact.Filename ||
		!strings.HasPrefix(artifact.Filename, artifact.JobID) {
		return newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	directory := filepath.Join(app.DataDir(), workspaceExportDirectory)
	path := filepath.Join(directory, artifact.Filename)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return workspaceExportStorageFailure(err)
	}
	return workspaceExportStorageFailure(syncDirectory(directory))
}

func notionAdminSpaceExportArtifactSupported(metadata notionAdminSpaceExportJobMetadata) bool {
	if metadata.ExportFormat != "markdown" && metadata.ExportFormat != "html" {
		return false
	}
	request := metadata.Request
	return (request.IncludeComments == nil || !*request.IncludeComments) &&
		(request.CollectionViewExportType == nil || *request.CollectionViewExportType == "all") &&
		len(request.TeamspaceIDs) == 0
}

func walkWorkspaceExportChunks(
	app core.App,
	job storedJob,
	visit func(workspaceExportInventoryChunk) error,
) error {
	afterIndex := 0
	expectedIndex := 1
	itemCount := 0
	for {
		chunks := []storedWorkspaceExportChunk{}
		err := app.DB().NewQuery(`
			SELECT [[chunk_index]], [[item_count]], [[data_json]], [[sha256]]
			FROM {{leafen_workspace_export_chunks}}
			WHERE [[job_id]] = {:jobId} AND [[chunk_index]] > {:afterIndex}
			ORDER BY [[chunk_index]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": job.ID, "afterIndex": afterIndex,
			"limit": workspaceExportArtifactChunkWindow,
		}).All(&chunks)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			break
		}
		for _, stored := range chunks {
			chunk, err := decodeWorkspaceExportChunk(stored, expectedIndex)
			if err != nil {
				return err
			}
			if err := visit(chunk); err != nil {
				return err
			}
			itemCount += len(chunk.Items)
			afterIndex = stored.Index
			expectedIndex++
		}
	}
	if expectedIndex-1 != job.ChunkCount || itemCount != job.Total {
		return newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	return nil
}

func decodeWorkspaceExportChunk(
	stored storedWorkspaceExportChunk,
	expectedIndex int,
) (workspaceExportInventoryChunk, error) {
	if stored.Index != expectedIndex || stored.ItemCount < 1 {
		return workspaceExportInventoryChunk{}, newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	digest := sha256.Sum256([]byte(stored.DataJSON))
	if hex.EncodeToString(digest[:]) != stored.SHA256 {
		return workspaceExportInventoryChunk{}, newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	var chunk workspaceExportInventoryChunk
	if err := json.Unmarshal([]byte(stored.DataJSON), &chunk); err != nil || len(chunk.Items) != stored.ItemCount {
		return workspaceExportInventoryChunk{}, newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
	}
	return chunk, nil
}

func writeWorkspaceExportZipEntry(archive *zip.Writer, name string, data []byte, modified time.Time) error {
	entry, err := archive.CreateHeader(workspaceExportZipHeader(name, modified))
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

func workspaceExportZipHeader(name string, modified time.Time) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Store, Flags: 0x0800}
	header.SetModTime(modified)
	return header
}

// workspaceExportZipTime is the time an export's zip entries carry: when the
// export was asked for, as Notion's export dates its files when it was made
// (seen 2026-11-21: its zip's entries at the export's time, in UTC), rather
// than 1980, which unpacked the files as made on 1 January 1980. It is the
// job's own time, so a job built again after a stop comes out byte for byte
// the same, as buildWorkspaceExportArtifact checks against the one it kept.
func workspaceExportZipTime(job storedJob) time.Time {
	if created := time.UnixMilli(job.Created).UTC(); created.Year() >= 1980 {
		return created
	}
	return time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
}
