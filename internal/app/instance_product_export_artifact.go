package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const instanceProductExportDirectory = "instance-product-exports"

var instanceProductExportIDPattern = regexp.MustCompile(`^instance-product-export-[A-Za-z0-9_-]{15}$`)

type storedInstanceProductExportChunk struct {
	Index     int    `db:"chunk_index"`
	ItemCount int    `db:"item_count"`
	DataJSON  string `db:"data_json"`
	SHA256    string `db:"sha256"`
}

type expiredInstanceProductExportArtifact struct {
	JobID    string `db:"job_id"`
	Filename string `db:"filename"`
}

type instanceProductExportErrorEnvelope struct {
	Error struct {
		Code                string `json:"code"`
		Message             string `json:"message"`
		Reference           string `json:"reference"`
		DiagnosticReference string `json:"diagnosticReference"`
	} `json:"error"`
}

func (r *durableJobRunner) publishInstanceProductExportArtifact(
	ctx context.Context,
	job storedJob,
	payload instanceProductExportJobPayload,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	administrator, err := isAdministrator(r.app, job.Owner)
	if err != nil {
		return err
	}
	if !administrator {
		return newInstanceProductExportFailure(errorCodeAdministratorNotFound)
	}
	artifact, path, err := buildInstanceProductExportArtifact(r.app, job, payload)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	err = r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		administrator, err := isAdministrator(txApp, current.Owner)
		if err != nil {
			return err
		}
		if !administrator {
			return newInstanceProductExportFailure(errorCodeAdministratorNotFound)
		}
		now := time.Now().UTC()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_instance_product_export_artifacts}}
				([[job_id]], [[owner]], [[format]], [[format_version]], [[filename]],
				 [[content_type]], [[bytes]], [[sha256]], [[created]])
			VALUES
				({:jobId}, {:owner}, {:format}, {:formatVersion}, {:filename},
				 {:contentType}, {:bytes}, {:sha256}, {:created})
			ON CONFLICT ([[job_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": current.ID, "owner": current.Owner,
			"format": artifact.Format, "formatVersion": artifact.FormatVersion,
			"filename": artifact.Filename, "contentType": artifact.ContentType,
			"bytes": artifact.Bytes, "sha256": artifact.SHA256, "created": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		payload.Phase = instanceProductExportPhaseCompleted
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'completed', [[payload_json]] = {:payload},
				[[processed_items]] = [[total_items]], [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{"id": current.ID, "payload": string(encoded), "updated": now.UnixMilli()}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err == nil && updated == 1 {
			keep = true
		}
		return err
	})
	return err
}

func buildInstanceProductExportArtifact(
	app core.App,
	job storedJob,
	payload instanceProductExportJobPayload,
) (storedInstanceProductExportArtifact, string, error) {
	if !instanceProductExportIDPattern.MatchString(job.ID) {
		return storedInstanceProductExportArtifact{}, "", newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
	}
	policies, coverage, err := loadInstanceProductExportPolicy(app)
	if err != nil {
		return storedInstanceProductExportArtifact{}, "", err
	}
	included := includedInstanceProductExportPolicies(policies)
	directory := filepath.Join(app.DataDir(), instanceProductExportDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	temporaryPath := filepath.Join(directory, "."+job.ID+".tmp")
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	defer os.Remove(temporaryPath)
	if err := writeInstanceProductExportDocument(file, app, job, payload, included, coverage); err != nil {
		_ = file.Close()
		return storedInstanceProductExportArtifact{}, "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	if err := file.Close(); err != nil {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	digest, bytes, err := fileSHA256(temporaryPath)
	if err != nil || bytes <= 0 {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	filename := "leafen-" + job.ID + ".json"
	finalPath := filepath.Join(directory, filename)
	if existingDigest, existingBytes, existingErr := fileSHA256(finalPath); existingErr == nil {
		if existingDigest != digest || existingBytes != bytes {
			return storedInstanceProductExportArtifact{}, "", newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
		}
		if err := os.Remove(temporaryPath); err != nil {
			return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
		}
	} else if !errors.Is(existingErr, os.ErrNotExist) {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(existingErr)
	} else if err := os.Rename(temporaryPath, finalPath); err != nil {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	if err := syncDirectory(directory); err != nil {
		return storedInstanceProductExportArtifact{}, "", instanceProductExportStorageFailure(err)
	}
	return storedInstanceProductExportArtifact{
		JobID: job.ID, Owner: job.Owner,
		Format: instanceProductExportFormat, FormatVersion: instanceProductExportFormatVersion,
		Filename: filename, ContentType: instanceProductExportContentType,
		Bytes: bytes, SHA256: digest,
	}, finalPath, nil
}

func writeInstanceProductExportDocument(
	writer io.Writer,
	app core.App,
	job storedJob,
	payload instanceProductExportJobPayload,
	policies []instanceProductExportTablePolicy,
	coverage instanceProductExportCoverage,
) error {
	if _, err := io.WriteString(writer, `{"format":"`+instanceProductExportFormat+`","formatVersion":1,"generatedAt":`); err != nil {
		return err
	}
	if err := json.NewEncoder(writer).Encode(payload.GeneratedAt); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, `,"notes":["This is a bounded product-data export, not a point-in-time whole-volume backup.","Authentication authority, credentials, operational state, and raw file bytes are excluded."],"coverage":`); err != nil {
		return err
	}
	if err := json.NewEncoder(writer).Encode(coverage); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, `,"tableCounts":`); err != nil {
		return err
	}
	if err := json.NewEncoder(writer).Encode(payload.TableCounts); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, `,"tables":{`); err != nil {
		return err
	}
	expectedChunk := 1
	for index, policy := range policies {
		if index > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		tableName, _ := json.Marshal(policy.Table)
		if _, err := writer.Write(tableName); err != nil {
			return err
		}
		if _, err := io.WriteString(writer, ":["); err != nil {
			return err
		}
		rows, nextChunk, err := writeInstanceProductExportTableRows(writer, app, job.ID, policy.Table, expectedChunk)
		if err != nil {
			return err
		}
		if rows != payload.TableCounts[policy.Table] {
			return newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
		}
		expectedChunk = nextChunk
		if _, err := io.WriteString(writer, "]"); err != nil {
			return err
		}
	}
	if expectedChunk-1 != job.ChunkCount {
		return newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
	}
	_, err := io.WriteString(writer, "}}\n")
	return err
}

func writeInstanceProductExportTableRows(
	writer io.Writer,
	app core.App,
	jobID, table string,
	expectedChunk int,
) (int, int, error) {
	afterIndex := 0
	rowCount := 0
	inRow := false
	for {
		chunks := []storedInstanceProductExportChunk{}
		err := app.DB().NewQuery(`
			SELECT [[chunk_index]], [[item_count]], [[data_json]], [[sha256]]
			FROM {{leafen_job_chunks}}
			WHERE [[job_id]] = {:jobId} AND [[cursor]] = {:table} AND [[chunk_index]] > {:afterIndex}
			ORDER BY [[chunk_index]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": jobID, "table": table, "afterIndex": afterIndex,
			"limit": instanceProductExportArtifactChunkWindow,
		}).All(&chunks)
		if err != nil {
			return 0, expectedChunk, err
		}
		if len(chunks) == 0 {
			break
		}
		for _, chunk := range chunks {
			if chunk.Index != expectedChunk || chunk.ItemCount < 0 {
				return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
			}
			envelope := instanceProductExportChunkEnvelope{}
			if json.Unmarshal([]byte(chunk.DataJSON), &envelope) != nil || envelope.Table != table {
				return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
			}
			fragment, err := base64.StdEncoding.DecodeString(envelope.Fragment)
			if err != nil {
				return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid, err)
			}
			digest := sha256.Sum256(fragment)
			if hex.EncodeToString(digest[:]) != chunk.SHA256 {
				return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
			}
			completed := 0
			for len(fragment) > 0 {
				newline := bytes.IndexByte(fragment, '\n')
				segment := fragment
				if newline >= 0 {
					segment = fragment[:newline]
				}
				if len(segment) > 0 {
					if !inRow {
						if rowCount > 0 {
							if _, err := io.WriteString(writer, ","); err != nil {
								return 0, expectedChunk, err
							}
						}
						inRow = true
					}
					if _, err := writer.Write(segment); err != nil {
						return 0, expectedChunk, err
					}
				}
				if newline >= 0 {
					if !inRow {
						return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
					}
					inRow = false
					rowCount++
					completed++
					fragment = fragment[newline+1:]
				} else {
					fragment = fragment[len(fragment):]
				}
			}
			if completed != chunk.ItemCount {
				return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
			}
			afterIndex = chunk.Index
			expectedChunk++
		}
	}
	if inRow {
		return 0, expectedChunk, newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
	}
	return rowCount, expectedChunk, nil
}

func instanceProductExportStorageFailure(err error) error {
	if err == nil {
		return newInstanceProductExportFailure(errorCodeInstanceProductExportArtifactInvalid)
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return newInstanceProductExportFailure(errorCodeInstanceProductExportStorageUnavailable, err)
	}
	return err
}

func cleanupExpiredInstanceProductExports(app core.App, now time.Time) error {
	expired := []expiredInstanceProductExportArtifact{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]], [[filename]]
		FROM {{leafen_instance_product_export_artifacts}}
		WHERE [[created]] < {:cutoff}
		ORDER BY [[created]], [[job_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"cutoff": now.Add(-instanceProductExportRetention).UnixMilli(),
		"limit":  instanceProductExportCleanupRows,
	}).All(&expired); err != nil {
		return err
	}
	for _, artifact := range expired {
		if filepath.Base(artifact.Filename) == artifact.Filename &&
			strings.HasPrefix(artifact.Filename, "leafen-instance-product-export-") {
			path := filepath.Join(app.DataDir(), instanceProductExportDirectory, artifact.Filename)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return instanceProductExportStorageFailure(err)
			}
		}
		if err := app.RunInTransaction(func(txApp core.App) error {
			for _, statement := range []string{
				`DELETE FROM {{leafen_job_chunks}} WHERE [[job_id]] = {:jobId}`,
				`DELETE FROM {{leafen_instance_product_export_artifacts}} WHERE [[job_id]] = {:jobId}`,
				`DELETE FROM {{leafen_jobs}} WHERE [[id]] = {:jobId} AND [[kind]] = {:kind}`,
			} {
				if _, err := txApp.DB().NewQuery(statement).Bind(dbx.Params{
					"jobId": artifact.JobID, "kind": instanceProductExportJobKind,
				}).Execute(); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *durableJobRunner) downloadInstanceProductExport(event *core.RequestEvent) error {
	user, ok := authorizeInstanceProductExportRequest(event)
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	job, err := r.instanceProductExportResponse(jobID, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeInstanceProductExportNotFound, "Product data export not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeInstanceProductExportReadFailed, "Couldn't read the product data export.", err)
	}
	if job.Status != instanceProductExportStatusCompleted {
		return apiError(event, http.StatusConflict, errorCodeInstanceProductExportNotReady, "Only a completed product data export can be downloaded.")
	}
	artifact, err := loadInstanceProductExportArtifact(event.App, jobID, user.Id)
	if err != nil || filepath.Base(artifact.Filename) != artifact.Filename || artifact.Format != instanceProductExportFormat || artifact.FormatVersion != instanceProductExportFormatVersion {
		return apiError(event, http.StatusConflict, errorCodeInstanceProductExportArtifactInvalid, "Couldn't check the product data export file.")
	}
	path := filepath.Join(event.App.DataDir(), instanceProductExportDirectory, artifact.Filename)
	digest, size, err := fileSHA256(path)
	if err != nil || size != artifact.Bytes || digest != artifact.SHA256 {
		return instanceProductExportArtifactAPIError(event, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodeInstanceProductExportArtifactInvalid, "Couldn't check the product data export file.")
	}
	defer file.Close()
	event.Response.Header().Set("Content-Type", artifact.ContentType)
	event.Response.Header().Set("Content-Disposition", contentDisposition("attachment", artifact.Filename))
	event.Response.Header().Set("Content-Length", strconv.FormatInt(artifact.Bytes, 10))
	event.Response.Header().Set("ETag", fmt.Sprintf("\"sha256:%s\"", artifact.SHA256))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	event.Response.Header().Set("Cache-Control", "private, no-store")
	event.Response.WriteHeader(http.StatusOK)
	_, err = io.Copy(event.Response, file)
	return err
}

func instanceProductExportArtifactAPIError(event *core.RequestEvent, cause error) error {
	reference := newDiagnosticReference()
	logAPIDiagnostic(
		event, http.StatusConflict, errorCodeInstanceProductExportArtifactInvalid, reference,
		"instance-product-export.download", "artifact-verify",
		errorCodeInstanceProductExportArtifactInvalid, cause, diagnosticSourcePosition(1),
	)
	response := instanceProductExportErrorEnvelope{}
	response.Error.Code = errorCodeInstanceProductExportArtifactInvalid
	response.Error.Message = "Couldn't check the product data export file."
	response.Error.Reference = reference
	response.Error.DiagnosticReference = reference
	return event.JSON(http.StatusConflict, response)
}
