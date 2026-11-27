package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedNotionImportRetentionRun struct {
	ID          string `db:"id"`
	Status      string `db:"status"`
	Version     int    `db:"version"`
	JobID       string `db:"job_id"`
	JobStatus   string `db:"job_status"`
	JobUpdated  int64  `db:"job_updated"`
	Reason      string `db:"reason"`
	Phase       string `db:"phase"`
	AfterKey    string `db:"after_key"`
	DeletedRows int    `db:"deleted_rows"`
	DeletedJobs int    `db:"deleted_jobs"`
}

type notionImportRetentionCandidate struct {
	ID      string `db:"id"`
	Status  string `db:"status"`
	Updated int64  `db:"updated"`
}

type notionImportRetentionKey struct {
	Key string `db:"key"`
}

type notionImportRetentionPhaseSpec struct {
	Table   string
	Key     string
	Numeric bool
}

func (runner *notionImportRetentionRunner) runSlice(
	ctx context.Context,
	scheduledAt time.Time,
) (notionImportRetentionSliceResult, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	result := notionImportRetentionSliceResult{Status: notionImportRetentionStatusIdle}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if scheduledAt.IsZero() || runner.retention < 24*time.Hour ||
		runner.retention > notionImportRetentionMaximumDays*24*time.Hour {
		return result, errNotionImportRetentionUnavailable
	}
	now := scheduledAt.UTC().UnixMilli()
	err := runner.app.RunInTransaction(func(txApp core.App) error {
		run, err := loadNotionImportRetentionRun(ctx, txApp)
		if err != nil {
			return err
		}
		if run.Status != notionImportRetentionStatusActive {
			candidate, reason, nextDue, err := selectNotionImportRetentionCandidate(
				ctx, txApp, now, runner.retention,
			)
			if err != nil {
				return err
			}
			result.NextDue = nextDue
			if candidate == nil {
				return nil
			}
			if err := claimNotionImportRetentionJob(ctx, txApp, run, *candidate, reason, now); err != nil {
				return err
			}
			result.Status = notionImportRetentionStatusActive
			result.HasMore = true
			result.NextDue = now
			return nil
		}
		result, err = advanceNotionImportRetentionRun(
			ctx, txApp, run, now, runner.retention,
		)
		return err
	})
	if err != nil {
		return notionImportRetentionSliceResult{}, errors.Join(
			errNotionImportRetentionUnavailable, err,
		)
	}
	return result, nil
}

func loadNotionImportRetentionRun(
	ctx context.Context,
	app core.App,
) (storedNotionImportRetentionRun, error) {
	var run storedNotionImportRetentionRun
	err := app.DB().NewQuery(`
		SELECT [[id]],[[status]],[[version]],[[job_id]],[[job_status]],[[job_updated]],
			[[reason]],[[phase]],[[after_key]],[[deleted_rows]],[[deleted_jobs]]
		FROM {{leafen_notion_import_retention_runs}} WHERE [[id]]='notion-import'
	`).WithContext(ctx).One(&run)
	return run, err
}

func selectNotionImportRetentionCandidate(
	ctx context.Context,
	app core.App,
	now int64,
	retention time.Duration,
) (*notionImportRetentionCandidate, string, int64, error) {
	cutoff := now - retention.Milliseconds()
	var aged notionImportRetentionCandidate
	err := app.DB().NewQuery(`
		SELECT [[id]],[[status]],[[updated]] FROM {{leafen_jobs}}
		INDEXED BY [[idx_leafen_jobs_notion_retention]]
		WHERE [[kind]]='notion-import' AND [[status]] IN ('completed','failed','canceled')
			AND [[updated]] <= {:cutoff}
		ORDER BY [[updated]],[[id]] LIMIT 1
	`).WithContext(ctx).Bind(dbx.Params{"cutoff": cutoff}).One(&aged)
	if err == nil {
		return &aged, notionImportRetentionReasonAge, now, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, "", 0, err
	}

	newest := make([]notionImportRetentionCandidate, 0, notionImportRetentionKeepMax+1)
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[status]],[[updated]] FROM {{leafen_jobs}}
		INDEXED BY [[idx_leafen_jobs_notion_retention]]
		WHERE [[kind]]='notion-import' AND [[status]] IN ('completed','failed','canceled')
		ORDER BY [[updated]] DESC,[[id]] DESC LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"limit": notionImportRetentionKeepMax + 1,
	}).All(&newest); err != nil {
		return nil, "", 0, err
	}
	if len(newest) > notionImportRetentionKeepMax {
		candidate := newest[notionImportRetentionKeepMax]
		return &candidate, notionImportRetentionReasonKeepCap, now, nil
	}

	var next sql.NullInt64
	if err := app.DB().NewQuery(`
		SELECT MIN([[updated]]) FROM {{leafen_jobs}}
		INDEXED BY [[idx_leafen_jobs_notion_retention]]
		WHERE [[kind]]='notion-import' AND [[status]] IN ('completed','failed','canceled')
	`).WithContext(ctx).Row(&next); err != nil {
		return nil, "", 0, err
	}
	if next.Valid {
		return nil, "", next.Int64 + retention.Milliseconds(), nil
	}
	return nil, "", 0, nil
}

func claimNotionImportRetentionJob(
	ctx context.Context,
	app core.App,
	run storedNotionImportRetentionRun,
	job notionImportRetentionCandidate,
	reason string,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_retention_runs}}
		SET [[status]]='active',[[version]]=[[version]]+1,
			[[job_id]]={:jobId},[[job_status]]={:jobStatus},[[job_updated]]={:jobUpdated},
			[[reason]]={:reason},[[phase]]='staging',[[after_key]]='',
			[[started_at]]={:now},[[completed_at]]=0,[[updated_at]]={:now}
		WHERE [[id]]='notion-import' AND [[status]]='idle' AND [[version]]={:version}
	`).WithContext(ctx).Bind(dbx.Params{
		"jobId": job.ID, "jobStatus": job.Status, "jobUpdated": job.Updated,
		"reason": reason, "now": now, "version": run.Version,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errNotionImportRetentionUnavailable
	}
	return nil
}

func advanceNotionImportRetentionRun(
	ctx context.Context,
	app core.App,
	run storedNotionImportRetentionRun,
	now int64,
	retention time.Duration,
) (notionImportRetentionSliceResult, error) {
	result := notionImportRetentionSliceResult{
		Status: notionImportRetentionStatusActive, HasMore: true, NextDue: now,
	}
	job, err := loadCurrentNotionImportRetentionJob(ctx, app, run.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return completeOrAbandonNotionImportRetention(ctx, app, run, now, "completed")
	}
	if err != nil {
		return result, err
	}
	current := job.ID == run.JobID && job.Status == run.JobStatus &&
		job.Updated == run.JobUpdated && notionImportRetentionTerminal(job.Status)
	if run.Reason == notionImportRetentionReasonAge {
		current = current && job.Updated+retention.Milliseconds() <= now
	} else if run.Reason != notionImportRetentionReasonKeepCap {
		current = false
	}
	if !current {
		return completeOrAbandonNotionImportRetention(ctx, app, run, now, "abandoned")
	}
	if run.Phase == notionImportRetentionPhaseJob {
		deleted, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_jobs}}
			WHERE [[id]]={:jobId} AND [[kind]]='notion-import'
				AND [[status]]={:status} AND [[updated]]={:updated}
		`).WithContext(ctx).Bind(dbx.Params{
			"jobId": run.JobID, "status": run.JobStatus, "updated": run.JobUpdated,
		}).Execute()
		if err != nil {
			return result, err
		}
		count, err := deleted.RowsAffected()
		if err != nil || count != 1 {
			if err == nil {
				err = errNotionImportRetentionUnavailable
			}
			return result, err
		}
		result, err = completeOrAbandonNotionImportRetention(ctx, app, run, now, "completed")
		result.DeletedJobs = 1
		return result, err
	}
	if run.Phase == notionImportRetentionPhaseStaging {
		nextPhase, afterKey, deleted, err := advanceNotionImportRetentionStaging(ctx, app, run)
		if err != nil {
			return result, err
		}
		if err := updateNotionImportRetentionCursor(ctx, app, run, nextPhase, afterKey, deleted, now); err != nil {
			return result, err
		}
		result.DeletedRows = deleted
		return result, nil
	}
	spec, ok := notionImportRetentionPhase(run.Phase)
	if !ok {
		return completeOrAbandonNotionImportRetention(ctx, app, run, now, "abandoned")
	}
	keys, hasMore, err := loadNotionImportRetentionKeys(ctx, app, spec, run)
	if err != nil {
		return result, err
	}
	if hasMore {
		keys = keys[:notionImportRetentionDeleteBatchSize]
	}
	// What a direct import made is kept as its records go (notion_import_kept_mappings.go).
	if err := keepNotionDirectImportMade(ctx, app, run, keys); err != nil {
		return result, err
	}
	deleted, err := deleteNotionImportRetentionKeys(ctx, app, spec, run.JobID, keys)
	if err != nil {
		return result, err
	}
	nextPhase, afterKey := notionImportRetentionNextPhase(run.Phase), ""
	if hasMore {
		nextPhase = run.Phase
		afterKey = keys[len(keys)-1]
	}
	if nextPhase == "" {
		return result, errNotionImportRetentionUnavailable
	}
	if err := updateNotionImportRetentionCursor(
		ctx, app, run, nextPhase, afterKey, deleted, now,
	); err != nil {
		return result, err
	}
	result.DeletedRows = deleted
	return result, nil
}

func loadCurrentNotionImportRetentionJob(
	ctx context.Context,
	app core.App,
	jobID string,
) (notionImportRetentionCandidate, error) {
	var job notionImportRetentionCandidate
	err := app.DB().NewQuery(`
		SELECT [[id]],[[status]],[[updated]] FROM {{leafen_jobs}}
		WHERE [[id]]={:jobId} AND [[kind]]='notion-import'
	`).WithContext(ctx).Bind(dbx.Params{"jobId": jobID}).One(&job)
	return job, err
}

func notionImportRetentionPhase(phase string) (notionImportRetentionPhaseSpec, bool) {
	switch phase {
	case notionImportRetentionPhaseSources:
		return notionImportRetentionPhaseSpec{Table: "leafen_notion_import_sources", Key: "sequence", Numeric: true}, true
	case notionImportRetentionPhaseMappings:
		return notionImportRetentionPhaseSpec{Table: "leafen_notion_import_mappings", Key: "source_id"}, true
	case notionImportRetentionPhaseFileTasks:
		return notionImportRetentionPhaseSpec{Table: "leafen_notion_import_file_tasks", Key: "source_id"}, true
	case notionImportRetentionPhaseChunks:
		return notionImportRetentionPhaseSpec{Table: "leafen_job_chunks", Key: "chunk_index", Numeric: true}, true
	case notionImportRetentionPhaseReport:
		return notionImportRetentionPhaseSpec{Table: "leafen_notion_import_reports", Key: "job_id"}, true
	case notionImportRetentionPhaseEffect:
		return notionImportRetentionPhaseSpec{Table: "leafen_job_effects", Key: "job_id"}, true
	default:
		return notionImportRetentionPhaseSpec{}, false
	}
}

func loadNotionImportRetentionKeys(
	ctx context.Context,
	app core.App,
	spec notionImportRetentionPhaseSpec,
	run storedNotionImportRetentionRun,
) ([]string, bool, error) {
	comparison := ""
	params := dbx.Params{
		"jobId": run.JobID, "limit": notionImportRetentionDeleteBatchSize + 1,
	}
	if run.AfterKey != "" {
		comparison = " AND [[" + spec.Key + "]] > {:afterKey}"
		if spec.Numeric {
			after, err := strconv.ParseInt(run.AfterKey, 10, 64)
			if err != nil {
				return nil, false, failedBecause(errNotionImportRetentionUnavailable, err)
			}
			params["afterKey"] = after
		} else {
			params["afterKey"] = run.AfterKey
		}
	}
	query := fmt.Sprintf(`
		SELECT CAST([[%.64s]] AS TEXT) AS [[key]] FROM [[%.80s]]
		WHERE [[job_id]]={:jobId}%s ORDER BY [[%.64s]] LIMIT {:limit}
	`, spec.Key, spec.Table, comparison, spec.Key)
	rows := make([]notionImportRetentionKey, 0, notionImportRetentionDeleteBatchSize+1)
	if err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, false, err
	}
	keys := make([]string, len(rows))
	for index, row := range rows {
		keys[index] = row.Key
	}
	return keys, len(keys) > notionImportRetentionDeleteBatchSize, nil
}

func deleteNotionImportRetentionKeys(
	ctx context.Context,
	app core.App,
	spec notionImportRetentionPhaseSpec,
	jobID string,
	keys []string,
) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	params := dbx.Params{"jobId": jobID}
	placeholders := make([]string, len(keys))
	for index, key := range keys {
		name := fmt.Sprintf("key%d", index)
		placeholders[index] = "{:" + name + "}"
		if spec.Numeric {
			value, err := strconv.ParseInt(key, 10, 64)
			if err != nil {
				return 0, failedBecause(errNotionImportRetentionUnavailable, err)
			}
			params[name] = value
		} else {
			params[name] = key
		}
	}
	query := fmt.Sprintf(`DELETE FROM [[%.80s]]
		WHERE [[job_id]]={:jobId} AND [[%.64s]] IN (%s)`,
		spec.Table, spec.Key, strings.Join(placeholders, ","),
	)
	result, err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if deleted != int64(len(keys)) {
		return 0, errNotionImportRetentionUnavailable
	}
	return len(keys), nil
}

func updateNotionImportRetentionCursor(
	ctx context.Context,
	app core.App,
	run storedNotionImportRetentionRun,
	phase, afterKey string,
	deleted int,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_retention_runs}}
		SET [[version]]=[[version]]+1,[[phase]]={:phase},[[after_key]]={:afterKey},
			[[deleted_rows]]=[[deleted_rows]]+{:deleted},[[updated_at]]={:now}
		WHERE [[id]]='notion-import' AND [[status]]='active' AND [[version]]={:version}
			AND [[job_id]]={:jobId} AND [[phase]]={:currentPhase} AND [[after_key]]={:currentAfter}
	`).WithContext(ctx).Bind(dbx.Params{
		"phase": phase, "afterKey": afterKey, "deleted": deleted, "now": now,
		"version": run.Version, "jobId": run.JobID,
		"currentPhase": run.Phase, "currentAfter": run.AfterKey,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errNotionImportRetentionUnavailable
	}
	return nil
}

func completeOrAbandonNotionImportRetention(
	ctx context.Context,
	app core.App,
	run storedNotionImportRetentionRun,
	now int64,
	status string,
) (notionImportRetentionSliceResult, error) {
	deletedJobs := 0
	if status == "completed" && run.Phase == notionImportRetentionPhaseJob {
		deletedJobs = 1
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_retention_runs}}
		SET [[status]]='idle',[[version]]=[[version]]+1,[[job_id]]='',[[job_status]]='',
			[[job_updated]]=0,[[reason]]='',[[phase]]='',[[after_key]]='',
			[[deleted_jobs]]=[[deleted_jobs]]+{:deletedJobs},[[completed_at]]={:now},[[updated_at]]={:now}
		WHERE [[id]]='notion-import' AND [[status]]='active' AND [[version]]={:version}
			AND [[job_id]]={:jobId}
	`).WithContext(ctx).Bind(dbx.Params{
		"deletedJobs": deletedJobs, "now": now,
		"version": run.Version, "jobId": run.JobID,
	}).Execute()
	if err != nil {
		return notionImportRetentionSliceResult{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		if err == nil {
			err = errNotionImportRetentionUnavailable
		}
		return notionImportRetentionSliceResult{}, err
	}
	return notionImportRetentionSliceResult{
		Status: status, DeletedJobs: deletedJobs, HasMore: true, NextDue: now,
	}, nil
}

func notionImportRetentionOwnsJob(
	ctx context.Context,
	app core.App,
	jobID string,
) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_retention_runs}}
		WHERE [[id]]='notion-import' AND [[status]]='active' AND [[job_id]]={:jobId}
	`).WithContext(ctx).Bind(dbx.Params{"jobId": jobID}).Row(&count)
	return count == 1, err
}
