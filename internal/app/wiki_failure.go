package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func scheduleWikiMutationFailure(
	app core.App,
	jobID, reference string,
	applyErr error,
) (bool, error) {
	base, err := loadStoredJob(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && base.Kind != wikiMutationJobKind) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	detail, err := loadWikiMutationByID(app, jobID, "")
	if err != nil {
		return true, err
	}
	terminal := errors.Is(applyErr, errWikiConflict) || base.Attempt+1 >= base.MaxAttempts
	if detail.Phase == wikiMutationPhaseCleaning {
		terminal = false
	}
	if !terminal {
		delay := durableJobReadyDelay
		// A cleanup that keeps failing waits longer each time and in the end
		// stops, so it holds neither the job runner nor its page (measured
		// 2026-10-30, approved the same day).
		if detail.Phase == wikiMutationPhaseCleaning {
			if base.Attempt+1 >= wikiCleaningMaxAttempts {
				return true, giveUpWikiCleaning(app, jobID, reference, applyErr)
			}
			delay = wikiCleaningRetryDelay(base.Attempt)
		}
		now := time.Now().UTC().UnixMilli()
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[attempt]]=[[attempt]]+1,[[diagnostic_reference]]={:reference},
				[[not_before]]={:notBefore},[[updated]]={:now}
			WHERE [[id]]={:jobId} AND [[status]]='queued'
		`).Bind(dbx.Params{
			"reference": reference, "notBefore": now + delay.Milliseconds(),
			"now": now, "jobId": jobID,
		}).Execute()
		return true, err
	}
	return true, app.RunInTransaction(func(txApp core.App) error {
		detail, err := loadWikiMutationByID(txApp, jobID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && detail.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_wiki_mutations}}
			SET [[phase]]='cleaning',[[error_code]]={:errorCode},
				[[revision]]=[[revision]]+1,[[updated_at]]={:now}
			WHERE [[job_id]]={:jobId} AND [[phase]] NOT IN ('completed','failed')
		`).Bind(dbx.Params{
			"errorCode": errorCodeWikiConflict, "now": now, "jobId": jobID,
		}).Execute(); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[cursor]]='cleaning',[[error_code]]={:errorCode},
				[[diagnostic_reference]]={:reference},[[attempt]]=[[attempt]]+1,
				[[not_before]]={:now},[[updated]]={:now}
			WHERE [[id]]={:jobId} AND [[status]]='queued'
		`).Bind(dbx.Params{
			"errorCode": errorCodeWikiConflict, "reference": reference,
			"now": now, "jobId": jobID,
		}).Execute()
		return err
	})
}

// cleanWikiMutationWindow ends a wiki job that failed, once the database
// steps took back what it made (wiki_database_job.go): the page is let go
// and the job fails.
func cleanWikiMutationWindow(
	app core.App,
	job storedWikiMutation,
	target *core.Record,
) error {
	target.Set("wiki_active_job_id", "")
	if err := app.Save(target); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutations}}
		SET [[phase]]='failed',[[revision]]=[[revision]]+1,
			[[updated_at]]={:now},[[completed_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[phase]]='cleaning'
	`).Bind(dbx.Params{"now": now, "jobId": job.ID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='failed',[[not_before]]=0,[[updated]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"now": now, "jobId": job.ID}).Execute()
	return err
}

// A wiki cleanup that keeps failing waits twice as long each time, up to
// five minutes, and stops on its twelfth attempt (measured 2026-10-30: the
// other wiki changes of its workspace answer 409 until it stops).
const (
	wikiCleaningMaxAttempts   = 12
	wikiCleaningRetryDelayCap = 5 * time.Minute
)

func wikiCleaningRetryDelay(attempt int) time.Duration {
	if attempt > 20 {
		attempt = 20
	}
	if delay := durableJobReadyDelay << attempt; delay < wikiCleaningRetryDelayCap {
		return delay
	}
	return wikiCleaningRetryDelayCap
}

// giveUpWikiCleaning ends a cleanup that would not finish: the job fails with
// its reason and the page is no longer held by it.
func giveUpWikiCleaning(app core.App, jobID, reference string, applyErr error) error {
	logDurableJobDiagnostic(app, errorCodeWikiConflict, reference, jobID, "wiki.cleaning", "give-up", applyErr)
	return app.RunInTransaction(func(txApp core.App) error {
		detail, err := loadWikiMutationByID(txApp, jobID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && detail.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if target, err := txApp.FindRecordById(pagesCollection, detail.TargetPageID); err == nil &&
			target.GetString("wiki_active_job_id") == jobID {
			target.Set("wiki_active_job_id", "")
			if err := txApp.Save(target); err != nil {
				return err
			}
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_wiki_mutations}}
			SET [[phase]]='failed',[[error_code]]={:errorCode},[[revision]]=[[revision]]+1,
				[[updated_at]]={:now},[[completed_at]]={:now}
			WHERE [[job_id]]={:jobId} AND [[phase]]='cleaning'
		`).Bind(dbx.Params{"errorCode": errorCodeWikiConflict, "now": now, "jobId": jobID}).Execute(); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]]='failed',[[error_code]]={:errorCode},[[diagnostic_reference]]={:reference},
				[[attempt]]=[[attempt]]+1,[[not_before]]=0,[[updated]]={:now}
			WHERE [[id]]={:jobId} AND [[status]]='queued'
		`).Bind(dbx.Params{"errorCode": errorCodeWikiConflict, "reference": reference, "now": now, "jobId": jobID}).Execute()
		return err
	})
}
