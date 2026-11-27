import type { Dispatch, SetStateAction } from "react";
import type { ContentGraph, GraphRow, GraphRowValue } from "@/contentGraph";
import { movePageIntoDatabase } from "@/databaseRowPageApi";
import { graphPositionBetween } from "@/lib/graphPosition";
import { newId, positionBetween } from "@/lib/ids";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { holdPageThroughMove } from "@/lib/pageBecomingRow";
import { holdRowsShown, withoutRows } from "@/lib/rowsShownAtPress";
import type { Bootstrap, Page, Session } from "@/types";
import { pageMapOf } from "@/lib/pageIndex";

/**
 * Pages moved into a database, shown there at the press (user decision
 * 2026-11-12: a press answers on the next frame — the move popover stayed,
 * and the pages in the sidebar, until the server had moved each one, a
 * full-page database's source read first): they leave the sidebar with the
 * pages under them, and where the open page draws the database they stand
 * as its last rows, by their titles, as the server makes them
 * (page_row_conversion.go), held over every graph the page reads until the
 * server moved them. `source` is the database's data source, known at the
 * press or read behind it. The server moves the pages one after another;
 * one refused is back in the sidebar with those after it and gone from the
 * database, and the promise is refused with its reason. The open one stays
 * drawn till then, and opens as its row's page (pageBecomingRow.ts).
 */
export async function movePagesIntoDatabaseShown(input: {
  session: Session;
  pageIds: readonly string[];
  /** The pages listed, which the moving ones and the pages under them are taken from. */
  pages: readonly Page[];
  source: string | Promise<string | undefined>;
  store: PageGraphStore | null;
  setData?: Dispatch<SetStateAction<Bootstrap>>;
}) {
  const { session, pageIds, pages, store, setData } = input;
  const leaving = withPagesUnder(pages, pageIds);
  const turned = pageIds.map((id) => holdPageThroughMove(session, [id]));
  setData?.((current) => ({ ...current, pages: current.pages.filter(({ id }) => !leaving.has(id)) }));
  const back = (from: number) => {
    const coming = withPagesUnder(pages, pageIds.slice(from));
    setData?.((current) => ({
      ...current, pages: [...current.pages.filter(({ id }) => !coming.has(id)), ...pages.filter(({ id }) => coming.has(id))],
    }));
    for (const release of turned.slice(from)) release(true);
  };
  const byId = pageMapOf(pages);
  let holds: ((refused?: boolean) => void)[] = [];
  let moved = 0;
  try {
    const dataSourceId = await input.source;
    if (!dataSourceId) throw new Error("The database's source was not found.");
    holds = pageIds.map((id) => {
      const page = byId.get(id);
      return page ? holdRowsShown(store, withPageAsRow(page, dataSourceId), withoutRows(new Set([id]))) : () => undefined;
    });
    for (; moved < pageIds.length; moved += 1) {
      await movePageIntoDatabase(session, pageIds[moved], { requestId: newId(), dataSourceId });
      holds[moved]?.();
      turned[moved]?.();
    }
  } catch (error) {
    for (const release of holds.slice(moved)) release(true);
    back(moved);
    throw error;
  }
}

/**
 * A database's page moved out as a page, shown in the sidebar at the press
 * (user decision 2026-11-12: a press answers on the next frame — a table
 * row moved to "개인 페이지" showed there once the server answered, 0.6–0.8 s
 * on a NAS) where the server puts it (page_row_conversion.go): last under
 * the page it goes under, in that page's place, or at the top, last in the
 * place chosen — a teamspace, or private pages — or, none chosen, in its
 * database's place, edited now. Where
 * that place isn't listed here, the server's list shows it. Released as
 * refused, the list is as it was before; the server's own list replaces it
 * once read.
 */
export function showRowAsPage(input: {
  page: Page; parentId: string; pagesById: Readonly<Record<string, Page>>; setData?: Dispatch<SetStateAction<Bootstrap>>;
  /** The teamspace, or "" the private pages, at whose top it goes; none, the place its database is in. */
  teamspaceId?: string;
}): (refused?: boolean) => void {
  const { page, parentId, pagesById, setData, teamspaceId } = input;
  const parent = parentId ? pagesById[parentId] : undefined;
  const place = parentId ? parent : teamspaceId !== undefined ? { teamspaceId } : pagesById[page.parentId ?? ""];
  if (!setData || !place) return () => undefined;
  const last = Object.values(pagesById).reduce((max, other) => (other.id !== page.id && !other.inTrash
    && (other.parentId || "") === parentId && other.position > max ? other.position : max), Number.NEGATIVE_INFINITY);
  const { archivedAt: _archived, archivedBy: _by, wikiRootId: _wiki, ...kept } = page;
  const wikiRootId = parent?.wikiRootId || (parent?.isWiki ? parent.id : "");
  const shown: Page = {
    ...kept, kind: "page", parentId, teamspaceId: place.teamspaceId ?? "", ...(wikiRootId ? { wikiRootId } : {}), inTrash: false,
    position: positionBetween(Number.isFinite(last) ? last : undefined, undefined), updatedAt: new Date().toISOString(),
  };
  let before: Page | undefined;
  setData((current) => {
    before = current.pages.find(({ id }) => id === page.id);
    return { ...current, pages: [...current.pages.filter(({ id }) => id !== page.id), shown] };
  });
  return (refused = false) => {
    if (!refused) return;
    const was = before;
    setData((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => id !== page.id), ...(was ? [was] : [])] }));
  };
}

/** The pages and every page under them. */
export function withPagesUnder(pages: readonly Page[], pageIds: readonly string[]) {
  const ids = new Set(pageIds);
  for (let grew = true; grew;) {
    grew = false;
    for (const page of pages) {
      if (page.parentId && ids.has(page.parentId) && !ids.has(page.id)) {
        ids.add(page.id);
        grew = true;
      }
    }
  }
  return ids;
}

/**
 * The graph with the page as the data source's last row — or, `first`, its
 * first, as the server puts a block turned into a page there
 * (page_row_conversion.go) — where the graph draws that source; as it is
 * otherwise.
 */
export function withPageAsRow(page: Pick<Page, "id" | "title"> & Partial<Page>, dataSourceId: string, first = false) {
  return (graph: ContentGraph): ContentGraph => {
    const title = graph.properties.find((property) => property.dataSourceId === dataSourceId && property.type === "title");
    if (!title || graph.rows.some(({ id }) => id === page.id)) return graph;
    const positions = graph.rows.flatMap((row) => row.dataSourceId === dataSourceId ? [row.position] : []).sort();
    const at = new Date().toISOString();
    const value: GraphRowValue = { type: "title", value: page.title, ...(page.titleSpans?.length ? { spans: page.titleSpans as GraphRowValue["spans"] } : {}) };
    const row: GraphRow = {
      id: page.id, dataSourceId, subitemParentId: null,
      position: first ? graphPositionBetween(undefined, positions[0]) : graphPositionBetween(positions.at(-1), undefined),
      icon: page.icon, iconType: page.iconType as GraphRow["iconType"], cover: page.cover, coverPosition: page.coverPosition,
      // The server makes the row as the page goes in, newest of the source's
      // rows: a view showing its newest first puts it first (viewRowOrder.ts).
      createdAt: at, createdBy: page.createdBy, updatedAt: at, lastEditedBy: page.lastEditedBy,
      hasContent: page.hasContent, values: { [title.id]: value },
    };
    return { ...graph, rows: [...graph.rows, row] };
  };
}

/**
 * The data source of a full-page database's page as the open page holds it
 * (its own database at the page's root), or "" where the open page doesn't
 * draw it — read then (fullPageDatabaseSource).
 */
export function databasePageSourceShown(store: PageGraphStore | null, pageId: string) {
  const graph = store?.get();
  const container = graph?.databaseContainers.find((item) => item.pageId === pageId && item.kind === "source" && !item.parentBlockId);
  const id = container && graph?.blocks.find((block) => block.id === container.blockId)?.content.dataSourceId;
  return typeof id === "string" ? id : "";
}
