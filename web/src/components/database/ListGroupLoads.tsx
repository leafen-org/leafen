import { useCallback, useState } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { DbView } from "@/lib/types";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { CardsLoadMore } from "./CardsLoadMore";
import { groupRowLoads } from "./queryProjection";

type GroupRow = { id: string; subitemParentId?: string | null };
const NO_ROWS: ReadonlySet<string> = new Set();

/**
 * How many of a list's, gallery's or timeline's group rows show: each group's
 * first top rows or cards with their sub-items, and more at each "↓ 더
 * 불러오기" by its "+ 새 페이지" — 25 and 50 more unless its "처음 불러오기" says, inline
 * and on its database's own page (groupRowLoads; Notion's desktop, seen
 * 2026-11-27), on a phone's own page 10 and 10 more, as Notion's phone (seen
 * 2026-11-15 at 402pt and on a Galaxy A90 at 411: a grouped list and a
 * grouped gallery 10, then "새 페이지", then "더 불러오기").
 */
export function useListGroupLoads(view: Pick<DbView, "config">, made: ReadonlySet<string> = NO_ROWS, onLimit?: (limit: number) => void) {
  const ownPage = useDatabaseOwnPage(), phone = useSidebarLayout(true).phone;
  const { first, more } = groupRowLoads(view, phone && ownPage);
  const [shown, setShown] = useState<ReadonlyMap<string, number>>(new Map());
  const cut = useCallback(<Row extends GroupRow>(key: string, rows: readonly Row[]): { rows: readonly Row[]; more: boolean } => {
    const byId = new Map(rows.map((row) => [row.id, row]));
    const topOf = (row: Row) => {
      let at = row;
      for (let parent = byId.get(at.subitemParentId ?? ""); parent && parent !== row; parent = byId.get(at.subitemParentId ?? "")) at = parent;
      return at.id;
    };
    const tops = rows.filter((row) => !byId.has(row.subitemParentId ?? ""));
    // Its first ones, and any made here since it opened, which show at once (as a table's, tableRowWindow).
    const count = shown.get(key) ?? first;
    if (tops.length <= count) return { rows, more: false };
    const kept = new Set(tops.filter((row, index) => index < count || made.has(row.id)).map((row) => row.id));
    return { rows: rows.filter((row) => kept.has(topOf(row))), more: kept.size < tops.length };
  }, [first, made, shown]);
  const loadMore = useCallback((key: string) => {
    setShown((current) => new Map(current).set(key, (current.get(key) ?? first) + more));
  }, [first, more]);
  return { cut, loadMore, step: first, onLimit };
}

/** A group's "↓ 더 불러오기", under its "+ 새 페이지", its "⋯" choosing how many each group first shows (seen 2026-11-27 in two tabs). */
export function ListGroupMore({ groupKey, measure, index, loads }: {
  groupKey: string; measure?: (element: HTMLElement | null) => void; index: number; loads: ReturnType<typeof useListGroupLoads>;
}) {
  return (
    <div ref={measure} data-index={index} data-list-group-more={groupKey}>
      <CardsLoadMore placement="view" limit={loads.step} onLimit={loads.onLimit} onLoadMore={() => loads.loadMore(groupKey)} />
    </div>
  );
}
