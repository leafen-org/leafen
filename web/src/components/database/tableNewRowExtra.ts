import { createContext, useContext, type ReactNode } from "react";

/**
 * What sits by a view's "+ 새 페이지", made for the view's settings as its
 * reader has them (its filters theirs until saved for everyone): a relation
 * tab's "↗ 기존 항목 연결", as Notion's page layout (seen 2026-11-12) —
 * beside a table's or a list's, under each board column's and under a
 * gallery's tile; a calendar, timeline or chart has none.
 */
export const NewPageExtraContext = createContext<((config: Readonly<Record<string, unknown>>) => ReactNode) | null>(null);

/** What sits by the view's "+ 새 페이지", for its settings as shown. */
export function useNewPageExtra(config: Readonly<Record<string, unknown>> | undefined): ReactNode {
  const extra = useContext(NewPageExtraContext);
  return extra ? extra(config ?? {}) : null;
}

/** A table's lines as shown: each row's id and the row it is a sub-item of. */
type ShownLine = { row: { id: string; parentId: string | null } };
type ShownRows = { rows: readonly ShownLine[]; groups: readonly { key: string; rows: readonly ShownLine[] }[] };

/**
 * A row made in a table while its search is on, as Notion's (seen
 * 2026-11-15, "+ 새 페이지" under the rows a search left): the search is
 * cleared, its box staying open and empty, so the new row shows among all
 * the rows; one made at the end goes right after the last row the search
 * showed (in its group, under the same parent), not after the rows it hid.
 * Its title takes the caret as any new row's. Returns the row the new one
 * goes after.
 */
export function tableRowAfterSearch(search: unknown, clearSearch: (() => void) | undefined, shown: ShownRows,
  parentId: string | null, afterRowId: string | null, beforeRowId: string | undefined, groupKey: string | undefined): string | null {
  if (typeof search !== "string" || !search.trim() || !clearSearch) return afterRowId;
  clearSearch();
  if (afterRowId || beforeRowId) return afterRowId;
  const lines = groupKey === undefined ? shown.rows : shown.groups.find(({ key }) => key === groupKey)?.rows ?? [];
  const last = [...lines].reverse().find(({ row }) => row.parentId === parentId);
  return last?.row.id ?? null;
}
