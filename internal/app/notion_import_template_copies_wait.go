package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An import ends only once the templates it made have their own copies of
// the databases of their bodies. A row that stood for such a template stays
// among its database's rows until those copies end (they are read from it,
// deferRowPageRemovalToDatabaseCopies), and the copies go on after the
// import's last step: an import that ended at that step said it was done
// while the template still stood as an ordinary row — in the table, a rows
// query, search and the row counts (2026-11-24, the gate's synthetic move:
// a rows query 25 ms after 'done' still listed the template '새 항목' of
// 장부, for some 100 ms on a fast Mac and for as long as a copy waits its
// turn, up to an hour, elsewhere). Notion lists no template among a
// database's rows. So the last step holds the import's end while a copy it
// deferred a row to is queued, and each copy's end wakes the import, which
// ends right after the last one.

// notionImportTemplateCopiesWait is how long the held end waits between its
// looks when no copy's end has woken it.
const notionImportTemplateCopiesWait = 2 * time.Second

// notionImportTemplateCopiesPending says whether a copy the import deferred
// a template's row to is still under way.
func notionImportTemplateCopiesPending(app core.App, importJobID string) (bool, error) {
	var pending int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_jobs}}
			WHERE [[kind]]={:kind} AND [[status]]={:queued}
				AND json_extract([[payload_json]], '$.importJobId')={:importJobId})
	`).Bind(dbx.Params{
		"kind": rowPageDatabaseCopyJobKind, "queued": durableJobStatusQueued, "importJobId": importJobID,
	}).Row(&pending)
	return pending != 0, err
}

// wakeNotionImportAfterTemplateCopy has an import whose end waits on a
// template's copy look again now that the copy has ended.
func wakeNotionImportAfterTemplateCopy(app core.App, importJobID string) error {
	if importJobID == "" {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]]={:now}
		WHERE [[id]]={:id} AND [[status]]={:queued} AND [[not_before]]>{:now}
	`).Bind(dbx.Params{"id": importJobID, "queued": durableJobStatusQueued, "now": now}).Execute(); err != nil {
		return err
	}
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	return nil
}
