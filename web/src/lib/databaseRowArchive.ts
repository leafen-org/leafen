import type { GraphRow } from "@/contentGraph";
import { keptItems } from "@/lib/rowsReplaced";

const activeKept = new WeakMap<readonly GraphRow[], readonly GraphRow[]>();
const archivedKept = new WeakMap<readonly GraphRow[], readonly GraphRow[]>();
const isActive = (row: GraphRow) => !row.archivedAt;
const isArchived = (row: GraphRow) => Boolean(row.archivedAt);
const rowId = (row: GraphRow) => row.id;

/**
 * An archived database row stays in its database: views show the rows that
 * are not archived, and the database settings list the archived ones. Each
 * is kept for the list as read, and changed alike when rows are changed in
 * it (lib/rowsReplaced.ts); with none archived the active are the list.
 */
export function splitArchivedDatabaseRows(rows: readonly GraphRow[]) {
  return {
    active: keptItems(rows, activeKept, isActive, rowId) as GraphRow[],
    archived: keptItems(rows, archivedKept, isArchived, rowId) as GraphRow[],
  };
}
