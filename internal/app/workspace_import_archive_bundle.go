package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A bundle another tool made can go into a workspace a migration named as
// its source (notion_bundle_sources.go). Its IDs are then made from the
// migration's connection, and what the tool said it couldn't carry goes to
// the source, for the migration's report.

const (
	workspaceBundleLossesMax     = 200
	workspaceBundleLossWhatRunes = 200
	workspaceBundleLossWhyRunes  = 500
)

func validWorkspaceBundleLosses(losses []workspaceBundleLoss) bool {
	if len(losses) > workspaceBundleLossesMax {
		return false
	}
	for _, loss := range losses {
		what := strings.TrimSpace(loss.What)
		if what == "" || utf8.RuneCountInString(what) > workspaceBundleLossWhatRunes || strings.ContainsAny(loss.What, "\r\n") ||
			utf8.RuneCountInString(loss.Why) > workspaceBundleLossWhyRunes || loss.Count < 1 || loss.Count > 1_000_000_000 {
			return false
		}
	}
	return true
}

// workspaceBundleStagingConnection is the migration connection whose
// source waits for its bundle in this workspace, "" when none does, and
// whether its bundles' pages take page IDs (a source made since they do).
func workspaceBundleStagingConnection(txApp core.App, ownerID, workspaceID string) (string, bool, error) {
	var connectionID string
	var pageIDs bool
	err := txApp.DB().NewQuery(`
		SELECT [[source]].[[connection_id]], [[source]].[[page_ids]] FROM {{leafen_bundle_import_sources}} AS [[source]]
		JOIN {{leafen_notion_import_connections}} AS [[connection]] ON [[connection]].[[id]] = [[source]].[[connection_id]]
		WHERE [[source]].[[staging_workspace_id]] = {:workspaceId} AND [[source]].[[status]] = 'importing'
			AND [[source]].[[import_job_id]] = '' AND [[connection]].[[owner]] = {:owner} AND [[connection]].[[active]] = 1
		LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "owner": ownerID}).Row(&connectionID, &pageIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return connectionID, pageIDs, err
}

// claimWorkspaceBundleStaging gives the waiting source its import.
func claimWorkspaceBundleStaging(txApp core.App, connectionID, jobID string) error {
	_, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_bundle_import_sources}} SET [[import_job_id]] = {:job}, [[updated]] = {:now}
		WHERE [[connection_id]] = {:connection} AND [[import_job_id]] = ''
	`).Bind(dbx.Params{"job": jobID, "connection": connectionID, "now": time.Now().UTC().UnixMilli()}).Execute()
	return err
}

// recordWorkspaceBundleLosses gives the source what the tool couldn't
// carry, as its bundle is staged.
func recordWorkspaceBundleLosses(txApp core.App, job storedJob, payload workspaceImportJobPayload, losses []workspaceBundleLoss) error {
	if payload.BundleConnectionID == "" {
		return nil
	}
	if losses == nil {
		losses = []workspaceBundleLoss{}
	}
	encoded, err := json.Marshal(losses)
	if err != nil {
		return err
	}
	_, err = txApp.DB().NewQuery(`
		UPDATE {{leafen_bundle_import_sources}} SET [[losses]] = {:losses}, [[updated]] = {:now}
		WHERE [[connection_id]] = {:connection} AND [[import_job_id]] = {:job}
	`).Bind(dbx.Params{"losses": string(encoded), "now": time.Now().UTC().UnixMilli(),
		"connection": payload.BundleConnectionID, "job": job.ID}).Execute()
	return err
}
