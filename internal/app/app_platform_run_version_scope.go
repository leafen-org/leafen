//go:build !leafen_noapps

package app

import "github.com/pocketbase/dbx"

// appRunVersionScope is which page-history versions a run made, for what
// the app did (app_platform_run_undo.go): the versions the run recorded as
// its own (page_history_app_runs.go), or — for a run made before runs
// recorded them — its connection's versions from when it started to when
// its changes end.
type appRunVersionScope struct {
	run        string
	recorded   bool
	connection string
	from, to   int64
}

func appRunScopeOf(row storedAppRunHistory, through int64) appRunVersionScope {
	return appRunVersionScope{run: row.ID, recorded: row.PagesRecorded == 1, connection: row.Connection, from: row.Created, to: through}
}

// reaches is whether the run can have made any version.
func (scope appRunVersionScope) reaches() bool { return scope.recorded || scope.connection != "" }

// where is the condition on a query's [[version]] rows, with its values
// added to params.
func (scope appRunVersionScope) where(params dbx.Params) string {
	if scope.recorded {
		params["scopeRun"] = scope.run
		return `[[version]].[[id]] IN (SELECT [[version_id]] FROM {{leafen_app_run_versions}} WHERE [[run_id]] = {:scopeRun})`
	}
	params["scopeConnection"], params["scopeKeyed"] = scope.connection, "mcp:"+scope.connection
	params["scopeFrom"], params["scopeTo"] = scope.from, scope.to
	return `[[version]].[[connection_id]] IN ({:scopeConnection}, {:scopeKeyed}) AND [[version]].[[created_at]] BETWEEN {:scopeFrom} AND {:scopeTo}`
}
