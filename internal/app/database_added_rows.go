package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where a person may add rows: any database whose page they may change but
// the sources the server keeps for itself — a workspace's People
// (people_database.go), whose rows are its people, made by the server as
// they join and by no one else, and the unnamed source a view
// block stands on (a waiting link's, a dashboard's: database_view_block.go),
// which is no database at all. So every list of places to add rows to
// leaves them out, as Notion's leave its People out (seen 2026-11-19 in
// Chrome at 1440, its Business-trial owner, in two tabs: the file windows'
// "위치 가져오기", the CSV window's "데이터베이스 선택", "옮기기" and an
// automation's "페이지 추가 위치" find nothing by "People"; "페이지 편집
// 위치" too, seen once), while the lists of databases to read — a linked
// view's "데이터 소스 연결", a relation's "관계형 대상" — offer People: an
// import's databases (workspace_import_csv_merge_targets.go) and the
// picker's list when it is asked for databases to add rows to
// (database_container_picker.go with "adds=rows", which offers no view
// block anyway). The writes those lists lead to check the same rule: a file
// import or a CSV going into a database, a page or a row moved into one,
// and an action's database read for setting it up.

// databaseSourceTakesAddedRowsSQL holds for the data source the expression
// names when a person may add rows to it.
func databaseSourceTakesAddedRowsSQL(dataSourceExpression string) string {
	return `(` + dataSourceExpression + ` NOT IN (SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}})
		AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_sources}} AS [[added_rows_membership]]
			JOIN {{leafen_database_containers}} AS [[added_rows_container]]
				ON [[added_rows_container]].[[id]] = [[added_rows_membership]].[[container_id]]
			WHERE [[added_rows_membership]].[[data_source_id]] = ` + dataSourceExpression + `
				AND [[added_rows_membership]].[[source_container_id]] = ''
				AND ` + databaseViewBlockSQL("[[added_rows_container]]") + `
		))`
}

// workspaceSourcesTakingNoAddedRows are the People sources of a workspace,
// for a list that goes through its sources one by one and offers no view
// block's already.
func workspaceSourcesTakingNoAddedRows(app core.App, workspaceID string) (map[string]bool, error) {
	sources := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}} WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Column(&sources); err != nil {
		return nil, err
	}
	refused := make(map[string]bool, len(sources))
	for _, source := range sources {
		refused[source] = true
	}
	return refused, nil
}
