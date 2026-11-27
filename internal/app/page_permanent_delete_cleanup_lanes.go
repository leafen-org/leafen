package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pagePermanentDeleteRowsLane deletes a page's database rows, in turns of the
// rows first in rowid order.
const pagePermanentDeleteRowsLane = 10

func cleanPagePermanentDeleteResidueLane(
	app core.App,
	pageID string,
	lane int,
) (bool, error) {
	params := dbx.Params{
		"pageId": pageID, "limit": maxPagePermanentDeleteItemsPerRequest,
	}
	statement := ""
	switch lane {
	case 0:
		statement = `
			UPDATE {{leafen_jobs}} SET [[status]] = 'canceled', [[updated]] = unixepoch('subsec') * 1000
			WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_jobs}}
				WHERE [[page_id]] = {:pageId} AND [[status]] = 'queued'
				ORDER BY [[rowid]] LIMIT {:limit}
			)`
	case 1:
		statement = `
			DELETE FROM {{leafen_job_chunks}} WHERE [[rowid]] IN (
				SELECT [[chunk]].[[rowid]] FROM {{leafen_job_chunks}} [[chunk]]
				JOIN {{leafen_jobs}} [[job]] ON [[job]].[[id]] = [[chunk]].[[job_id]]
				WHERE [[job]].[[page_id]] = {:pageId}
				ORDER BY [[chunk]].[[rowid]] LIMIT {:limit}
			)`
	case 2:
		statement = boundedPagePermanentDeleteSQL("leafen_job_effects", "page_id", "")
	case 3:
		statement = boundedPagePermanentDeleteSQL("leafen_jobs", "page_id", "")
	case 4:
		statement = `
			DELETE FROM {{leafen_database_query_dependency_values}} WHERE [[rowid]] IN (
				SELECT [[dependency]].[[rowid]]
				FROM {{leafen_database_query_dependency_values}} [[dependency]]
				JOIN {{leafen_database_query_jobs}} [[job]] ON [[job]].[[id]] = [[dependency]].[[job_id]]
				WHERE [[job]].[[page_id]] = {:pageId}
				ORDER BY [[dependency]].[[rowid]] LIMIT {:limit}
			)`
	case 5:
		statement = boundedPagePermanentDeleteSQL("leafen_database_query_jobs", "page_id", "")
	case 6:
		statement = boundedPagePermanentDeleteSQL("leafen_database_dependency_edges", "page_id", "")
	case 7:
		statement = boundedPagePermanentDeleteSQL("leafen_database_dependency_bindings", "page_id", "")
	case 8:
		statement = boundedPagePermanentDeleteSQL("leafen_database_relation_items", "page_id", "")
	case 9:
		statement = boundedPagePermanentDeleteSQL("leafen_database_row_values", "page_id", "")
	case pagePermanentDeleteRowsLane:
		statement = boundedPagePermanentDeleteSQL("leafen_database_rows", "page_id", "")
	case 11:
		statement = boundedPagePermanentDeleteSQL("leafen_database_properties", "page_id", "")
	case 12:
		statement = boundedDatabaseContainerMembershipDeleteSQL("leafen_database_container_views")
	case 13:
		statement = boundedDatabaseContainerMembershipDeleteSQL("leafen_database_container_sources")
	case 14:
		statement = boundedPagePermanentDeleteSQL("leafen_database_containers", "page_id", "")
	case 15:
		statement = boundedPagePermanentDeleteSQL("leafen_database_views", "page_id", "")
	case 16:
		statement = boundedPagePermanentDeleteSQL("leafen_data_sources", "page_id", "")
	case 17:
		statement = boundedPagePermanentDeleteSQL("leafen_blocks", "page_id", "")
	case 18:
		statement = boundedPagePermanentDeleteSQL("leafen_graph_mutations", "page_id", "")
	case 19:
		statement = boundedPagePermanentDeleteSQL("leafen_graph_changes", "page_id", "")
	case 20:
		statement = boundedPagePermanentDeleteSQL("leafen_ygo_updates", "room", "")
	case 21:
		statement = boundedPagePermanentDeleteSQL("leafen_ygo_structural_updates", "room", "")
	case 22:
		statement = boundedPagePermanentDeleteSQL("leafen_ygo_structural_documents", "room", "")
	case 23:
		statement = boundedPagePermanentDeleteSQL("leafen_ygo_documents", "room", "")
	case 24:
		return deleteWorkspaceSearchFTSPage(app, pageID)
	case 25:
		statement = boundedPagePermanentDeleteSQL("leafen_search_pages", "page_id", "")
	case 26:
		statement = boundedPagePermanentDeleteSQL("leafen_search_dirty", "page_id", "")
	case 27:
		statement = boundedPagePermanentDeleteSQL("leafen_interactive_automation_executions", "page_id", "")
	case 28:
		statement = boundedPagePermanentDeleteSQL(
			"leafen_notion_import_mappings", "native_id", "AND [[kind]] = 'page'",
		)
	case 29:
		statement = `
			UPDATE {{leafen_workspace_creation_receipts}} SET [[page_id]] = ''
			WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_workspace_creation_receipts}}
				WHERE [[page_id]] = {:pageId} ORDER BY [[rowid]] LIMIT {:limit}
			)`
	case 30:
		statement = boundedPagePermanentDeleteSQL("leafen_ygo_structural_quarantine", "room", "")
	case 31:
		statement = boundedPagePermanentDeleteSQL("leafen_ygo_update_quarantine", "room", "")
	case 32:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_restore_receipts", "page_id", "")
	case 33:
		statement = `
			DELETE FROM {{leafen_page_history_snapshot_blocks}} WHERE [[rowid]] IN (
				SELECT [[block]].[[rowid]] FROM {{leafen_page_history_snapshot_blocks}} [[block]]
				JOIN {{leafen_page_history_snapshot_jobs}} [[job]]
					ON [[job]].[[materialization_id]]=[[block]].[[materialization_id]]
				WHERE [[job]].[[page_id]]={:pageId}
				ORDER BY [[block]].[[rowid]] LIMIT {:limit}
			)`
	case 34:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_snapshot_jobs", "page_id", "")
	case 35:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_block_spans", "page_id", "")
	case 36:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_text_spans", "page_id", "")
	case 37:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_checkpoints", "page_id", "")
	case 38:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_events", "page_id", "")
	case 39:
		statement = boundedPagePermanentDeleteSQL("leafen_page_history_versions", "page_id", "")
	case 40:
		return cleanNotificationsForPermanentlyDeletedPage(
			app, pageID, maxPagePermanentDeleteItemsPerRequest,
		)
	case 41:
		statement = `
			DELETE FROM {{leafen_database_sql_query_jobs}} WHERE [[rowid]] IN (
				SELECT [[job]].[[rowid]]
				FROM {{leafen_database_sql_query_jobs}} AS [[job]]
				JOIN {{leafen_database_sql_query_sources}} AS [[source]]
					INDEXED BY [[idx_leafen_database_sql_query_sources_page]]
					ON [[source]].[[job_id]] = [[job]].[[id]]
				WHERE [[source]].[[page_id]] = {:pageId}
				GROUP BY [[job]].[[rowid]]
				ORDER BY [[job]].[[rowid]] LIMIT {:limit}
			)`
	case 42:
		statement = boundedPagePermanentDeleteSQL("leafen_block_trash_tombstones", "page_id", "")
	case 43:
		// A page's search chunks; their index rows went with its own (lane 24).
		statement = boundedPagePermanentDeleteSQL("leafen_search_chunks", "page_id", "")
	case 44, 45, 46, 47, 48, 49, 50, 51:
		// Its databases' automations, what they did and what they wait to do
		// (2026-10-24: nothing took them, and a workspace with one couldn't be
		// deleted), the definitions last.
		statement = boundedPagePermanentDeleteSQL(pagePermanentDeleteAutomationTables[lane-44], "page_id", "")
	default:
		return false, errPagePermanentDeleteUnavailable
	}
	// A lane with nothing of the page's runs no write: its own select of the
	// rows it would take is read first. A table with many triggers and
	// foreign keys makes even an empty DELETE a program of thousands of steps
	// to prepare — leafen_database_rows took 3.6 ms of the 10 ms each page of
	// a staged bundle took to go, its pages holding no rows (2026-11-24).
	if rows, ok := pagePermanentDeleteLaneRows(statement); ok {
		found := 0
		if err := app.DB().NewQuery("SELECT EXISTS (" + rows + ")").Bind(params).Row(&found); err != nil {
			return false, err
		}
		if found == 0 {
			return false, nil
		}
	}
	result, err := app.DB().NewQuery(statement).Bind(params).Execute()
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if lane == pagePermanentDeleteRowsLane && affected > 0 {
		// Rows whose pages held content of their own queued it (row_page_content_cleanup.go).
		if err := wakeRowPageContentCleanup(app); err != nil {
			return false, err
		}
	}
	return affected >= maxPagePermanentDeleteItemsPerRequest, nil
}

func boundedDatabaseContainerMembershipDeleteSQL(table string) string {
	pageIndex := "idx_leafen_database_container_sources_page_cleanup"
	membershipColumn := "data_source_id"
	// Rows on the deleted page go first. A database elsewhere loses only the
	// members it links from the deleted page, and their views: a linked one
	// that links others too keeps them, as in the page's 30 days
	// (database_gone_links_live.go). Each row is selected once so a short batch
	// means the lane is done.
	// The members a linked database links from the deleted page: lanes 12 and
	// 13 run before its data sources go (lane 16).
	linkedFromPage := `
					AND [[membership]].[[data_source_id]] IN (
						SELECT [[gone_source]].[[id]] FROM {{leafen_data_sources}} AS [[gone_source]]
						WHERE [[gone_source]].[[page_id]] = {:pageId})`
	linkedMemberCleanup := `
				UNION ALL
				SELECT [[membership]].[[rowid]], [[membership]].[[container_id]],
					[[membership]].[[data_source_id]] AS [[membership_id]]
				FROM {{leafen_database_containers}} AS [[source]]
					INDEXED BY [[idx_leafen_database_containers_page_cleanup]]
				JOIN {{leafen_database_container_sources}} AS [[membership]]
					INDEXED BY [[idx_leafen_database_container_sources_linked_source]]
					ON [[membership]].[[source_container_id]] = [[source]].[[id]]
				JOIN {{leafen_database_containers}} AS [[linking]]
					ON [[linking]].[[id]] = [[membership]].[[container_id]] AND [[linking]].[[kind]] = 'source'
					AND [[linking]].[[page_id]] != {:pageId}
				WHERE [[source]].[[page_id]] = {:pageId} AND [[source]].[[kind]] = 'source'`
	if table == "leafen_database_container_views" {
		pageIndex = "idx_leafen_database_container_views_page_cleanup"
		membershipColumn = "view_id"
		linkedFromPage = `
					AND EXISTS (
						SELECT 1 FROM {{leafen_database_views}} AS [[gone_view]]
						JOIN {{leafen_data_sources}} AS [[gone_source]] ON [[gone_source]].[[id]] = [[gone_view]].[[data_source_id]]
						WHERE [[gone_view]].[[id]] = [[membership]].[[view_id]] AND [[gone_source]].[[page_id]] = {:pageId})`
		// Views run before members, so the views a database owns for that
		// member are found through the member it is about to lose.
		linkedMemberCleanup = `
				UNION ALL
				SELECT [[membership]].[[rowid]], [[membership]].[[container_id]],
					[[membership]].[[view_id]] AS [[membership_id]]
				FROM {{leafen_database_containers}} AS [[source]]
					INDEXED BY [[idx_leafen_database_containers_page_cleanup]]
				JOIN {{leafen_database_container_sources}} AS [[linked_member]]
					INDEXED BY [[idx_leafen_database_container_sources_linked_source]]
					ON [[linked_member]].[[source_container_id]] = [[source]].[[id]]
				JOIN {{leafen_database_containers}} AS [[linking]]
					ON [[linking]].[[id]] = [[linked_member]].[[container_id]] AND [[linking]].[[kind]] = 'source'
					AND [[linking]].[[page_id]] != {:pageId}
				JOIN {{leafen_database_container_views}} AS [[membership]]
					ON [[membership]].[[container_id]] = [[linked_member]].[[container_id]]
				JOIN {{leafen_database_views}} AS [[view]]
					ON [[view]].[[id]] = [[membership]].[[view_id]]
					AND [[view]].[[data_source_id]] = [[linked_member]].[[data_source_id]]
				WHERE [[source]].[[page_id]] = {:pageId} AND [[source]].[[kind]] = 'source'`
	}
	return `
		DELETE FROM {{` + table + `}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM (
				SELECT [[membership]].[[rowid]], [[membership]].[[container_id]],
					[[membership]].[[` + membershipColumn + `]] AS [[membership_id]]
				FROM {{` + table + `}} AS [[membership]] INDEXED BY [[` + pageIndex + `]]
				WHERE [[membership]].[[page_id]] = {:pageId}
				UNION ALL
				SELECT [[membership]].[[rowid]], [[membership]].[[container_id]],
					[[membership]].[[` + membershipColumn + `]] AS [[membership_id]]
				FROM {{leafen_database_containers}} AS [[source]]
					INDEXED BY [[idx_leafen_database_containers_page_cleanup]]
				JOIN {{leafen_database_containers}} AS [[linked]]
					INDEXED BY [[idx_leafen_database_containers_source_target]]
					ON [[linked]].[[source_container_id]] = [[source]].[[id]]
					AND [[linked]].[[page_id]] != {:pageId}
				JOIN {{` + table + `}} AS [[membership]]
					ON [[membership]].[[container_id]] = [[linked]].[[id]]` + linkedFromPage + `
				WHERE [[source]].[[page_id]] = {:pageId} AND [[source]].[[kind]] = 'source'` + linkedMemberCleanup + `
				ORDER BY [[container_id]], [[membership_id]] LIMIT {:limit}
			)
		)`
}

// pagePermanentDeleteLaneRows is the select a lane's statement takes its
// rows by — every lane writes `WHERE [[rowid]] IN (<select>)` — or false.
func pagePermanentDeleteLaneRows(statement string) (string, bool) {
	const marker = "WHERE [[rowid]] IN ("
	start, end := strings.Index(statement, marker), strings.LastIndex(statement, ")")
	if start < 0 || end < start+len(marker) {
		return "", false
	}
	return strings.TrimSpace(statement[start+len(marker) : end]), true
}

func boundedPagePermanentDeleteSQL(table, key, extra string) string {
	return `
		DELETE FROM {{` + table + `}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{` + table + `}}
			WHERE [[` + key + `]] = {:pageId} ` + extra + `
			ORDER BY [[rowid]] LIMIT {:limit}
		)`
}

// pagePermanentDeleteAutomationTables are the tables a page's databases'
// automations keep, in the order lanes 44 to 51 clear them.
var pagePermanentDeleteAutomationTables = []string{
	"leafen_database_automation_events",
	"leafen_database_automation_outcomes",
	"leafen_database_automation_deliveries",
	"leafen_database_automation_schedule_occurrences",
	"leafen_database_automation_notifications",
	"leafen_database_automation_mutations",
	"leafen_database_automation_frontiers",
	"leafen_database_automation_definitions",
}
