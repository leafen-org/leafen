package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How far a Notion import has come, said the same on the import screen, in
// an agent's migration status and in the server's log. Reading Notion comes
// first and makes nothing in Leafen: every page, row and block is staged,
// and only then made, as a page's links, relations, merges and views need
// everything they name read first. So while it reads it counts what it has
// read and what still waits — page and row bodies, databases whose rows it
// hasn't listed — with the scan's row count when a migration has one; and
// when it last moved, so a run that stands still says so.

// notionImportProgressLogEvery is how often a running import says where it
// is in the server's log.
const notionImportProgressLogEvery = 3 * time.Minute

// notionImportProgressOf reads a job's progress.
func notionImportProgressOf(app core.App, job storedJob, payload notionImportJobPayload) (*notionImportProgress, error) {
	return notionImportProgressOfFor(context.Background(), app, job, payload)
}

// notionImportProgressOfFor is notionImportProgressOf for the owner whose own
// request ctx carries.
func notionImportProgressOfFor(ctx context.Context, app core.App, job storedJob, payload notionImportJobPayload) (*notionImportProgress, error) {
	progress := &notionImportProgress{
		WaitingDatabases: len(payload.PendingDatabaseIDs), ChangedAt: job.Updated,
		// A run again's data is whole as of the last run that finished
		// (notion_import_filling_again.go); 0 for a first move.
		MovedAsOf: notionImportMovedBeforeOf(app, job, payload).AsOf,
	}
	// Counted as Notion has them, as the report counts them
	// (notion_import_counted.go; 2026-11-16: a real run counted 49 pages
	// and 1,014 rows of a scan's 37 and 913, and a run 241 rows of Notion's
	// 239, its 2 templates among them): the page Leafen makes to hold each
	// full-page database is no Notion page, and rows in Notion's trash, which
	// come to Leafen's, and templates are counted apart.
	rows := []struct {
		Kind    string `db:"kind"`
		Counted string `db:"counted"`
		Pending bool   `db:"pending"`
		Count   int    `db:"count"`
		Latest  int64  `db:"latest"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[kind]], [[counted]], [[pending]], COUNT(*) AS [[count]], MAX([[created]]) AS [[latest]]
		FROM (SELECT [[kind]], [[discovery_status]] = 'pending' AS [[pending]], [[created]],
				` + notionImportCountedKindSQL + ` AS [[counted]]
			FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId})
		GROUP BY [[kind]], [[counted]], [[pending]]
	`).Bind(notionImportCountedParams(dbx.Params{"jobId": job.ID})).All(&rows); err != nil {
		return nil, err
	}
	bodiesRead, bodiesWaiting := 0, 0
	for _, row := range rows {
		switch row.Counted {
		case notionImportCountedPage:
			progress.ReadPages += row.Count
		case notionImportCountedRow:
			progress.ReadRows += row.Count
		case notionImportCountedTrashedRow:
			progress.ReadTrashedRows += row.Count
		case notionImportCountedTemplate:
			progress.ReadTemplates += row.Count
		case notionImportCountedDatabase:
			progress.ReadDatabases += row.Count
		case notionImportCountedBlock:
			progress.ReadBlocks += row.Count
		}
		if row.Pending {
			progress.WaitingBodies += row.Count
		}
		// A page's or row's body, a template's too; not a database's holder.
		if body := (row.Kind == "page" || row.Kind == "row") && row.Counted != ""; body && row.Pending {
			bodiesWaiting += row.Count
		} else if body {
			bodiesRead += row.Count
		}
	}
	// A run again's templates are those it read again and those Notion still
	// gives that it left as they were, with what their bodies hold
	// (notion_import_counted.go).
	if payload.MigrationRerunSince > 0 {
		kept, err := notionImportKeptTemplates(app, job.ID, payload.MigrationID)
		if err != nil {
			return nil, err
		}
		progress.ReadTemplates += kept.Templates
		progress.ReadPages += kept.Body.Pages
		progress.ReadDatabases += kept.Body.Databases
		progress.ReadRows += kept.Body.Rows
		progress.ReadBlocks += kept.Body.Blocks
	}
	// The bodies read of all there are: as many as the scan counted, or as
	// many as reading found if that is more — read never goes back.
	progress.BodiesRead, progress.BodiesTotal = bodiesRead, bodiesRead+bodiesWaiting
	if payload.Phase == notionImportPhaseScope {
		// The job's own time moves with every turn; what it read moves only
		// when Notion gave something.
		progress.ChangedAt = 0
		for _, row := range rows {
			progress.ChangedAt = max(progress.ChangedAt, row.Latest)
		}
		if progress.ChangedAt == 0 {
			progress.ChangedAt = job.Created
		}
	}
	// A migration's run counts down to the end its pace puts it at, past the
	// end it promised only near it (notion_migration_time_left.go): the time left
	// goes up only when the pace is clearly slower, or the promised end moves
	// with its reason.
	run, counted := notionMigrationEstimate{}, 0
	if payload.MigrationID != "" {
		var expected struct {
			Pages    int    `db:"pages"`
			Rows     int    `db:"rows"`
			Estimate string `db:"estimate"`
			Tally    string `db:"tally"`
		}
		if err := app.DB().NewQuery(`
			SELECT COALESCE(json_extract([[summary_json]], '$.counts.pages'), 0) AS [[pages]],
				COALESCE(json_extract([[summary_json]], '$.counts.rows'), 0) AS [[rows]],
				COALESCE(json_extract([[run_report_json]], '$.estimate'), '') AS [[estimate]],
				COALESCE(json_extract([[run_report_json]], '$.tally'), '') AS [[tally]]
			FROM {{leafen_notion_migrations}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": payload.MigrationID}).One(&expected); err == nil {
			progress.ExpectedPages, progress.ExpectedRows = expected.Pages, expected.Rows
			if expected.Estimate != "" && json.Unmarshal([]byte(expected.Estimate), &run) == nil {
				counted = run.Pages + run.Rows
				// The estimate of a run before this one counts no time left.
				if run.StartedAt < job.Created {
					run.EndsAt = 0
				}
			}
			// A run again counts what it made new, changed and left as it was
			// (notion_migration_run_again.go).
			tally := notionMigrationRunTally{}
			if payload.MigrationRerunSince > 0 && expected.Tally != "" && json.Unmarshal([]byte(expected.Tally), &tally) == nil {
				progress.Tally = &tally
			}
		}
		if payload.Phase == notionImportPhaseScope {
			progress.BodiesTotal = max(progress.BodiesTotal, progress.ExpectedPages+progress.ExpectedRows)
		}
	}
	// What the bodies hold is read after them, a level at a time: its reads
	// done of all, by the reckoning the run's time left goes by
	// (notion_migration_reading_work.go), so its line moves while they are
	// read.
	if payload.Phase == notionImportPhaseScope {
		bodies, err := notionMigrationReadingBodies(app, job.ID, notionMigrationStageReading)
		if err != nil {
			return nil, err
		}
		progress.InnerRead, progress.InnerTotal = bodies.ofRows(run).innerReads(counted)
	}
	switch {
	case run.EndsAt > 0:
		// Its bar fills by the same reckoning (notionMigrationTimeShare), and
		// the line beside it says why it is far from the time said
		// (notion_migration_time_said.go).
		now := time.Now()
		progress.MinutesLeft = notionMigrationMinutesLeft(&run, now)
		progress.TimeShare = notionMigrationTimeShare(run, now)
		// The same share in whole percent, its unit in its name, for an
		// answer an agent reads: timeShare counts thousandths for the bar,
		// and an agent said 63 of them as "63%" (2026-11-23).
		progress.PercentDone = progress.TimeShare / 10
		if job.Status != durableJobStatusQueued {
			break
		}
		if language, err := serverTextLanguageIn(ctx, app, job.Owner); err == nil {
			progress.LeftSay = notionMigrationLeftSay(language, run, now)
		}
	case payload.Phase == notionImportPhaseScope:
		// Its pace leaves out the time it stood with Leafen off.
		progress.MinutesLeft = notionImportReadingMinutesLeft(progress, bodiesRead, job.Created+payload.ReadingOffMs)
	}
	return progress, nil
}

// notionImportReadingMinutesLeft guesses the minutes reading still takes from
// how fast it has read the page and row bodies since it started (moved on by
// the time it stood with Leafen off): those waiting, and the rows a
// migration's scan counted that aren't listed yet, each a body to read. 0
// when there is nothing to go by yet.
func notionImportReadingMinutesLeft(progress *notionImportProgress, bodiesRead int, started int64) int {
	elapsed := float64(time.Now().UnixMilli()-started) / float64(time.Minute.Milliseconds())
	if bodiesRead < 20 || elapsed <= 0 {
		return 0
	}
	waiting := progress.WaitingBodies
	if unlisted := progress.ExpectedRows - progress.ReadRows; unlisted > 0 {
		waiting += unlisted
	}
	return max(1, int(math.Ceil(float64(waiting)/(float64(bodiesRead)/elapsed))))
}

var notionImportProgressLogged = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

// logNotionImportProgress says in the server's log, every few minutes a job
// runs, where it is: what it read and what waits, or what it made of all.
func logNotionImportProgress(app core.App, jobID string) {
	notionImportProgressLogged.Lock()
	last, seen := notionImportProgressLogged.at[jobID]
	if seen && time.Since(last) < notionImportProgressLogEvery {
		notionImportProgressLogged.Unlock()
		return
	}
	for other, at := range notionImportProgressLogged.at {
		if time.Since(at) > time.Hour {
			delete(notionImportProgressLogged.at, other)
		}
	}
	notionImportProgressLogged.at[jobID] = time.Now()
	notionImportProgressLogged.Unlock()
	job, err := loadStoredJob(app, jobID, "")
	if err != nil {
		return
	}
	payload := notionImportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil {
		return
	}
	if job.Status != durableJobStatusQueued {
		return
	}
	progress, err := notionImportProgressOf(app, job, payload)
	if err != nil {
		return
	}
	if payload.Phase == notionImportPhaseScope {
		fmt.Fprintf(os.Stderr, "leafen: notion import %s reading: %d pages, %d databases, %d rows, %d blocks read; "+
			"%d of about %d blocks inside the bodies read; %d bodies, %d databases and %d files wait; nothing made yet\n",
			jobID, progress.ReadPages, progress.ReadDatabases, progress.ReadRows, progress.ReadBlocks,
			progress.InnerRead, progress.InnerTotal, progress.WaitingBodies, progress.WaitingDatabases, len(payload.PendingFileIDs))
		return
	}
	if job.Total > 0 && job.Processed >= job.Total {
		fmt.Fprintf(os.Stderr, "leafen: notion import %s finishing: all %d made; comments %d of %d pages and rows, %d of %d blocks asked\n",
			jobID, job.Total, payload.CommentPagesDone, payload.CommentPagesTotal, payload.CommentBlocksDone, payload.CommentBlocksTotal)
		return
	}
	fmt.Fprintf(os.Stderr, "leafen: notion import %s making: %d of %d made\n", jobID, job.Processed, job.Total)
}

// notionImportProgressText says what a running import's counts don't: that
// nothing shows in Leafen while it reads (a run again: what moved before
// stays as it was), and how long nothing moved. Its counts are the import's
// progress, which Leafen's import window shows live; the time left is said
// apart, by the run's one reckoning (notionMigrationEstimateNext)
// (2026-11-23: every status said a long line of every count read, and an
// agent passed each on).
func notionImportProgressText(job notionImportJob, now time.Time) string {
	progress := job.Progress
	if progress == nil {
		return ""
	}
	text := ""
	if job.Phase == notionImportPhaseScope {
		// A run again leaves what the runs before moved as it is meanwhile
		// (notion_import_filling_again.go).
		text = "Nothing is made in Leafen until reading ends, so its pages and views appear all at once."
		if progress.MovedAsOf > 0 {
			text = "What the runs before moved is all in Leafen as of the last move that finished (" +
				time.UnixMilli(progress.MovedAsOf).UTC().Format(time.RFC3339) +
				") and stays as it is meanwhile; once reading ends, only what Notion changed since changes."
		}
	}
	if still := now.Sub(time.UnixMilli(progress.ChangedAt)); progress.ChangedAt > 0 && still >= 5*time.Minute {
		text += " Its counts haven't moved for " + notionMigrationDuration(accountLanguageEn, int(still.Minutes())) + "; the run keeps trying by itself."
	}
	return strings.TrimSpace(text)
}

// notionImportReadingInner reports whether reading's line says what the
// bodies hold: once that is read, or every body is.
func notionImportReadingInner(progress notionImportProgress) bool {
	return progress.InnerTotal > 0 && (progress.InnerRead > 0 || progress.BodiesRead >= progress.BodiesTotal)
}
