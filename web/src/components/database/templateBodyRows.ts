import { useMemo, useRef } from "react";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import { nativeBoardQueryBounds } from "./boardViewModel";
import type { FilterThisPage } from "./filterThisPage";
import { graphViewFilterSeeds } from "./graphViewFilterSeeds";

type Makers = {
  effectiveView(view: GraphView): GraphView;
  tableAdapter?: NativeTableProductAdapter;
  boardAdapter?: NativeBoardProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
};

/**
 * A row made in a view in a database template's body, which the view keeps
 * to "this page" on a two-way relation to the template's database, is the
 * template's on that relation's other side, as Notion's: its row holds the
 * template there out of sight — its own relation shows empty and it leaves
 * the view (PAGE_NO_ROW_HOLDS) — and the template's value of the other side
 * lists it, so each page made from the template takes it, and the pair then
 * relates the row to that page (seen 2026-11-20 twice, two tabs and the API:
 * the template's "Related to …" showed the row in its editor, a page made
 * from it in the app and one by the API each listed the row, and the row's
 * relation listed both pages beside the hidden template). Here the template's
 * own value takes the row as its editor writes any value (FilterThisPage
 * `takeRow`); the row keeps none of it, so nothing shows the template in it.
 * A one-way relation has no other side: Notion's row holds the template
 * alone and no page made from it takes the row, so here it takes nothing.
 * Every row the view makes goes through these: a table's, board's, list's,
 * gallery's and timeline's, "새로 만들기"'s, a sub-item's.
 */
export function useTemplateBodyRows<Personal extends Makers>(
  thisPage: FilterThisPage | null, source: GraphDataSource, view: GraphView, properties: readonly GraphProperty[], personal: Personal,
): Personal {
  const latest = useRef({ thisPage, source, view, properties, personal });
  latest.current = { thisPage, source, view, properties, personal };
  const taking = Boolean(thisPage?.template && thisPage.takeRow);
  const { tableAdapter, boardAdapter, timelineAdapter } = personal;
  const makers = useMemo(() => {
    if (!taking) return null;
    // The template takes a row its view made in its own database, on each side the view's filters keep to it.
    const take = (database: GraphDataSource, row: GraphRow | null) => {
      const { thisPage: page, source: own, view: shown, properties: known, personal: queries } = latest.current;
      if (!row || !page?.takeRow || database.id !== own.id) return row;
      for (const side of templateSides(page, known, queries.effectiveView(shown))) page.takeRow(side, row.id);
      return row;
    };
    return {
      tableAdapter: tableAdapter && {
        ...tableAdapter,
        createRow: async (...args: Parameters<NativeTableProductAdapter["createRow"]>) => take(args[0], await tableAdapter.createRow(...args)),
      },
      boardAdapter: boardAdapter && {
        ...boardAdapter,
        createRow: async (...args: Parameters<NativeBoardProductAdapter["createRow"]>) => take(args[0], await boardAdapter.createRow(...args)),
      },
      timelineAdapter: timelineAdapter && {
        ...timelineAdapter,
        createDatedRow: async (...args: Parameters<NativeTimelineProductAdapter["createDatedRow"]>) =>
          take(args[0], await timelineAdapter.createDatedRow(...args)),
      },
    };
  }, [boardAdapter, tableAdapter, taking, timelineAdapter]);
  // The same writers but for the rows they make, which take the same arguments and give the same row.
  return useMemo(() => makers ? { ...personal, ...makers } as Personal : personal, [makers, personal]);
}

/**
 * The template's properties that take a row made in the view: the other side
 * of each two-way relation to the template's database whose filters keep the
 * view to "this page", as they give a new row its values (querySeeds.ts).
 */
export function templateSides(thisPage: FilterThisPage, properties: readonly GraphProperty[], view: GraphView): string[] {
  const seeds = graphViewFilterSeeds(properties, view, { currentPageId: thisPage.rowId }, nativeBoardQueryBounds);
  const sides: string[] = [];
  for (const property of properties) {
    const config = (property.config ?? {}) as { relationDatabaseId?: unknown; relatedPropertyId?: unknown; relationSymmetric?: unknown };
    const seeded = seeds[property.id];
    if (property.type !== "relation" || config.relationDatabaseId !== thisPage.dataSourceId) continue;
    if (!Array.isArray(seeded) || !seeded.includes(thisPage.rowId)) continue;
    // A relation of the template's own database that pairs with itself is its own other side.
    const side = typeof config.relatedPropertyId === "string" && config.relatedPropertyId
      ? config.relatedPropertyId : config.relationSymmetric === true ? property.id : "";
    if (side && !sides.includes(side)) sides.push(side);
  }
  return sides;
}
