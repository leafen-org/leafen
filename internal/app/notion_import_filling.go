package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source a Notion import is still making answers a query or a fetch
// with a line that says so — the step the import is at and how much it has
// made — so an agent doesn't take its rows so far, or none, for all of them
// (2026-11-16: an agent read a migration's data source mid-run as an empty
// database). A data source no import is making answers as it is, empty or
// not. What an import is making is what its job made of a Notion database
// (its mappings) and, for a migration's run again, what the runs before made
// (the migration's ledger), from its first step to its last. One the runs
// before moved whole says that instead, and that the run again brings in
// only Notion's changes (notion_import_filling_again.go).

// notionImportFilling is a running import making a data source, with when
// the data source was made.
type notionImportFilling struct {
	DataSourceID string `db:"native_id"`
	JobID        string `db:"job_id"`
	Created      int64  `db:"created"`
}

// notionImportsFilling finds which of dataSourceIDs a Notion import that
// hasn't ended is making, each with its job.
func notionImportsFilling(app core.App, dataSourceIDs []string) ([]notionImportFilling, error) {
	ids := slices.Compact(slices.Sorted(slices.Values(dataSourceIDs)))
	if len(ids) == 0 {
		return nil, nil
	}
	params := dbx.Params{"kind": notionImportJobKind, "queued": durableJobStatusQueued}
	names := make([]string, 0, len(ids))
	for index, id := range ids {
		name := fmt.Sprintf("source%d", index)
		params[name] = id
		names = append(names, "{:"+name+"}")
	}
	in := strings.Join(names, ", ")
	found := []notionImportFilling{}
	if err := app.DB().NewQuery(`
		SELECT [[mapping]].[[native_id]], [[mapping]].[[job_id]], ` + notionImportFillingCreatedSQL("mapping") + ` AS [[created]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_jobs}} AS [[job]] ON [[job]].[[id]] = [[mapping]].[[job_id]]
		WHERE [[mapping]].[[native_id]] IN (` + in + `) AND [[mapping]].[[kind]] = 'data_source'
			AND [[job]].[[kind]] = {:kind} AND [[job]].[[status]] = {:queued}
		UNION
		SELECT [[ledger]].[[native_id]], [[job]].[[id]], ` + notionImportFillingCreatedSQL("ledger") + `
		FROM {{leafen_jobs}} AS [[job]]
		JOIN {{leafen_notion_migration_ledger}} AS [[ledger]]
			ON [[ledger]].[[migration_id]] = json_extract([[job]].[[payload_json]], '$.migrationId')
		WHERE [[job]].[[kind]] = {:kind} AND [[job]].[[status]] = {:queued}
			AND [[ledger]].[[native_id]] IN (` + in + `) AND [[ledger]].[[kind]] = 'data_source'
		ORDER BY 1, 2
	`).Bind(params).All(&found); err != nil {
		return nil, err
	}
	return found, nil
}

// notionImportFillingCreatedSQL is when the data source a row of from names
// was made, 0 when it isn't there.
func notionImportFillingCreatedSQL(from string) string {
	return `COALESCE((SELECT [[made]].[[created]] FROM {{leafen_data_sources}} AS [[made]] WHERE [[made]].[[id]] = [[` + from + `]].[[native_id]]), 0)`
}

// notionImportFillingJob is where a running import making a data source is,
// in words: its stage as its status and screen name it (reading, creating,
// comments, verifying) and its step (Stage), with how much it has made
// (Where), and where to follow it; and what the runs before it moved.
type notionImportFillingJob struct {
	Owner, Stage, Where, Follow string
	Moved                       notionImportMovedBefore
}

func loadNotionImportFillingJob(app core.App, jobID string) (notionImportFillingJob, bool) {
	job, err := loadStoredJob(app, jobID, "")
	if err != nil {
		return notionImportFillingJob{}, false
	}
	payload := notionImportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil {
		return notionImportFillingJob{}, false
	}
	filling := notionImportFillingJob{
		Owner: job.Owner,
		Stage: fmt.Sprintf("stage %q, at the step %q", notionImportJobStage(app, job, payload),
			notionImportStageName(accountLanguageEn, notionImportStage(app, job, payload))),
		Follow: "the import screen shows where it is",
		Moved:  notionImportMovedBeforeOf(app, job, payload),
	}
	filling.Where = filling.Stage
	if job.Total > 0 && job.Processed < job.Total {
		filling.Where += fmt.Sprintf(", %d of %d made", job.Processed, job.Total)
	}
	if payload.MigrationID != "" {
		filling.Follow = "leafen_notion_migration_status shows where the run is"
	}
	return filling, true
}

// notionImportFillingNotices say, for each of dataSourceIDs an import is
// making, that it is and where the import is — or, for one the runs before
// moved whole, that it is all there and only Notion's changes are coming;
// none when no import is. They are a read's aside: one that can't be worked
// out leaves the read as it is.
func notionImportFillingNotices(app core.App, dataSourceIDs ...string) []string {
	filling, err := notionImportsFilling(app, dataSourceIDs)
	if err != nil || len(filling) == 0 {
		return nil
	}
	notices := []string{}
	said := map[string]bool{}
	for _, each := range filling {
		job, ok := loadNotionImportFillingJob(app, each.JobID)
		if said[each.DataSourceID] || !ok {
			continue
		}
		said[each.DataSourceID] = true
		if job.Moved.whole(each.Created) {
			notices = append(notices, notionImportFillingAgainNotice(app, each.DataSourceID, job))
			continue
		}
		notices = append(notices, fmt.Sprintf(
			"%s is still being imported from Notion (%s): its rows may be missing or not yet as Notion has them, "+
				"so an empty or short answer isn't all it holds. Read it again once the import ends; %s.",
			mcpDataSourceURL(app, each.DataSourceID), job.Where, job.Follow,
		))
	}
	return notices
}

// notionImportFillingRefusal says of a data source the reader can't open
// yet because their own import is still making it — its pages show once the
// import comes to them — that it is, rather than that they can't open it;
// any other refusal stays as it is, one of a data source the runs before
// moved whole too: its pages show already, so the run again isn't why.
func notionImportFillingRefusal(ctx context.Context, app core.App, refusal error, dataSourceIDs ...string) error {
	if !errors.Is(refusal, errMCPScopeDenied) {
		return refusal
	}
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return refusal
	}
	filling, err := notionImportsFilling(app, dataSourceIDs)
	if err != nil {
		return refusal
	}
	for _, each := range filling {
		if job, ok := loadNotionImportFillingJob(app, each.JobID); ok && job.Owner == authority.PrincipalID && !job.Moved.whole(each.Created) {
			return mcpDenied(
				"%s is still being imported from Notion (%s): it opens once the import shows its pages. Read it again then; %s",
				mcpDataSourceURL(app, each.DataSourceID), job.Where, job.Follow,
			)
		}
	}
	return refusal
}

// withNotionImportFilling adds to a query's answer what notices its data
// sources have (notionImportFillingNotices).
func withNotionImportFilling(app core.App, output notionMCPQueryDataSourcesOutput, dataSourceIDs ...string) notionMCPQueryDataSourcesOutput {
	output.Warnings = append(output.Warnings, notionImportFillingNotices(app, dataSourceIDs...)...)
	return output
}
