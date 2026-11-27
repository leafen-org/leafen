import { startTransition, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { compareGraphOrder, type ContentGraph, type GraphRow } from "@/contentGraph";
import { readDatabaseRowPage, setDatabaseRowPageArchive } from "@/databaseRowPageApi";
import { afterNextPaint } from "@/lib/afterPress";
import { createPageGraphStore, useOpenPageGraphStore, type PageGraphStore } from "@/lib/pageGraphStore";
import { itemsWithout, replaceItems } from "@/lib/rowsReplaced";
import { sendRowTrash, type RowTrashPlace } from "@/lib/rowTrashKept";
import type { Session } from "@/types";

/** A graph a press's rows reach: the open page's, or one drawn beside it (useRowsShownBeside). */
type HeldGraph = Pick<PageGraphStore, "get" | "set" | "subscribe">;

/** The graphs drawn beside each open page's store, while they are drawn. */
const graphsBeside = new WeakMap<PageGraphStore, Set<HeldGraph>>();

function besideOf(store: PageGraphStore): HeldGraph[] {
  return [...graphsBeside.get(store) ?? []];
}

/** The open page's graph, then each drawn beside it (useRowsShownBeside); none outside the app. */
export function graphsDrawn(store: PageGraphStore | null): ContentGraph[] {
  return store ? [store.get(), ...besideOf(store).map((graph) => graph.get())] : [];
}

/**
 * A graph read on its own beside the open page — a sidebar database view
 * section's rows — shown with what every press holds on the open page's rows
 * (holdRowsShown), wherever the press was made: a row moved or sent to the
 * trash from the open page's table, a peek beside it, a favorite or another
 * section leaves the section at the press too, and comes back if the server
 * refuses (user decision 2026-11-12: a press answers on the next frame —
 * the section showed such a row until it read its rows again, at the next
 * change of address, or past it when that read came before the server's
 * answer). Each graph `read` gives is shown so; null while there is none.
 * Drawn in the press's own frame: a section's few rows, not the page's.
 */
export function useRowsShownBeside(read: ContentGraph | null): ContentGraph | null {
  const open = useOpenPageGraphStore();
  const [own] = useState(createPageGraphStore);
  // The read last given to the graph shown: one given since shows as read
  // until it is, in the same frame.
  const taken = useRef<ContentGraph | null>(null);
  useLayoutEffect(() => {
    if (!read) return;
    taken.current = read;
    own.set(read);
  }, [own, read]);
  useEffect(() => {
    if (!open) return undefined;
    const graphs = graphsBeside.get(open) ?? new Set<HeldGraph>();
    graphsBeside.set(open, graphs);
    graphs.add(own);
    return () => { graphs.delete(own); };
  }, [open, own]);
  const shown = useSyncExternalStore(own.subscribe, own.get, own.get);
  return read ? (taken.current === read ? shown : read) : null;
}

/**
 * Database rows a press sends to the trash or the archive, or brings back —
 * or a rename's name or icon (lib/renameShownAtPress.ts) — shown so on the
 * open page at once while the server is still to answer
 * (user decision 2026-11-12: a press answers on the next frame, the server
 * is reconciled last). `shown` makes the change on a graph, answering the
 * same graph when it holds nothing to change; every graph the page takes
 * meanwhile — read again, or changed from elsewhere — shows it too, until
 * the returned release. Released as refused, `undo` takes the change back
 * on the graph shown, even with the server out of reach; the page read
 * again then shows the server's own rows. Released as "kept" — the outbox
 * keeps the change for a server out of reach (rowTrashKept.ts) — the change
 * is shown where it isn't yet, its frame not come, and left so: what the
 * outbox keeps shows it from then on (rowTrashShown).
 *
 * The page draws the change behind the press's own frame (startTransition:
 * in pieces, a newer press first), so what the press closed — its menu —
 * paints at once: drawn in the press, a row leaving a page of 146 table
 * rows held the press's frame 140–160 ms (2026-11-13, NAS condition).
 * The store is the open page's anywhere in the app (useOpenPageGraphStore);
 * where none is, nothing is shown ahead. A graph drawn beside the page
 * (useRowsShownBeside) shows the change to its rows too, drawn at once.
 * `atOnce` draws it at once, not behind the frames that follow ("beside":
 * the graphs beside the page at once, the page once the press's frame is
 * painted — a rename's): a page's
 * blocks back at a ⌘Z (showBlocksBack), a row its side peek's "…" trashes
 * right after that press's frame, in which the peek starts sliding out
 * (useSidePeekMotion) — behind it, the row left once the slide was over,
 * 350 ms after the press (NAS condition, CPU ×2, 2026-11-13).
 */
export function holdRowsShown(store: PageGraphStore | null, shown: (graph: ContentGraph) => ContentGraph,
  undo: (graph: ContentGraph) => ContentGraph, atOnce: boolean | "beside" = false): (refused?: boolean | "kept") => void {
  if (!store) return () => {};
  let held = true;
  // The open page's graph takes the change whole; one drawn beside it its
  // rows and its databases' names and icons (a section's heading) alone.
  const graphs = [{ graph: store as HeldGraph, whole: true, changed: false },
    ...besideOf(store).map((graph) => ({ graph, whole: false, changed: false }))];
  const change = (target: (typeof graphs)[number], made: (graph: ContentGraph) => ContentGraph) => {
    const now = target.graph.get();
    const next = made(now);
    if (next === now || (!target.whole && next.rows === now.rows && next.dataSources === now.dataSources)) return false;
    target.graph.set(target.whole ? next : { ...now, rows: next.rows, dataSources: next.dataSources });
    return true;
  };
  const show = (target: (typeof graphs)[number]) => {
    if (held && change(target, shown)) target.changed = true;
  };
  const showAll = () => { for (const target of graphs) show(target); };
  if (atOnce === true) showAll();
  else if (atOnce === "beside") {
    // The graphs beside the page (a sidebar section's few rows) at once, the
    // page once the press's frame is painted (a later press first).
    for (const target of graphs) if (!target.whole) show(target);
    afterNextPaint(() => startTransition(showAll));
  } else startTransition(showAll);
  const stops = graphs.map((target) => target.graph.subscribe(() => show(target)));
  return (refused = false) => {
    if (!held) return;
    if (refused === "kept") showAll();
    held = false;
    for (const stop of stops) stop();
    // Beside the page, only where it was shown.
    if (refused === true) for (const target of graphs) if (target.whole || target.changed) change(target, undo);
  };
}

/**
 * The rows and every sub-item under them as the open page draws them, and
 * a graph beside it those it doesn't (a sidebar view section's, its
 * database not on the page); none outside the app.
 */
export function rowsDrawn(store: PageGraphStore | null, rowIds: readonly string[]): GraphRow[] {
  const found = new Map<string, GraphRow>();
  for (const { rows } of graphsDrawn(store)) {
    const ids = rowsWithSubitems(rows, rowIds);
    for (const row of rows) if (ids.has(row.id) && !found.has(row.id)) found.set(row.id, row);
  }
  return [...found.values()];
}

/**
 * Rows as the open page drew them shown gone from their databases — sent to
 * the trash or moved out — or, `back`, where they were drawn (out of the
 * trash), from the press until the returned release, as holdRowsShown;
 * released as refused, they are as drawn before. The one way a press made
 * anywhere in the app — a database's own menus, the sidebar, a favorite, a
 * peek beside the page — shows its rows gone from the open page's databases.
 */
export function holdRowsGone(store: PageGraphStore | null, drawn: readonly GraphRow[], back = false, atOnce = false) {
  const gone = withoutRowsWhereDrawn(drawn), there = withRowsAsDrawn(drawn);
  return holdRowsShown(store, back ? there : gone, back ? gone : there, atOnce);
}

/** The rows and every sub-item under them, which go to the trash together. */
export function rowsWithSubitems(rows: readonly GraphRow[], rootIds: readonly string[]): Set<string> {
  const ids = new Set(rootIds);
  for (let grew = true; grew;) {
    grew = false;
    for (const row of rows) {
      if (row.subitemParentId && ids.has(row.subitemParentId) && !ids.has(row.id)) {
        ids.add(row.id);
        grew = true;
      }
    }
  }
  return ids;
}

// The rows go and come as a splice of the rows drawn, and change as rows
// replaced in place (lib/rowsReplaced.ts): what the views work out of their
// rows — their pages, their cells' related pages — is worked out for those
// rows alone.
const graphOrder = (row: GraphRow, other: GraphRow) => compareGraphOrder(row, other) < 0;

/** The graph without the rows going to the trash. */
export function withoutRows(ids: ReadonlySet<string>) {
  return (graph: ContentGraph): ContentGraph => {
    const rows = itemsWithout(graph.rows, ({ id }) => ids.has(id));
    return rows === graph.rows ? graph : { ...graph, rows };
  };
}

/**
 * The graph without the rows where they were drawn, in their databases: one
 * read in another database since (moved there) stays.
 */
export function withoutRowsWhereDrawn(rows: readonly GraphRow[]) {
  const drawn = new Map(rows.map(({ id, dataSourceId }) => [id, dataSourceId]));
  return (graph: ContentGraph): ContentGraph => {
    const left = itemsWithout(graph.rows, (row) => drawn.get(row.id) === row.dataSourceId);
    return left === graph.rows ? graph : { ...graph, rows: left };
  };
}

/**
 * The graph with rows as they were drawn before a press: ones it no longer
 * holds back in their places, where it draws their databases, and their
 * archive marks as they were.
 */
export function withRowsAsDrawn(rows: readonly GraphRow[]) {
  const drawn = new Map(rows.map((row) => [row.id, row]));
  return (graph: ContentGraph): ContentGraph => {
    const held = new Set(graph.rows.map(({ id }) => id));
    const back = rows.filter(({ id, dataSourceId }) => !held.has(id) && graph.dataSources.some((source) => source.id === dataSourceId));
    const marked = new Map<number, GraphRow>();
    graph.rows.forEach((row, index) => {
      const was = drawn.get(row.id);
      if (was && (row.archivedAt ?? "") !== (was.archivedAt ?? "")) marked.set(index, { ...row, archivedAt: was.archivedAt, archivedBy: was.archivedBy });
    });
    if (!marked.size && !back.length) return graph;
    const kept = marked.size ? replaceItems(graph.rows, marked) : graph.rows;
    return { ...graph, rows: back.length ? itemsWithout(kept, () => false, back, graphOrder) : kept };
  };
}

/** The graph with the rows archived, by the person, or out of the archive: views show only rows not archived. */
export function withRowsArchived(ids: ReadonlySet<string>, archived: boolean, by = "") {
  const at = archived ? new Date().toISOString() : "";
  return (graph: ContentGraph): ContentGraph => {
    const changed = new Map<number, GraphRow>();
    graph.rows.forEach((row, index) => {
      if (ids.has(row.id) && Boolean(row.archivedAt) !== archived) changed.set(index, { ...row, archivedAt: at, archivedBy: archived ? by : "" });
    });
    return changed.size ? { ...graph, rows: replaceItems(graph.rows, changed) } : graph;
  };
}

/**
 * Writes a database row's page into or out of the archive, by its own
 * revision read first, or to the trash or back from it, kept for a server
 * out of reach ("kept", rowTrashKept.ts — `place` the page holding its
 * database and the rows drawn with it): each write on its own, not through a
 * row page service reading one row at a time, where a menu opened on another
 * row meanwhile dropped it (its read for the row was called off). A row
 * already where it is going (a step taken twice) is left.
 */
export async function writeRowPage(session: Session, rowId: string, change: "trash" | "restore" | "archive" | "unarchive",
  place?: RowTrashPlace): Promise<"sent" | "kept"> {
  if (change === "trash" || change === "restore") return sendRowTrash(session, [rowId], change === "trash", place);
  const { row } = await readDatabaseRowPage(session, rowId, { limit: 1 });
  await setDatabaseRowPageArchive(session, rowId, { archived: change === "archive", expectedAuthorityRevision: row.authorityRevision });
  return "sent";
}

/**
 * The page holding the database of rows a graph drew — the open page's
 * store holds the open page's and those drawn beside it — as their changes
 * go (a linked view's source's page), else `pageId`: where their trash is
 * kept (rowTrashKept.ts).
 */
export function rowsHolderPage(drawnIn: ContentGraph | PageGraphStore | null | undefined, rows: readonly Pick<GraphRow, "dataSourceId">[],
  pageId?: string | null) {
  const graphs = !drawnIn ? [] : "rows" in drawnIn ? [drawnIn] : graphsDrawn(drawnIn);
  const source = rows[0] && graphs.flatMap(({ dataSources }) => dataSources).find(({ id }) => id === rows[0]!.dataSourceId);
  return source?.pageId ?? pageId ?? null;
}
