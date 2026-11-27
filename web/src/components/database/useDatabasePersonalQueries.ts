import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { useProductToast } from "@/lib/productToast";
import type { ProductStatusSink } from "@/lib/productStatus";
import { refuseViewQueryGrowth } from "@/lib/viewQueryBound";
import { askingLinkedViewHere } from "@/lib/databaseStarterLinked";
import {
  sameViewQuery,
  splitViewChange,
  viewAsSaved,
  viewQuery,
  withViewQuery,
  type PersonalQuery,
} from "./databasePersonalQuery";
import { readPersonalQuery, writePersonalQuery } from "./databasePersonalQueryStore";
import { askRemoveSortAfterDrop, viewShowsSorted } from "./sortedViewDrop";
import { viewKeepsOwnRowOrder } from "./viewRowOrder";

// The writers' ways that save a view given them, besides updateView: a row
// moved in a view keeping its own order, properties saved with the view that
// shows them, a board's groups.
type ViewWriter = {
  updateView(view: GraphView, isCurrent: () => boolean, step?: boolean): Promise<void>;
  moveRows?(rows: readonly GraphRow[], view: GraphView | undefined, rowIds: readonly string[], isCurrent: () => boolean,
    values?: Readonly<Record<string, GraphRow["values"]>>, withDates?: boolean): Promise<void>;
  commitProperties?(properties: readonly GraphProperty[], isCurrent: () => boolean, view?: GraphView, from?: string): Promise<void>;
  moveRow?(rowId: string, change: { view?: GraphView }, isCurrent: () => boolean): Promise<void>;
  moveCards?(moves: readonly { rowId: string; change: { view?: GraphView } }[], isCurrent: () => boolean): Promise<void>;
  updateOptions?(propertyId: string, options: readonly unknown[], isCurrent: () => boolean, view?: GraphView): Promise<void>;
  rowMadeBeside?(view: GraphView): boolean;
};

/**
 * The filters and sorts a viewer applies to each view of a database, for
 * them alone, and the view writers that keep them so: a change to a view's
 * filters or sorts stays the viewer's own, and a change to its other
 * settings saves for everyone with the view's own filters and sorts. The
 * viewer can reset them, and an editor can save them for everyone. They are
 * kept in this browser for the viewer (`userId`) and view, so a reload keeps
 * them. Where `enabled` is false, every change saves for everyone.
 * Every view the database's views write goes through these writers, which
 * save it as the view is saved (viewAsSaved): never with the toolbar's
 * search, whatever way writes it.
 */
export function useDatabasePersonalQueries<Table extends ViewWriter, Board extends ViewWriter, Timeline extends ViewWriter>({
  views,
  enabled,
  userId,
  tableAdapter,
  boardAdapter,
  timelineAdapter,
  onStatus,
}: {
  views: readonly GraphView[];
  enabled: boolean;
  userId?: string;
  /** Where a refused change says why. */
  onStatus?: ProductStatusSink;
  tableAdapter?: Table;
  boardAdapter?: Board;
  timelineAdapter?: Timeline;
}) {
  const { t } = useTranslation("databaseView");
  const { notify } = useProductToast();
  const viewIds = views.map((view) => view.id).join(" ");
  const [queries, setQueries] = useState<Record<string, PersonalQuery>>(() => enabled ? keptQueries(userId, viewIds, {}) : {});
  const latest = useRef({ views, queries });
  latest.current = { views, queries };

  const setQuery = useCallback((viewId: string, query: PersonalQuery | null) => {
    if (enabled) writePersonalQuery(userId, viewId, query);
    setQueries((current) => {
      if (query) return { ...current, [viewId]: query };
      if (!(viewId in current)) return current;
      const next = { ...current };
      delete next[viewId];
      return next;
    });
  }, [enabled, userId]);

  // The viewer's kept filters and sorts apply again to views that come later;
  // where they aren't kept (a page the reader alone opens) none apply.
  useEffect(() => {
    if (enabled) setQueries((current) => keptQueries(userId, viewIds, current));
    else setQueries((current) => Object.keys(current).length ? {} : current);
  }, [enabled, userId, viewIds]);

  // Filters and sorts the view now saves are no longer the viewer's own,
  // unless an undone save gives them back before the view reads as it was.
  const restoring = useRef(new Set<string>());
  useEffect(() => {
    for (const [viewId, query] of Object.entries(latest.current.queries)) {
      const saved = views.find((view) => view.id === viewId);
      if (!saved || !sameViewQuery(query, viewQuery(saved.config))) restoring.current.delete(viewId);
      else if (!restoring.current.has(viewId)) setQuery(viewId, null);
    }
  }, [queries, setQuery, views]);

  const personalWriter = useCallback(<Writer extends ViewWriter>(writer?: Writer): Writer | undefined => {
    if (!writer) return writer;
    // The view as saved, whose filters and sorts a write keeps unless it
    // changes them: none where no filter or sort is the viewer's own, nor for
    // a view still being made, as a linked view's chosen copy, which takes
    // them for everyone (2026-11-02: a copy's sort came as the viewer's own,
    // with "모두에게 저장").
    const savedOf = (view: GraphView) => {
      const saved = latest.current.views.find((candidate) => candidate.id === view.id);
      return enabled && saved && !askingLinkedViewHere(saved) ? saved : undefined;
    };
    // A way that only places rows, or saves properties or groups with the
    // view, changes none of its filters or sorts.
    const asSaved = (view: GraphView | undefined) => view && viewAsSaved(view, savedOf(view));
    const wrapped: ViewWriter = {
      ...writer,
      async updateView(view: GraphView, isCurrent: () => boolean, step?: boolean) {
        const saved = savedOf(view);
        if (!saved) return writer.updateView(viewAsSaved(view), isCurrent, step);
        const own = latest.current.queries[view.id];
        const seen = own ? { ...saved, config: withViewQuery(saved.config, own) } : saved;
        if (onStatus && refuseViewQueryGrowth(seen.config, view.config, onStatus)) return;
        const change = splitViewChange(saved, seen, view);
        if (change.own !== undefined) setQuery(view.id, change.own);
        if (change.save) await writer.updateView(change.save, isCurrent, step);
      },
    };
    // Rows moved in a sorted view keeping its own order ask whether its sorts
    // go (sortedViewDrop.ts): "제거" takes them off for everyone as a step of
    // ⌘Z after the move's own, and the viewer's own sorts with them, which, as
    // a viewer's own sorts set from the toolbar, no step brings back. Notion's
    // saved sort came back at the first ⌘Z and the order at the second (seen
    // 2026-11-24 in two tabs); its viewer's own sort taken off stayed off, the
    // first ⌘Z putting the order back (seen once).
    const removeSorts = (placed: GraphView) => {
      const base = savedOf(placed) ?? latest.current.views.find(({ id }) => id === placed.id) ?? placed;
      if (viewShowsSorted(base)) void writer.updateView({ ...base, config: { ...base.config, rowOrder: placed.config.rowOrder, sorts: [] } }, () => true, true);
      const own = latest.current.queries[placed.id];
      if (!own || !Array.isArray(own.sorts) || !own.sorts.length) return;
      const left = { ...own, sorts: [] };
      setQuery(placed.id, sameViewQuery(left, { ...viewQuery(base.config), sorts: [] }) ? null : left);
    };
    if (writer.moveRows) wrapped.moveRows = (rows, view, rowIds, isCurrent, values, withDates) => {
      const placed = asSaved(view);
      const moved = writer.moveRows!(rows, placed, rowIds, isCurrent, values, withDates);
      if (placed && viewShowsSorted(view)) void askRemoveSortAfterDrop().then((remove) => { if (remove) removeSorts(placed); });
      return moved;
    };
    // A row a row's "+" made beside it in such a view asks the same, as Notion's
    // (sortedViewDrop.ts): `view`, the view as shown with the row placed there
    // in its own order; "제거" leaves it standing there. True when it asks.
    wrapped.rowMadeBeside = (view) => {
      const placed = asSaved(view);
      if (!placed || !viewShowsSorted(view) || !viewKeepsOwnRowOrder(view.config)) return false;
      void askRemoveSortAfterDrop(true).then((remove) => { if (remove) removeSorts(placed); });
      return true;
    };
    if (writer.commitProperties) wrapped.commitProperties = (properties, isCurrent, view, from) => writer.commitProperties!(properties, isCurrent, asSaved(view), from);
    if (writer.moveRow) wrapped.moveRow = (rowId, change, isCurrent) => writer.moveRow!(rowId, { ...change, view: asSaved(change.view) }, isCurrent);
    if (writer.moveCards) wrapped.moveCards = (moves, isCurrent) => writer.moveCards!(moves.map(({ rowId, change }) => ({ rowId, change: { ...change, view: asSaved(change.view) } })), isCurrent);
    if (writer.updateOptions) wrapped.updateOptions = (propertyId, options, isCurrent, view) => writer.updateOptions!(propertyId, options, isCurrent, asSaved(view));
    return wrapped as Writer;
  }, [enabled, onStatus, setQuery]);

  const personalTable = useMemo(() => personalWriter(tableAdapter), [personalWriter, tableAdapter]);
  const personalBoard = useMemo(() => personalWriter(boardAdapter), [personalWriter, boardAdapter]);
  const personalTimeline = useMemo(() => personalWriter(timelineAdapter), [personalWriter, timelineAdapter]);

  const effectiveView = useCallback((view: GraphView): GraphView => {
    const own = queries[view.id];
    return own ? { ...view, config: withViewQuery(view.config, own) } : view;
  }, [queries]);

  // "모두에게 필터가 저장되었습니다. · 실행 취소", as Notion's (seen
  // 2026-11-03, for sorts too); undoing saves the view as it was and gives
  // the viewer their own filters and sorts back.
  const saveForEveryone = useCallback((viewId: string) => {
    const writer = tableAdapter ?? boardAdapter ?? timelineAdapter;
    const saved = latest.current.views.find((view) => view.id === viewId);
    const own = latest.current.queries[viewId];
    if (!writer || !saved || !own) return;
    void writer.updateView({ ...saved, config: withViewQuery(saved.config, own) }, () => true).then(() => {
      notify(t("toast.savedFiltersForEveryone"), "success", { label: t("undo"), onClick: async () => {
        await writer.updateView(saved, () => true);
        restoring.current.add(viewId);
        setQuery(viewId, own);
      } });
    });
  }, [boardAdapter, notify, setQuery, t, tableAdapter, timelineAdapter]);

  return {
    /** The view as the viewer sees it, with their own filters and sorts. */
    effectiveView,
    /** Whether the viewer applies filters or sorts of their own to the view. */
    hasOwnQuery: (viewId: string) => viewId in queries,
    /** Applies filters and sorts for the viewer alone, as someone who can't edit the view sets them. */
    setOwnQuery: (view: GraphView) => {
      const saved = latest.current.views.find((candidate) => candidate.id === view.id);
      const query = viewQuery(view.config);
      setQuery(view.id, saved && sameViewQuery(query, viewQuery(saved.config)) ? null : query);
    },
    resetOwnQuery: (viewId: string) => setQuery(viewId, null),
    saveForEveryone,
    tableAdapter: personalTable,
    boardAdapter: personalBoard,
    timelineAdapter: personalTimeline,
  };
}

/** The filters and sorts the viewer keeps here for views not yet in `current`, added to it. */
function keptQueries(userId: string | undefined, viewIds: string, current: Record<string, PersonalQuery>) {
  let next = current;
  for (const viewId of viewIds.split(" ")) {
    const stored = viewId && !(viewId in current) ? readPersonalQuery(userId, viewId) : undefined;
    if (!stored) continue;
    if (next === current) next = { ...current };
    next[viewId] = stored;
  }
  return next;
}
