package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type instanceProductExportFailure struct {
	code  string
	cause error
}

func (failure *instanceProductExportFailure) Error() string {
	if failure.cause == nil {
		return failure.code
	}
	return failure.code + ": " + failure.cause.Error()
}

func (failure *instanceProductExportFailure) Unwrap() error { return failure.cause }

type instanceProductExportChunkEnvelope struct {
	Table    string `json:"table"`
	Fragment string `json:"fragment"`
}

func newInstanceProductExportFailure(code string, cause ...error) error {
	var wrapped error
	if len(cause) > 0 {
		wrapped = cause[0]
	}
	return &instanceProductExportFailure{code: code, cause: wrapped}
}

func stableInstanceProductExportFailureCode(err error) string {
	if errors.Is(err, errInstanceProductExportPolicyIncomplete) {
		return errorCodeInstanceProductExportPolicyIncomplete
	}
	failure := &instanceProductExportFailure{}
	if errors.As(err, &failure) {
		return failure.code
	}
	return ""
}

func (r *durableJobRunner) applyInstanceProductExportChunk(ctx context.Context, job storedJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload := instanceProductExportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil || payload.FormatVersion != instanceProductExportFormatVersion {
		return r.failInstanceProductExportJob(job, errorCodeInstanceProductExportCreateFailed, errors.New("invalid product export payload"))
	}
	var err error
	switch payload.Phase {
	case instanceProductExportPhaseCapture:
		err = r.captureInstanceProductExportChunk(job, payload)
	case instanceProductExportPhasePublish:
		err = r.publishInstanceProductExportArtifact(ctx, job, payload)
	default:
		err = newInstanceProductExportFailure(errorCodeInstanceProductExportCreateFailed)
	}
	if err == nil {
		return nil
	}
	code := stableInstanceProductExportFailureCode(err)
	if isSQLiteBusyError(err) {
		code = errorCodeInstanceProductExportDatabaseBusy
	}
	if code == "" {
		code = errorCodeInstanceProductExportCreateFailed
	}
	return r.failInstanceProductExportJob(job, code, err)
}

func (r *durableJobRunner) captureInstanceProductExportChunk(job storedJob, payload instanceProductExportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(current.PayloadJSON), &payload); err != nil {
			return err
		}
		administrator, err := isAdministrator(txApp, current.Owner)
		if err != nil {
			return err
		}
		if !administrator {
			return newInstanceProductExportFailure(errorCodeAdministratorNotFound)
		}
		policies, err := r.cachedInstanceProductExportPolicies(txApp)
		if err != nil {
			return err
		}
		included := includedInstanceProductExportPolicies(policies)
		cursor := instanceProductExportCaptureCursor{RowID: payload.RowID}
		if payload.Capture != nil {
			cursor = *payload.Capture
			if cursor.RowID != payload.RowID {
				return newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
			}
		}
		for payload.TableIndex < len(included) {
			policy := included[payload.TableIndex]
			batch, err := readInstanceProductExportRows(
				txApp, policy, cursor,
				instanceProductExportFragmentTargetBytes,
				instanceProductExportItemsPerTurn,
				current.ID,
			)
			if err != nil {
				return err
			}
			if payload.TableCounts == nil {
				payload.TableCounts = map[string]int{}
			}
			payload.RowID = batch.Cursor.RowID
			payload.Capture = &batch.Cursor
			payload.TableCounts[policy.Table] += batch.CompletedRows
			if batch.Exhausted {
				payload.TableIndex++
				payload.RowID = 0
				payload.Capture = nil
				cursor = instanceProductExportCaptureCursor{}
			} else {
				cursor = batch.Cursor
			}
			if len(batch.Fragment) == 0 {
				if batch.Exhausted {
					continue
				}
				return newInstanceProductExportFailure(errorCodeInstanceProductExportCreateFailed, errors.New("empty product export capture turn"))
			}
			if err := storeInstanceProductExportFragment(txApp, current, policy.Table, batch); err != nil {
				return err
			}
			return updateInstanceProductExportProgress(
				txApp, current, payload, batch.CompletedRows, int64(len(batch.Fragment)), current.ChunkCount+1,
			)
		}
		payload.Phase = instanceProductExportPhasePublish
		payload.Capture = nil
		return updateInstanceProductExportProgress(txApp, current, payload, 0, 0, current.ChunkCount)
	})
}

func (r *durableJobRunner) cachedInstanceProductExportPolicies(app core.App) ([]instanceProductExportTablePolicy, error) {
	var schemaCookie int
	if err := app.DB().NewQuery(`PRAGMA schema_version`).Row(&schemaCookie); err != nil {
		return nil, err
	}
	r.instanceProductExportPolicyMu.Lock()
	defer r.instanceProductExportPolicyMu.Unlock()
	if schemaCookie == r.instanceProductExportPolicySchemaCookie && len(r.instanceProductExportPolicies) > 0 {
		return append([]instanceProductExportTablePolicy(nil), r.instanceProductExportPolicies...), nil
	}
	policies, _, err := loadInstanceProductExportPolicy(app)
	if err != nil {
		return nil, err
	}
	r.instanceProductExportPolicySchemaCookie = schemaCookie
	r.instanceProductExportPolicies = append([]instanceProductExportTablePolicy(nil), policies...)
	return append([]instanceProductExportTablePolicy(nil), policies...), nil
}

func includedInstanceProductExportPolicies(policies []instanceProductExportTablePolicy) []instanceProductExportTablePolicy {
	included := make([]instanceProductExportTablePolicy, 0, len(policies))
	for _, policy := range policies {
		if policy.Disposition == instanceProductExportIncluded {
			included = append(included, policy)
		}
	}
	return included
}

func updateInstanceProductExportProgress(
	app core.App,
	job storedJob,
	payload instanceProductExportJobPayload,
	processed int,
	capturedBytes int64,
	chunkCount int,
) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	total := job.Total
	if payload.Phase == instanceProductExportPhasePublish {
		total = job.Processed + processed
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]] = {:payload}, [[cursor]] = {:cursor},
			[[processed_items]] = [[processed_items]] + {:processed},
			[[total_items]] = {:total},
			[[chunk_count]] = {:chunkCount}, [[captured_bytes]] = [[captured_bytes]] + {:capturedBytes},
			[[not_before]] = {:notBefore}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "payload": string(encoded),
		"cursor":    instanceProductExportProgressCursor(payload),
		"processed": processed, "total": total, "chunkCount": chunkCount, "capturedBytes": capturedBytes,
		"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
	}).Execute()
	return err
}

func instanceProductExportProgressCursor(payload instanceProductExportJobPayload) string {
	if payload.Capture == nil {
		return fmt.Sprintf("%d:%d", payload.TableIndex, payload.RowID)
	}
	return fmt.Sprintf(
		"%d:%d:%d:%d:%d", payload.TableIndex, payload.RowID,
		payload.Capture.ActiveRowID, payload.Capture.ColumnIndex, payload.Capture.ColumnOffset,
	)
}

func (r *durableJobRunner) failInstanceProductExportJob(job storedJob, code string, cause error) error {
	reference := newDiagnosticReference()
	_, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'failed', [[error_code]] = {:errorCode},
			[[diagnostic_reference]] = {:reference}, [[attempt]] = [[attempt]] + 1,
			[[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "errorCode": code, "reference": reference,
		"updated": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return err
	}
	logDurableJobDiagnostic(r.app, code, reference, job.ID, "instance-product-export.run", "terminal", cause)
	return nil
}
