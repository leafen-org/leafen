import { useMemo, useRef } from "react";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { useLendRowOrder, type LentRowOrder } from "./lentRowSiblings";
import { rowsOfItsGroup } from "./rowPeekGroup";
import { projectRows } from "./viewRowProjection";

type ShownRowsInput = {
  rows: readonly GraphRow[];
  properties: readonly GraphProperty[];
  view: GraphView;
  currentUserId?: string;
  pageId: string;
  workspaceId: string;
  subitems: boolean;
};

const shownRows = ({ rows, properties, view, currentUserId, pageId, workspaceId, subitems }: ShownRowsInput) =>
  projectRows({ currentUserId, pageId, properties, rows, view, workspaceId, subitems: subitems ? { faded: new Set<string>() } : undefined });

/** The rows before and after one among its group's as shown (rowsOfItsGroup); null for a row the view doesn't show. */
function siblingsAmong(shown: readonly GraphRow[], rowId: string, input: ShownRowsInput) {
  const order = rowsOfItsGroup(shown, rowId, input).map((row) => row.id);
  const index = order.indexOf(rowId);
  return index < 0 ? null : { previous: order[index - 1], next: order[index + 1] };
}

/**
 * The rows before and after the open one as its view shows them, its
 * filters and sorts applied and within its group, which a peek's ∧ and ∨
 * open as Notion's do (seen 2026-11-01); none while no row is open. A view
 * that opens its rows its own way (`lendTo`, its onOpenRow) lends its order
 * instead to the page peek that shows them (lentRowSiblings.ts).
 */
export function useRowPeekSiblings({ rowId, lendTo, ...input }: ShownRowsInput & {
  rowId: string | undefined;
  lendTo?: (rowId: string) => void;
}): { previous?: string; next?: string } {
  const { rows, properties, view, currentUserId, pageId, workspaceId, subitems } = input;
  const lendToNow = useRef(lendTo);
  lendToNow.current = lendTo;
  const lending = Boolean(lendTo);
  const lentOrder = useMemo((): LentRowOrder | null => {
    if (!lending) return null;
    const same = { rows, properties, view, currentUserId, pageId, workspaceId, subitems };
    // Worked out once a peek asks, not as the view draws.
    let shown: readonly GraphRow[] | null = null;
    return {
      siblingsOf: (id) => siblingsAmong(shown ??= shownRows(same), id, same),
      open: (id) => lendToNow.current?.(id),
    };
  }, [currentUserId, lending, pageId, properties, rows, subitems, view, workspaceId]);
  useLendRowOrder(lentOrder);
  return useMemo(() => {
    if (!rowId) return {};
    const same = { rows, properties, view, currentUserId, pageId, workspaceId, subitems };
    return siblingsAmong(shownRows(same), rowId, same) ?? {};
  }, [currentUserId, pageId, properties, rowId, rows, subitems, view, workspaceId]);
}

/**
 * The page a view shows first, its filters and sorts and the viewer's own
 * applied, which the page layout builder opened from it previews first, as
 * Notion's (seen 2026-11-14: r4 under a sort by Num, r2 once filtered over
 * 2 though not saved for everyone); none where it shows its pages in groups,
 * or shows none, Notion's builder then previewing a new page ("새 페이지").
 */
export function viewFirstShownRowId(input: Parameters<typeof projectRows>[0]): string | null {
  if (input.properties.some((property) => property.id === input.view.config.groupBy)) return null;
  const shown = projectRows(input), ids = new Set(shown.map((row) => row.id));
  // With sub-items, a page shown under its own: its parent's place.
  return shown.find((row) => !input.subitems || !row.subitemParentId || !ids.has(row.subitemParentId))?.id ?? null;
}
