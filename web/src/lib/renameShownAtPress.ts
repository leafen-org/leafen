import { useCallback, useEffect, useSyncExternalStore } from "react";
import { withDataSourceChange, type ContentGraph, type GraphBlock, type GraphOperation, type GraphRow } from "@/contentGraph";
import type { GraphDataSourceIconType } from "@/contentGraphWire.generated";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { replaceItems } from "@/lib/rowsReplaced";
import { graphsDrawn, holdRowsShown } from "@/lib/rowsShownAtPress";
import { sameTitle } from "@/lib/titleMerge";
import type { TextSpan } from "@/lib/types";

/*
 * What a rename popover (PageRenamePopover) changes — a name as each key
 * types it, an icon as it is picked — shown at once wherever it is drawn,
 * before the server answers, as Notion's draws it (seen 2026-11-18: the
 * sidebar row, the popover and the open page's inline title in the press's
 * own frame, the name following each key), and kept there until the server
 * answers; refused, it goes back where it showed (user decision 2026-11-12:
 * a press answers on the next frame, the server is reconciled last). Saved,
 * what showed stays until what reads it is read again with the server's
 * own. The one way every rename beside the page shows its change: the open
 * page's graph and the graphs drawn beside it take it (holdRowsShown), and
 * a sidebar database row, which names its database as the workspace's list
 * of them does, takes it here (useShownDatabaseHead).
 */

/** A row's icon given through its page (setDatabaseRowPagePresentation), which no graph change carries. */
type RowIconShown = { kind: "rowIcon"; rowId: string; icon: string; iconType: GraphRow["iconType"] };

/** A change a rename shows: the graph change its write sends, or a row's icon. */
export type ShownChange = GraphOperation | RowIconShown;

function replaceById<T extends { id: string }>(list: readonly T[], id: string, change: (item: T) => T): readonly T[] {
  const index = list.findIndex((item) => item.id === id);
  if (index < 0) return list;
  const next = change(list[index]);
  return next === list[index] ? list : replaceItems(list, new Map([[index, next]]));
}

/**
 * The graph with the changes made to what it holds: a database's name or
 * icon, a database block's own name, a row's values or icon. What it doesn't
 * hold stays out of it — a change shown on another page open is no part of it.
 */
function withChangesShown(changes: readonly ShownChange[]) {
  return (graph: ContentGraph): ContentGraph => {
    let next = graph;
    for (const change of changes) {
      if (change.kind === "upsertDataSource") {
        const dataSources = replaceById(next.dataSources, change.dataSource.id, (source) => withDataSourceChange(source, change.dataSource));
        if (dataSources !== next.dataSources) next = { ...next, dataSources: dataSources as ContentGraph["dataSources"] };
      } else if (change.kind === "upsertBlock") {
        const blocks = replaceById(next.blocks, change.block.id, (block): GraphBlock => ({ ...block, content: change.block.content }));
        if (blocks !== next.blocks) next = { ...next, blocks: blocks as ContentGraph["blocks"] };
      } else if (change.kind === "upsertRow") {
        const rows = replaceById(next.rows, change.row.id, (row) => ({ ...row, values: { ...row.values, ...change.row.values } }));
        if (rows !== next.rows) next = { ...next, rows: rows as ContentGraph["rows"] };
      } else if (change.kind === "rowIcon") {
        const rows = replaceById(next.rows, change.rowId, (row) => row.icon === change.icon && row.iconType === change.iconType
          ? row : { ...row, icon: change.icon, iconType: change.iconType });
        if (rows !== next.rows) next = { ...next, rows: rows as ContentGraph["rows"] };
      }
    }
    return next;
  };
}

/**
 * A row's name given as its page's title (a favorite row page renamed), as
 * the open page and the graphs beside it hold it, and the row as they draw
 * it now, which a refusal puts back: none where nothing draws the row.
 */
export function rowNameShown(store: PageGraphStore | null, rowId: string, title: string, spans?: readonly TextSpan[]) {
  for (const graph of graphsDrawn(store)) {
    const row = graph.rows.find(({ id }) => id === rowId);
    const property = row && graph.properties.find((candidate) => candidate.dataSourceId === row.dataSourceId && candidate.type === "title");
    if (!row || !property) continue;
    const named: ShownChange = { kind: "upsertRow", row: { ...row, values: { [property.id]: { type: "title", value: title, ...(spans ? { spans: [...spans] } : {}) } } } };
    return { changes: [named], settled: [{ kind: "upsertRow", row } satisfies ShownChange] };
  }
  return { changes: [] as ShownChange[], settled: [] as ShownChange[] };
}

type Release = (refused?: boolean) => void;

/**
 * One rename's showing, from its popover opening to its last save answered:
 * `show` draws a change in place of the one shown before it — in the
 * press's frame where the sidebar draws it (a section's rows; a database
 * row's name and icon, useShownDatabaseHead), on the open page once that
 * frame is painted, a newer press first: drawn in the press, the page's
 * redraw held an icon pick's frame 16–48 ms more (NAS condition, CPU ×2,
 * 2026-11-18: 200–232 ms against 176–184 before). `saving` keeps the change
 * shown with the write that saves it — released once it lands, taken back
 * if refused to `settled` (the content as the server had it, read at the
 * first change shown) with the changes saved since; `end` lets go of a
 * change shown and never saved (typed back to what was saved).
 */
export function renameShowing(store: PageGraphStore | null, settled: () => readonly ShownChange[]) {
  let showing: Release | null = null;
  // Read at the first change shown: a database's page is read as its rename opens.
  let base: readonly ShownChange[] | null = null;
  const hold = (changes: readonly ShownChange[]) => holdRowsShown(store, withChangesShown(changes), withChangesShown(base ??= settled()), "beside");
  return {
    show(changes: readonly ShownChange[]) {
      const was = showing;
      showing = hold(changes);
      was?.();
    },
    saving(changes: readonly ShownChange[], saved: Promise<unknown>) {
      // The last change shown is the one saved: the popover saves its words as last typed.
      const held = showing ?? hold(changes);
      showing = null;
      void saved.then(() => {
        base = [...(base ?? settled()), ...changes];
        held();
      }, () => held(true));
    },
    end() {
      showing?.();
      showing = null;
    },
  };
}

/** A database's name and icon as its sidebar rows draw them. */
export type DatabaseHead = { title: string; titleSpans?: readonly TextSpan[]; icon: string; iconType: GraphDataSourceIconType };

type HeldHead = { head: DatabaseHead; over: DatabaseHead; answered: boolean };

const heads = new Map<string, HeldHead>();
const listeners = new Map<string, Set<() => void>>();

function tell(id: string) {
  for (const listener of [...listeners.get(id) ?? []]) listener();
}

function sameDatabaseHead(a: DatabaseHead, b: DatabaseHead) {
  return a.icon === b.icon && a.iconType === b.iconType
    && sameTitle({ title: a.title, spans: a.titleSpans as TextSpan[] | undefined }, { title: b.title, spans: b.titleSpans as TextSpan[] | undefined });
}

/**
 * Shows `change` on the database `id` (its block) wherever a sidebar row
 * names it, over `over` — its name and icon as the workspace's list holds
 * them now — while the list still holds that (as a typed page title stands
 * over the list's, lib/pageNames.ts): once the list is read with the
 * server's own, it is drawn again. Answers how the save went: landed, it
 * stays until the list moves on; refused, it goes at once.
 */
export function showDatabaseHead(id: string, change: Partial<DatabaseHead>, over: DatabaseHead) {
  const was = heads.get(id);
  const base = was && sameDatabaseHead(was.over, over) ? was.head : over;
  const entry: HeldHead = { head: { ...base, ...change }, over, answered: false };
  // Back to what the list holds, nothing is shown over it.
  if (sameDatabaseHead(entry.head, over)) {
    if (heads.delete(id)) tell(id);
    return (_saved?: Promise<unknown>) => undefined;
  }
  heads.set(id, entry);
  tell(id);
  return (saved?: Promise<unknown>) => {
    void saved?.then(() => {
      if (heads.get(id) === entry) entry.answered = true;
    }, () => {
      if (heads.get(id) !== entry) return;
      heads.delete(id);
      tell(id);
    });
  };
}

function subscribe(id: string, listener: () => void) {
  let set = listeners.get(id);
  if (!set) listeners.set(id, set = new Set());
  set.add(listener);
  return () => {
    set.delete(listener);
    if (!set.size) listeners.delete(id);
  };
}

/**
 * The database's name and icon as a sidebar row draws them: one a rename
 * shows (showDatabaseHead) while the list still holds what it showed over,
 * else the list's `listed`. A change shown and saved is let go of once the
 * list moves on: the list's is the database's own again, a change made
 * elsewhere after it too.
 */
export function useShownDatabaseHead(id: string, listed: DatabaseHead): DatabaseHead {
  const follow = useCallback((listener: () => void) => subscribe(id, listener), [id]);
  const held = useSyncExternalStore(follow, () => heads.get(id));
  const standing = held && sameDatabaseHead(held.over, listed);
  useEffect(() => {
    if (held && held.answered && (!standing || sameDatabaseHead(held.head, listed)) && heads.get(id) === held) heads.delete(id);
  }, [held, id, listed, standing]);
  return standing ? held.head : listed;
}
