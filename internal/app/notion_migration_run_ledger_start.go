package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A run that takes over a stopped one, or runs again, starts from the
// migration's ledger in its job's first turn, not in the request that starts
// it: what the stopped runs made joins the ledger, then the new job's
// mappings come from the ledger, a window at a time each
// (notion_migration_ledger_window.go), before the job reads Notion
// (2026-11-17, a stopped 15,000-block run on a NAS-class server: done in the
// request, 'run again' was answered in 5.1 to 7.8 s; in the first turn, in
// 1.0 to 1.2 s, the turn's windows holding the one write connection under
// 0.1 s each as before). The job reads nothing and makes nothing before its first
// turn; a run stopped before that turn passes its stopped runs on to the
// next (notionMigrationLedgerFrom), and the run's top page is found in them
// meanwhile (notionMigrationTakenOverTop).

// notionMigrationLedgerStart is what a run's first turn takes from the
// ledger: emptied once taken.
type notionMigrationLedgerStart struct {
	// The stopped runs' jobs whose making joins the ledger first, oldest first.
	LedgerFrom []string `json:"ledgerFrom,omitempty"`
	// Whether the job starts from the ledger.
	LedgerPreload bool `json:"ledgerPreload,omitzero"`
}

// notionMigrationLedgerFrom is the stopped runs a run that takes over
// stoppedJob records: it, and those it took over itself, were it stopped
// before its first turn.
func notionMigrationLedgerFrom(stopped *notionImportJobPayload, stoppedJob string) []string {
	if stoppedJob == "" {
		return nil
	}
	from := []string{}
	if stopped != nil {
		from = append(from, stopped.LedgerFrom...)
	}
	return append(from, stoppedJob)
}

// startNotionMigrationRunFromLedger records the stopped runs' making and
// preloads the job from the ledger, then has the job go on, its next turn
// reading Notion. A turn cut short does it again: neither writes what the
// ledger or the job already has.
func (r *durableJobRunner) startNotionMigrationRunFromLedger(job storedJob, payload notionImportJobPayload) error {
	for _, stoppedJob := range payload.LedgerFrom {
		if err := recordNotionMigrationLedger(r.app, payload.MigrationID, stoppedJob); err != nil {
			return err
		}
	}
	if payload.LedgerPreload {
		// What a template's body left there before is forgotten first
		// (notion_migration_template_body_leftovers.go).
		if err := forgetNotionMigrationTemplateBodyLeftovers(r.app, payload.MigrationID); err != nil {
			return err
		}
		if err := preloadNotionMigrationLedger(r.app, payload.MigrationID, job.ID); err != nil {
			return err
		}
	}
	payload.notionMigrationLedgerStart = notionMigrationLedgerStart{}
	return r.commitNotionDiscovery(job, payload, nil)
}

// notionMigrationTopShows is the one rule of every lookup of a run's top page
// — the ledger's (notionMigrationImportTop), the job's own and the stopped
// runs' it takes over: the page shows, so not one still staging. A run
// stopped before it published leaves its pages staging for the run that
// takes it over, which records them in the ledger in its first turn and
// publishes them only at its end; until then its answer and the list name no
// top page, as for a first run (2026-11-17: a run again after a first run
// stopped before it published answered with a staging page from the ledger,
// a link to a page nobody could open yet).
const notionMigrationTopShows = `JOIN {{leafen_pages}} AS [[shown]] ON [[shown]].[[id]]=[[top]].[[native_id]] AND [[shown]].[[notion_import_staging]]=FALSE`

// notionMigrationTakenOverTop is the run's top page as the ledger will have
// it once its first turn records the stopped runs it takes over: the page a
// stopped run made of the migration's first root it made and shows, as
// notionMigrationImportTop reads it from the ledger.
func notionMigrationTakenOverTop(app core.App, stored storedNotionMigration, jobID string) (string, error) {
	payload := loadNotionMigrationStoppedPayload(app, jobID)
	if payload == nil {
		return "", nil
	}
	for index := len(payload.LedgerFrom) - 1; index >= 0; index-- {
		pageID := ""
		err := app.DB().NewQuery(`
			SELECT [[top]].[[native_id]] FROM {{leafen_notion_import_mappings}} AS [[top]]
			` + notionMigrationTopShows + `
			JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration]].[[id]]={:migrationId}
			JOIN json_each([[migration]].[[roots_json]]) AS [[root]]
			WHERE [[top]].[[job_id]]={:jobId} AND [[top]].[[kind]]='page' AND [[top]].[[native_id]]!=''
				AND [[top]].[[source_id]]=CASE WHEN json_extract([[root]].[[value]], '$.kind')='database'
					THEN 'notion-database-root-page:' || json_extract([[root]].[[value]], '$.id')
					ELSE json_extract([[root]].[[value]], '$.id') END
			ORDER BY [[root]].[[key]] LIMIT 1
		`).Bind(dbx.Params{"migrationId": stored.ID, "jobId": payload.LedgerFrom[index]}).Row(&pageID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if pageID != "" {
			return pageID, nil
		}
	}
	return "", nil
}

// notionMigrationJobTopPage is the page a job made of the migration's first
// root it made, once it shows.
func notionMigrationJobTopPage(app core.App, stored storedNotionMigration, jobID string) (string, error) {
	pageID := ""
	err := app.DB().NewQuery(`
		SELECT [[top]].[[native_id]] FROM {{leafen_notion_import_mappings}} AS [[top]]
		` + notionMigrationTopShows + `
		JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration]].[[id]]={:migrationId}
		JOIN json_each([[migration]].[[roots_json]]) AS [[root]]
		WHERE [[top]].[[job_id]]={:jobId} AND [[top]].[[kind]]='page'
			AND [[top]].[[source_id]] IN (json_extract([[root]].[[value]], '$.id'), 'notion-database-root-page:' || json_extract([[root]].[[value]], '$.id'))
		ORDER BY [[root]].[[key]] LIMIT 1
	`).Bind(dbx.Params{"migrationId": stored.ID, "jobId": jobID}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}
