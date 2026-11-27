package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How long a run waits for Notion to answer whether a connection reaches the
// migration, and how many of the person's other connections it asks.
const (
	notionMigrationConnectionProbe  = 5 * time.Second
	notionMigrationConnectionsAsked = 5
)

// notionMigrationReachingConnection is the connection a migration's next run
// goes through: its own while Notion still gives it the migration's first
// root, else the person's newest connection here that does. A token replaced,
// or an integration made anew, carries the migration on; when none reads the
// root the run goes through its own and says why it stopped.
func notionMigrationReachingConnection(ctx context.Context, app core.App, stored storedNotionMigration) string {
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &roots); err != nil || len(roots) == 0 || stored.SourceLabel != "" {
		return stored.ConnectionID
	}
	reads := func(connection storedNotionImportConnection) bool {
		token, err := decryptNotionConnection(app, connection)
		if err != nil {
			return false
		}
		asked, cancel := context.WithTimeout(ctx, notionMigrationConnectionProbe)
		defer cancel()
		client := newNotionProviderClient(token)
		if roots[0].Kind == "database" {
			_, err = client.database(asked, roots[0].ID)
			// A database of several data sources is read in the data
			// source version.
			if notionProviderRefused(err) {
				if answer, known := client.readsDatabase(asked, roots[0].ID); known && answer == notionScopeParentShown {
					err = nil
				}
			}
		} else {
			_, err = client.page(asked, roots[0].ID)
		}
		return err == nil
	}
	own, err := loadNotionConnection(app, stored.ConnectionID, stored.Owner, stored.WorkspaceID)
	if err == nil && reads(own) {
		return stored.ConnectionID
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return stored.ConnectionID
	}
	others := []storedNotionImportConnection{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]],
			[[label]], [[token_cipher]], [[active]]
		FROM {{leafen_notion_import_connections}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId} AND [[active]] = 1 AND [[id]] != {:own}
		ORDER BY [[created]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": stored.Owner, "workspaceId": stored.WorkspaceID, "own": stored.ConnectionID,
		"limit": notionMigrationConnectionsAsked,
	}).All(&others); err != nil {
		return stored.ConnectionID
	}
	for _, other := range others {
		if reads(other) {
			return other.ID
		}
	}
	return stored.ConnectionID
}

// carryNotionImportJobOnConnection has a stopped migration's job go on through
// the connection that reaches it now.
func carryNotionImportJobOnConnection(ctx context.Context, app core.App, jobID, owner, workspaceID string) error {
	job, err := loadStoredJob(app, jobID, owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (!ownedJobMatches(job, notionImportJobKind, workspaceID) || job.Status != durableJobStatusFailed)) {
		return nil
	}
	if err != nil {
		return err
	}
	payload := notionImportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	if payload.MigrationID == "" {
		return nil
	}
	stored, err := loadNotionMigration(app, owner, workspaceID, payload.MigrationID)
	if errors.Is(err, errNotionMigrationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	connectionID := notionMigrationReachingConnection(ctx, app, stored)
	if connectionID == payload.ConnectionID {
		return nil
	}
	payload.ConnectionID = connectionID
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload}
			WHERE [[id]] = {:id} AND [[status]] = 'failed'
		`).Bind(dbx.Params{"id": job.ID, "payload": string(encoded)}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_migrations}} SET [[connection_id]] = {:connectionId} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": stored.ID, "connectionId": connectionID}).Execute()
		return err
	})
}
