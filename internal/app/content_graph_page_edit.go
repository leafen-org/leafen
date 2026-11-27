package app

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// noteGraphPageEdited notes a graph change on its page's last edited time
// and editor where Notion's counts it (seen 2026-11-16 two ways, the top
// bar's 편집 with the ⋯ menu's 최종 편집 and the API's last_edited_by, in two
// tabs): a change to the page's blocks or text, and on a database's own page
// also a change to what the database is — a property added, renamed or given
// a new option (from a cell too), a view added or its settings saved for
// everyone, the database renamed — and a row added, by "+ 새 페이지" or
// "새로 만들기". A value written to a row already there edits that row's
// page alone, a row put in the trash leaves the database's page as it was,
// and so do an inline database's changes the page that holds it. What the
// server writes itself — People's rows, an ID numbered, a relation mirrored,
// an import — is no one's edit of the database.
func noteGraphPageEdited(
	ctx context.Context, app core.App, pageID, actorID string, receipt graphMutationReceipt,
	operations []graphOperation, rows *pageHistoryRowCapture, hooks contentGraphMutationHooks,
) error {
	edited := receipt.Counts.Blocks > 0 || receipt.TextUpdate != ""
	someones := actorID != "" && actorID != leafenSystemAuthorID && !hooks.PreserveRowEditStamps &&
		hooks.WorkspaceImportReplaceJobID == "" && !hooks.WorkspaceImportReplaceRollback
	if !edited && someones && graphChangeShapesDatabase(receipt.Counts, operations, rows) {
		kind := ""
		if err := app.DB().NewQuery("SELECT [[kind]] FROM {{leafen_pages}} WHERE [[id]] = {:pageId}").
			WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).Row(&kind); err != nil {
			return err
		}
		edited = kind == pageKindDatabase
	}
	if !edited {
		return nil
	}
	// The system's rewrite of a database page's values keeps its own time
	// (PreserveRowEditStamps), and so does a discussion's mark.
	if hooks.PreserveRowEditStamps || hooks.CommentMarks {
		return notePageRecordEdited(ctx, app, pageID, actorID)
	}
	if err := notePageContentEdited(ctx, app, pageID, actorID); err != nil {
		return err
	}
	// A public connection's change reads as its bot's (connection_page_edits.go).
	return recordConnectionPageEdit(ctx, app, pageID)
}

// graphChangeShapesDatabase tells whether a change reaches what a database
// is rather than only the rows it already had: its sources, views or
// properties, or a row it adds.
func graphChangeShapesDatabase(counts graphMutationCounts, operations []graphOperation, rows *pageHistoryRowCapture) bool {
	if counts.DataSources > 0 || counts.Views > 0 || counts.Properties > 0 {
		return true
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil || rows == nil {
			continue
		}
		if _, existed := rows.before[operation.Row.ID]; !existed {
			return true
		}
	}
	return false
}
