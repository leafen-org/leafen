package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A version an app's run makes names the run, beside the connection it came
// through: what one run did is the versions it made, however its time
// overlaps another run's on the same connection (2026-11-16: two runs of an
// app's action pressed in quick succession — the later run listed both
// runs' rows, the earlier none, and undoing the later took back both). The
// run rides on the context of the run's own calls, as its connection does
// (page_history_connections.go), and each version it records is kept in
// leafen_app_run_versions.

type appRunContextKey struct{}

// withAppRunID makes the native calls made with ctx a run's.
func withAppRunID(ctx context.Context, runID string) context.Context {
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, appRunContextKey{}, runID)
}

// appRunIDOf is the run ctx's calls are made for, or "".
func appRunIDOf(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	runID, _ := ctx.Value(appRunContextKey{}).(string)
	return runID
}

// insertPageHistoryVersion inserts a version's row, and the run that made it
// when the connection names one.
func insertPageHistoryVersion(app core.App, connection pageHistoryConnection, version dbx.Params) error {
	if _, err := app.DB().Insert("leafen_page_history_versions", version).Execute(); err != nil {
		return err
	}
	if connection.AppRun == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_run_versions}} ([[version_id]], [[run_id]], [[page_id]], [[created_at]])
		VALUES ({:version}, {:run}, {:page}, {:at}) ON CONFLICT ([[version_id]]) DO NOTHING`).
		Bind(dbx.Params{"version": version["id"], "run": connection.AppRun, "page": version["page_id"], "at": version["created_at"]}).Execute()
	return err
}

// pageHistoryVersionAppRun is the run that made a version, or "".
func pageHistoryVersionAppRun(app core.App, versionID string) (string, error) {
	var runID string
	err := app.DB().NewQuery(`SELECT [[run_id]] FROM {{leafen_app_run_versions}} WHERE [[version_id]] = {:version}`).
		Bind(dbx.Params{"version": versionID}).Row(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return runID, err
}

// pageHistorySameAppRun is whether a version was made by the run a
// connection writes for — none for both counts — so one run's changes never
// go on in another's version.
func pageHistorySameAppRun(app core.App, versionID string, connection pageHistoryConnection) bool {
	runID, err := pageHistoryVersionAppRun(app, versionID)
	return err == nil && runID == connection.AppRun
}

// appRunPageVersions are the versions of a page one run made, as a set.
func appRunPageVersions(app core.App, runID, pageID string) (map[string]bool, error) {
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[version_id]] FROM {{leafen_app_run_versions}} WHERE [[run_id]] = {:run} AND [[page_id]] = {:page}`).
		Bind(dbx.Params{"run": runID, "page": pageID}).Column(&ids); err != nil {
		return nil, err
	}
	versions := make(map[string]bool, len(ids))
	for _, id := range ids {
		versions[id] = true
	}
	return versions, nil
}

// pageHistoryRevertPicks is which versions a revert from target takes back:
// target's alone; with connection, every later change of its connection,
// to through when given — and when through is given for a version an app's
// run made (the run's "되돌리기" in what the app did), that run's versions
// alone, however other runs' changes on the same connection fall between.
func pageHistoryRevertPicks(app core.App, pageID string, target storedPageHistoryVersion, input pageHistoryRevertRequest) func(storedPageHistoryVersion) bool {
	if !input.Connection {
		return func(version storedPageHistoryVersion) bool { return version.VersionCursor == target.VersionCursor }
	}
	if input.Through > 0 {
		if runID, err := pageHistoryVersionAppRun(app, target.ID); err == nil && runID != "" {
			if made, err := appRunPageVersions(app, runID, pageID); err == nil {
				return func(version storedPageHistoryVersion) bool { return made[version.ID] }
			}
		}
	}
	return func(version storedPageHistoryVersion) bool {
		return version.ConnectionID == target.ConnectionID && (input.Through == 0 || version.CreatedAt <= input.Through)
	}
}
