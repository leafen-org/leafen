import type { ContentGraph, GraphOperation } from "@/contentGraph";

/**
 * Whether a write edits its page as Notion's last edited time counts it, as
 * the server notes it (content_graph_page_edit.go; Notion seen 2026-11-16 two
 * ways): blocks made, moved or removed edit any page, and on a database's own
 * page so does a change to what the database is — its sources, views or
 * properties (a property added or renamed, a new option from a cell, a view
 * added or its settings saved for everyone) — or a row it adds. A value
 * written to a row already there edits that row's page alone, and an inline
 * database's changes leave the page that holds it as it was.
 */
export function graphWriteEditsPage(graph: ContentGraph, operations: readonly GraphOperation[], databasePage: boolean): boolean {
  let rowsWritten = false;
  for (const operation of operations) {
    switch (operation.kind) {
      case "upsertBlock":
      case "moveBlock":
      case "deleteBlock":
        return true;
      case "upsertDataSource":
      case "deleteDataSource":
      case "upsertView":
      case "updateView":
      case "deleteView":
      case "upsertProperty":
      case "deleteProperty":
        if (databasePage) return true;
        break;
      case "upsertRow":
        rowsWritten = true;
        break;
      default:
        break;
    }
  }
  if (!databasePage || !rowsWritten) return false;
  // A row the graph doesn't hold yet is one this write adds.
  return operations.some((operation) => operation.kind === "upsertRow" && !graph.rows.some(({ id }) => id === operation.row.id));
}
