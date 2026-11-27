"use client";

import { useCommittedOperationScope } from "../product/useCommittedOperationScope";

import { useCallback, useContext, useEffect, useMemo, useRef } from "react";
import { pageHref } from "@/lib/navigation";
import { useStateUnlessSame } from "@/lib/useStateUnlessSame";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { pushRoute, ROUTE_CHANGE_EVENT } from "@/lib/router";
import { leaveSidePeek } from "@/lib/sidePeekMotion";
import { addressWithView, INLINE_DATABASE_PEEK_PARAM, peekedPageId, peekKeepsView } from "@/lib/pagePeek";
import { addressViewLeftByPeek, noteDatabaseViewAdded } from "@/lib/databaseViewChoice";
import { databaseStartRefused } from "@/lib/databaseStartRefusal";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { DatabaseNamesAddressContext, useDatabaseOwnPage } from "./databaseFullPageLink";
import { SuggestingModeContext } from "@/components/editor/suggestingMode";
import { useTemplateWindow } from "./templateWindow";
import { TEMPLATE_PAGE_PARAM } from "./useTemplateAddress";
import {
  announceRowSelection, OpenDatabasePagesContext, RowsOpenInContext, subscribeRowOpeners, useRowOpener,
} from "./databaseRowOpeners";
import {
  databaseViewUrlState,
  databaseViewUrlWithSelection,
  type DatabaseViewRowMode,
} from "./databaseViewModel";
import type {
  DatabaseViewOperationContext,
  DatabaseViewOperations,
  DatabaseViewSnapshot,
} from "./DatabaseView";

function currentUrl() {
  return new URL(window.location.href);
}

function publishUrl(url: URL, replace = false) {
  const href = `${url.pathname}${url.search}${url.hash}`;
  window.history[replace ? "replaceState" : "pushState"](window.history.state, "", href);
}

function operationSurface(operations: DatabaseViewOperations): DatabaseViewOperations {
  return Object.freeze({
    isCurrent: operations.isCurrent,
    selectView: operations.selectView,
    createView: operations.createView,
    updateViews: operations.updateViews,
    deleteView: operations.deleteView,
    restoreView: operations.restoreView,
    openRow: operations.openRow,
    onError: operations.onError,
  });
}

/**
 * An opened row names the view that opened it in the URL where the database
 * is shown as a page (openRow), and one view opens the row the URL names:
 * that view, else the one that opened it here, else the first on the page
 * holding the row. With a `rowScope`, the mount opens rows only for that view, and a row
 * the URL names for another view stays closed here. A dashboard `widget`'s
 * scope is its dashboard: the address names the dashboard, never the widget,
 * whatever the widget does, so the address reopens the dashboard (Notion's
 * `?v=` names the dashboard); a row a widget opens names it too, and a
 * dashboard opens none itself. With a `publicHref`, a published page's
 * database opens a row in full, or in a new tab, as the row's public page.
 * A view that `opensRowsItself` never opens the row the URL names, nor offers
 * to: what its own way opens it with draws it — My tasks' and Home's tasks
 * the page's peek (AuthenticatedPagePeek), People's rows a person's profile —
 * so one address draws one peek, not the view's read-only one beneath it.
 */
export function useDatabaseViewController(
  snapshot: DatabaseViewSnapshot,
  adapter: NativeDatabaseViewProductAdapter,
  onError: (error: unknown) => void,
  rowScope?: string,
  publicHref?: (pageId: string) => string,
  widget = false,
  opensRowsItself = false,
) {
  const currentSnapshot = useRef(snapshot);
  currentSnapshot.current = snapshot;
  const lifecycle = useCommittedOperationScope([snapshot.contextIdentity]);
  const [rowSelection, setRowSelection] = useStateUnlessSame<{
    rowId: string;
    mode: DatabaseViewRowMode;
  } | null>(null);
  const opener = useRowOpener();
  const openPages = useContext(OpenDatabasePagesContext);
  // A touch screen opens a row as its own page, as Notion's phone does (seen
  // 2026-10-29: there is no room beside or around a preview there) and its
  // iPad (seen 2026-11-12 in iPad Safari 834: a row's "열기" opens its whole
  // page, "←" back to the database).
  const { touch } = useSidebarLayout(true);
  // The place showing the view may open its rows its own way (RowsOpenInContext).
  const placeOpensIn = useContext(RowsOpenInContext);
  // A view in a template's body opens its rows in the template window's place (templateWindow.ts).
  const templateWindow = useTemplateWindow();
  // Whether this view opens the address's row, or leaves it to its own way of opening rows.
  const addressRowHere = !opensRowsItself;
  const shownMode = useCallback((mode: DatabaseViewRowMode): DatabaseViewRowMode => (touch ? "full" : placeOpensIn ?? mode), [placeOpensIn, touch]);
  // A database's own page names its chosen view in its address, as Notion's;
  // an inline database's choice leaves the page's address alone, and so does
  // the home's pinned database, whose rows open there without a view
  // (DatabaseNamesAddressContext).
  const namesAddress = useContext(DatabaseNamesAddressContext);
  const pageNamesView = useDatabaseOwnPage() && namesAddress;
  const ownPage = pageNamesView && !widget;
  // A database's own page opened in a peek over another page names its view
  // there too as it opens, beside the peek's (`?v=<view>&p=<its page>&pm=s`),
  // as Notion's (seen 2026-11-14: a full-page database ⌥-pressed open; its
  // `v` stays once it closes); the view it shows there is the person's choice
  // all the same (chosenDatabaseView). One opened alone from a page — by its
  // own ID (inlineDatabasePeekHref), or an older address's `pdb` — leaves the
  // address as it was.
  const holderPageId = snapshot.holderPageId;
  const peekedHere = useCallback((url: URL) => ownPage && Boolean(holderPageId)
    && peekedPageId(url.searchParams.get("p")) === holderPageId && !url.searchParams.has(INLINE_DATABASE_PEEK_PARAM), [holderPageId, ownPage]);
  // Shown in a side or center peek, a full-page one or one opened alone (SuggestingMode `inPeek`).
  const inPeek = useContext(SuggestingModeContext).inPeek === true;

  const isCurrent = useCallback((context: DatabaseViewOperationContext) => {
    const current = currentSnapshot.current;
    return !context.signal.aborted
      && context.contextIdentity === current.contextIdentity
      && context.snapshotIdentity === current.snapshotIdentity
      && context.requestGeneration === current.requestGeneration
      && context.dataSourceId === current.dataSource.id
      && context.viewId === current.activeView.id;
  }, []);

  // The view an address names that this mount last opened (`?v=`), or the one
  // last chosen here, whose address the history keeps.
  const linkedViewApplied = useRef<string | null>(null);
  // The row this view opened last, which it opens again while the address,
  // naming no view (a database in a page's), names that row: of two views
  // showing it, the one pressed.
  const opened = useRef<string | null>(null);

  const operations = useMemo<DatabaseViewOperations>(() => ({
    isCurrent,
    // A view chosen on a database's own page is a step in the history, its
    // address naming it, as Notion's (seen twice 2026-11-25 in Chrome, two
    // tabs: each view tab pressed one more entry, back and forward going
    // through the views chosen, the window's title with them). The address
    // moves first, so the choice is the person's at it (databaseViewChoice.ts)
    // and back to the address before shows its view again. One in a peek over
    // another page leaves the address as the peek opened it, the history too,
    // as Notion's (seen twice that day: a side peek's tab press kept `?v=` and
    // the history's length; back and forward to it show the view last chosen),
    // and an inline database's leaves the page's own.
    selectView(view, context) {
      if (!isCurrent(context)) return;
      if (ownPage && typeof window !== "undefined" && !inPeek && !peekedHere(currentUrl())) {
        const next = databaseViewUrlWithSelection(currentUrl(), { viewId: view.id });
        if (next.href !== window.location.href) publishUrl(next);
        linkedViewApplied.current = view.id;
      }
      adapter.selectView(snapshot.block, view);
    },
    async createView(type, context, shown) {
      if (!isCurrent(context)) return null;
      // Its ⌘Z shows the view shown now again (noteDatabaseViewAdded).
      const blockId = snapshot.block.id, fromId = snapshot.activeView.id;
      return adapter.createView(snapshot.block, snapshot.dataSource, type, () => isCurrent(context), snapshot.activeView, undefined, (made) => {
        noteDatabaseViewAdded(blockId, made.id, fromId);
        shown?.(made);
      });
    },
    updateViews(views, context, step) {
      return adapter.updateViews(views, () => isCurrent(context), step);
    },
    // The view shown going, the first left shows, its address in the page's
    // too, as Notion's (seen 2026-11-05: deleting the shown view opens
    // "Default view"); a stale address would reopen the gone view's fallback.
    async deleteView(view, context) {
      await adapter.deleteView(view, () => isCurrent(context));
      if (!isCurrent(context) || snapshot.activeView.id !== view.id) return;
      const next = snapshot.views.find(({ id }) => id !== view.id);
      if (!next) return;
      adapter.selectView(snapshot.block, next);
      if (ownPage && typeof window !== "undefined") {
        publishUrl(databaseViewUrlWithSelection(currentUrl(), { viewId: next.id }), true);
      }
    },
    restoreView(view, context) {
      return adapter.restoreView(view, () => isCurrent(context));
    },
    openRow(rowId, requested, context, fromMenu = false) {
      if (!isCurrent(context) || typeof window === "undefined") return;
      const mode = requested === "new_tab" ? requested : shownMode(requested);
      if (mode === "new_tab") {
        window.open(publicHref ? publicHref(rowId) : pageHref(rowId), "_blank", "noopener,noreferrer");
        return;
      }
      // A row opened as a full page goes to its own address, as Notion's (seen
      // 2026-11-04); back returns to the database.
      if (mode === "full") {
        pushRoute(publicHref ? publicHref(rowId) : pageHref(rowId));
        return;
      }
      // One a template's body opens takes its window's place, as Notion's (templateWindow.ts): the address names the row
      // instead of the template in one step, so back opens the template again, and the view on the page holding the row —
      // else the page's peek (AuthenticatedPagePeek) — opens it as the address moves, the template's window leaving.
      if (templateWindow) {
        const next = databaseViewUrlWithSelection(currentUrl(), { rowId, rowMode: mode, openAbove: [] });
        next.searchParams.delete(TEMPLATE_PAGE_PARAM);
        pushRoute(`${next.pathname}${next.search}${next.hash}`);
        templateWindow.leave();
        return;
      }
      // A row opened from inside an opened row keeps that row, and those above
      // it, open beneath it.
      const state = databaseViewUrlState(currentUrl());
      const openAbove = rowId === state.rowId ? state.openAbove
        : state.rowId && openPages.includes(state.rowId)
          ? [...state.openAbove, { rowId: state.rowId, rowMode: state.rowMode, viewId: state.viewId ?? "" }]
          : [];
      // The address names the view the row opened from where the database is
      // shown as a page — its own, a wiki's, one alone in a peek — and none,
      // dropping one named there, for a database in a page, as Notion's (seen
      // 2026-11-25 in Chrome, two tabs: an inline database's row "열기" gave
      // `?p=<row>&pm=s` under the page's address and under a `?v=` a closed
      // peek left; in that database's side peek `?v=<its view>&p=<row>&pm=s`;
      // on a full-page database `?v=<view>&p=<row>&pm=s`). Its menu's "다음에서
      // 열기 › 사이드 보기" opens the row as a page is (`fromMenu`): the view
      // stays only as the address's own (peekKeepsView) — in a database's
      // peek it gave `?p=<row>&pm=s`. A dashboard's widget names its dashboard.
      const keepsView = pageNamesView && (!fromMenu || peekKeepsView(currentUrl(), rowId));
      opened.current = rowId;
      publishUrl(databaseViewUrlWithSelection(currentUrl(), {
        rowId, rowMode: mode, viewId: namesAddress ? rowScope ?? (keepsView ? snapshot.activeView.id : null) : null, openAbove,
      }));
      opener.offer({ rowId, preferred: true });
      setRowSelection({ rowId, mode });
      announceRowSelection();
    },
    onError(error, context) {
      if (isCurrent(context)) onError(error);
    },
  }), [adapter, inPeek, isCurrent, namesAddress, onError, opener, openPages, ownPage, pageNamesView, peekedHere, publicHref, rowScope, shownMode, snapshot.activeView.id, snapshot.block, snapshot.dataSource, templateWindow]);

  // A database page's address names the view it shows, as Notion's does (seen
  // 2026-11-03): from the start, and whenever the address comes back to the
  // page without one while the view stays — a board group preview's "열기",
  // the sidebar or a link to the page it is on — so a copied address reopens
  // that view. One in a page leaves the page's own, and one whose page's
  // start was refused, drawn till the page is read again, names none
  // (databaseStartRefused).
  useEffect(() => {
    if (!ownPage || typeof window === "undefined") return;
    const path = window.location.pathname;
    function nameView() {
      if (window.location.pathname !== path || (holderPageId && databaseStartRefused(holderPageId))) return;
      const url = databaseViewUrlState(currentUrl());
      // In a peek the address's view is the peek's: one another database
      // left there (the page behind, a peek closed before) is named over.
      const peeked = peekedHere(currentUrl());
      if (peeked ? url.viewId && (url.viewId === snapshot.activeView.id || snapshot.views.some(({ id }) => id === url.viewId))
        : url.viewId || url.rowId) return;
      publishUrl(peeked ? addressWithView(currentUrl(), snapshot.activeView.id)
        : databaseViewUrlWithSelection(currentUrl(), { viewId: snapshot.activeView.id }), true);
    }
    nameView();
    window.addEventListener(ROUTE_CHANGE_EVENT, nameView);
    window.addEventListener("popstate", nameView);
    return () => {
      window.removeEventListener(ROUTE_CHANGE_EVENT, nameView);
      window.removeEventListener("popstate", nameView);
    };
  }, [holderPageId, ownPage, peekedHere, snapshot.activeView.id, snapshot.views]);

  // The view an address names (`?v=`, as "copy view link" makes it) opens
  // when the page opens or the address turns to it, as Notion's does; the
  // view the person picks afterwards stays theirs, so the same address does
  // not pull it back; going back or forward to another view's address shows
  // that view. In a peek the person's choice shows, whatever the peek's
  // address names, and a peek closed leaves its view to no one (chosenDatabaseView).
  useEffect(() => {
    if (typeof window === "undefined") return;
    function synchronize() {
      const url = databaseViewUrlState(currentUrl());
      const named = addressViewLeftByPeek() ? null : url.viewId;
      if (!url.rowId && !inPeek && named !== linkedViewApplied.current && snapshot.views.length > 0) {
        const linked = named ? snapshot.views.find(({ id }) => id === named) : undefined;
        // A view the page's kept copy doesn't have yet opens once it arrives.
        if (linked || !named) linkedViewApplied.current = named;
        if (linked && linked.id !== snapshot.activeView.id) adapter.selectView(snapshot.block, linked);
      }
      const ownsRow = (rowId: string | null) => Boolean(rowId) && ([...snapshot.rows, ...(snapshot.archivedRows ?? [])]
        .some(({ id }) => id === rowId) || Boolean(snapshot.trashedRowIds?.includes(rowId!)));
      if (!url.rowId && url.openAbove.length === 0) opened.current = null;
      // The row the address opens, or else the nearest row above it that
      // this view opened, which stays open beneath the one opened from it.
      const above = ownsRow(url.rowId) ? undefined : [...url.openAbove].reverse().find(({ rowId }) => ownsRow(rowId));
      // Beneath a row opened in full, the rows above it cover the page too.
      const state = above
        ? { ...url, rowId: above.rowId, rowMode: url.rowMode === "full" ? "full" as const : above.rowMode, viewId: above.viewId || null }
        : url;
      if (!addressRowHere || !state.rowId || !ownsRow(state.rowId) || (rowScope && state.viewId !== rowScope)
        || snapshot.activeView.type === "dashboard" || openPages.includes(state.rowId)) {
        opener.offer(null);
        setRowSelection(null);
        return;
      }
      const selected = state.viewId ? snapshot.views.find(({ id }) => id === state.viewId) : undefined;
      opener.offer({ rowId: state.rowId, preferred: Boolean(selected) || (!state.viewId && opened.current === state.rowId) });
      if (!opener.isOpener()) {
        setRowSelection(null);
        return;
      }
      const rowId = state.rowId, mode = shownMode(state.rowMode);
      setRowSelection((current) => current?.rowId === rowId && current.mode === mode ? current : { rowId, mode });
      if (selected && selected.id !== snapshot.activeView.id) adapter.selectView(snapshot.block, selected);
    }
    synchronize();
    window.addEventListener("popstate", synchronize);
    // An address moved in the app names a row too: one made elsewhere (a
    // sidebar "+") opens here once both it and the address have come.
    window.addEventListener(ROUTE_CHANGE_EVENT, synchronize);
    const unsubscribe = subscribeRowOpeners(synchronize);
    return () => {
      window.removeEventListener("popstate", synchronize);
      window.removeEventListener(ROUTE_CHANGE_EVENT, synchronize);
      unsubscribe();
    };
  }, [adapter, addressRowHere, inPeek, openPages, opener, rowScope, shownMode, snapshot.activeView.id, snapshot.activeView.type, snapshot.archivedRows, snapshot.block, snapshot.rows, snapshot.trashedRowIds, snapshot.views]);

  const closeRow = useCallback(() => {
    if (typeof window !== "undefined") {
      // Closing a preview closes the rows beneath it too, as Notion's does
      // (back returns to them); a row opened in full from inside another row
      // goes back to that row's page. A database in a page leaves the page's
      // own address, without the view the row was opened from; a database
      // page keeps its view (Notion's, seen 2026-11-03).
      const state = databaseViewUrlState(currentUrl());
      const parent = state.rowId === rowSelection?.rowId && rowSelection?.mode === "full" ? state.openAbove.at(-1) : undefined;
      publishUrl(databaseViewUrlWithSelection(currentUrl(), parent
        ? { rowId: parent.rowId, rowMode: parent.rowMode, viewId: parent.viewId || undefined, openAbove: state.openAbove.slice(0, -1) }
        : { rowId: null, ...(pageNamesView ? {} : { viewId: null }) }));
    }
    opener.offer(null);
    setRowSelection(null);
    announceRowSelection();
  }, [opener, pageNamesView, rowSelection?.mode, rowSelection?.rowId]);
  // A press closing the row's side peek from outside it — its row's "닫기",
  // its "…" menu's trash — slides it out in the press's own frame, as one in
  // the peek does (useSidePeekMotion).
  const closeRowAtPress = useCallback(() => leaveSidePeek(closeRow), [closeRow]);

  if (rowSelection) {
    const context: DatabaseViewOperationContext = {
      contextIdentity: snapshot.contextIdentity,
      dataSourceId: snapshot.dataSource.id,
      requestGeneration: snapshot.requestGeneration,
      requestKey: `row:${rowSelection.rowId}`,
      signal: lifecycle.signal,
      snapshotIdentity: snapshot.snapshotIdentity,
      viewId: snapshot.activeView.id,
    };
    void operations.isCurrent(context);
  }

  return {
    closeRow,
    closeRowAtPress,
    lifecycle,
    operations: operationSurface(operations),
    rowSelection,
  };
}
