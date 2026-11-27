package app

import (
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The ways in a read log records (read_log.go): each serves its reads to
// recordContentReads as it gives them out.

// sortedReadIDs is a list of IDs as reads of one kind.
func sortedReadIDs(kind, how string, ids []string) []contentRead {
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	reads := make([]contentRead, 0, len(sorted))
	for _, id := range sorted {
		reads = append(reads, contentRead{Kind: kind, ID: id, How: how})
	}
	return reads
}

// recordAPIReads records what the Notion-compatible API gave a connection.
func recordAPIReads(app core.App, identity notionOAuthBearerIdentity, reads []contentRead) {
	recordContentReads(app, identity.WorkspaceID, contentReader{UserID: identity.PrincipalID, Via: contentReadViaAPI, Client: identity.ClientID}, reads)
}

// contentReadOfPage is a page record read on its own: a database item's
// page reads as the item.
func contentReadOfPage(page *core.Record) contentRead {
	if isDatabaseRowPageRecord(page) {
		return contentRead{Kind: "row", ID: page.Id, How: contentReadOpen}
	}
	return contentRead{Kind: "page", ID: page.Id, How: contentReadOpen}
}

// recordPublicContentReads records what a share link, a site or a view
// link showed a visitor, by the link (never its secret).
func recordPublicContentReads(app core.App, snapshot publicContentSnapshotResponse) {
	reads := []contentRead{}
	if snapshot.Page.ID != "" {
		reads = append(reads, contentRead{Kind: "page", ID: snapshot.Page.ID, How: contentReadOpen})
	}
	for _, item := range snapshot.Graph {
		if id, _ := item.Value["id"].(string); item.Kind == "row" && id != "" {
			reads = append(reads, contentRead{Kind: "row", ID: id, How: contentReadList})
		}
	}
	recordContentReads(app, snapshot.Page.WorkspaceID, contentReader{Via: contentReadViaShare, Client: snapshot.SourceKind + ":" + snapshot.AuthorityID}, reads)
}

// recordExportReads records what a downloaded export holds: each page and
// item of a page's or a database's export; for the whole workspace's, each
// of its top pages the export took, as the whole of them.
func recordExportReads(app core.App, userID string, job workspaceExportJob) {
	reads := []contentRead{}
	switch job.Scope.Kind {
	case workspaceExportScopeWorkspace:
		tops := []string{}
		// The top pages are read by their parent, the row and import marks
		// after the row (+): SQLite, which has no statistics, took the
		// (workspace, import) index and read the workspace's every page.
		_ = app.DB().NewQuery(`
			SELECT 'page:' || [[id]] FROM {{leafen_pages}}
			WHERE [[workspace]] = {:workspaceId} AND [[parent_id]] = '' AND +[[data_source_id]] = '' AND +[[notion_import_staging]] = FALSE
				AND ([[teamspace_id]] != '' OR [[private_owner_id]] = {:userId})
			UNION SELECT 'page:' || [[page_id]] FROM {{leafen_workspace_export_scope_pages}} WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"workspaceId": job.WorkspaceID, "userId": userID, "jobId": job.ID}).Column(&tops)
		reads = sortedReadIDs("scope", contentReadOpen, tops)
	default:
		pages := []struct {
			ID      string `db:"page_id"`
			RowPage bool   `db:"row_page"`
		}{}
		_ = app.DB().NewQuery(`SELECT [[page_id]], [[row_page]] FROM {{leafen_workspace_export_scope_pages}} WHERE [[job_id]] = {:jobId}`).
			Bind(dbx.Params{"jobId": job.ID}).All(&pages)
		for _, page := range pages {
			kind := "page"
			if page.RowPage {
				kind = "row"
			}
			reads = append(reads, contentRead{Kind: kind, ID: page.ID, How: contentReadOpen})
		}
		rows := []string{}
		_ = app.DB().NewQuery(`
			SELECT [[row]].[[id]] FROM {{leafen_database_rows}} AS [[row]]
			WHERE (` + databaseSourceRowsSQL("[[row]]", "{:target}") + `) OR [[row]].[[page_id]] = {:target}
				OR [[row]].[[page_id]] IN (SELECT [[page_id]] FROM {{leafen_workspace_export_scope_pages}} WHERE [[job_id]] = {:jobId})
		`).Bind(dbx.Params{"target": job.Scope.TargetID, "jobId": job.ID}).Column(&rows)
		reads = append(reads, sortedReadIDs("row", contentReadOpen, rows)...)
	}
	recordContentReads(app, job.WorkspaceID, contentReader{UserID: userID, Via: contentReadViaExport, Client: job.Format}, reads)
}

// recordMCPReads records what an agent's connection was given.
func recordMCPReads(app core.App, authority mcpAuthority, workspaceID string, reads []contentRead) {
	recordContentReads(app, workspaceID, contentReader{UserID: authority.PrincipalID, Via: contentReadViaMCP, Client: authority.ClientName}, reads)
}
