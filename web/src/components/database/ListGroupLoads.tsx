import { useCallback, useState } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { DbView } from "@/lib/types";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { CardsLoadMore } from "./CardsLoadMore";
import { PHONE_GROUP_ROWS, chosenInitialLoadLimit } from "./queryProjection";

type GroupRow = { id: string; subitemParentId?: string | null };

/**
 * How many of a list's or gallery's group rows show: on a phone a database's
 * own page draws each group's first 10 top rows or cards (or the "처음
 * 불러오기" set) with their sub-items, and 10 more at each "↓ 더 불러오기"
 * under its "+ 새 페이지", as Notion's phone (seen 2026-11-15 at 402pt and on
 * a Galaxy A90 at 411: a grouped list and a grouped gallery 10, then "새
 * 페이지", then "더 불러오기"); elsewhere every row, as Notion's desktop.
 */
export function useListGroupLoads(view: Pick<DbView, "config">) {
  const ownPage = useDatabaseOwnPage(), phone = useSidebarLayout(true).phone;
  const step = phone && ownPage ? chosenInitialLoadLimit(view) ?? PHONE_GROUP_ROWS : undefined;
  const [shown, setShown] = useState<ReadonlyMap<string, number>>(new Map());
  const cut = useCallback(<Row extends GroupRow>(key: string, rows: readonly Row[]): { rows: readonly Row[]; more: boolean } => {
    if (step === undefined) return { rows, more: false };
    const byId = new Map(rows.map((row) => [row.id, row]));
    const topOf = (row: Row) => {
      let at = row;
      for (let parent = byId.get(at.subitemParentId ?? ""); parent && parent !== row; parent = byId.get(at.subitemParentId ?? "")) at = parent;
      return at.id;
    };
    const tops = rows.filter((row) => !byId.has(row.subitemParentId ?? ""));
    const count = shown.get(key) ?? step;
    if (tops.length <= count) return { rows, more: false };
    const kept = new Set(tops.slice(0, count).map((row) => row.id));
    return { rows: rows.filter((row) => kept.has(topOf(row))), more: true };
  }, [shown, step]);
  const loadMore = useCallback((key: string) => {
    if (step !== undefined) setShown((current) => new Map(current).set(key, (current.get(key) ?? step) + step));
  }, [step]);
  return { cut, loadMore, step: step ?? PHONE_GROUP_ROWS };
}

/** A group's "↓ 더 불러오기", under its "+ 새 페이지". */
export function ListGroupMore({ groupKey, measure, index, loads }: {
  groupKey: string; measure?: (element: HTMLElement | null) => void; index: number; loads: ReturnType<typeof useListGroupLoads>;
}) {
  return (
    <div ref={measure} data-index={index} data-list-group-more={groupKey}>
      <CardsLoadMore placement="view" limit={loads.step} onLoadMore={() => loads.loadMore(groupKey)} />
    </div>
  );
}
