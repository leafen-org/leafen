"use client";

import type { ProductStatusSink } from "@/lib/productStatus";
import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Search } from "@/icons/leafen";
import { positionBetween } from "@/lib/ids";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { keepAndSendPageMetadata, optimisticPageMetadata, type NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { fullPageDatabaseSource } from "@/lib/relationDatabaseSearch";
import { MOVE_LOAD_MORE_KEY, MoveLoadMoreRow, moveSearchShown } from "./moveSearchWindow";
import { productRefusalReason } from "@/lib/productErrorPresentation";
import { pathLeavesOwnPages } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { DatabaseRowMoveDestination } from "@/lib/databaseRowPageService";
import type { Page, Session } from "@/types";
import { type MovePopoverAnchor, useMovePopoverStyle } from "./movePopoverPlacement";
import { useProductPages } from "./product/productPagesContext";
import {
  appendMovePosition,
  moveDestinations,
  moveListRows,
  moveTreeChildren, movedOutOfPrivate,
  pageMap,
  toggledMoveRow,
  type MoveDestination,
  type MoveListRow,
} from "./pageMoveDialogModel";
import { MOVE_LIST_KEYS, MoveListRows, useMoveListEdges } from "./MoveDestinationRow";
import { MoveDatabaseOption } from "./MoveDatabaseOption";
import { moveDatabaseLocation, rankedAmongPages } from "./moveSearchOrder";
import { rememberMoveTarget, useMoveSuggestions } from "./moveSuggestions";
import { notePageChangeStep } from "./editor/pageUndo";
import { MoveWorkspacePicker } from "./MoveWorkspacePicker";
import { useMoveToWorkspace } from "./useMoveToWorkspace";
import { putPageBackShown, useDatabasePageMove } from "./useDatabasePageMove";
import { databasePageSourceShown, movePagesIntoDatabaseShown, showRowAsPage, withPagesUnder } from "./pagesIntoDatabase";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { useMoveSearches } from "./useMoveSearches";
import { useMovePlaces } from "./useMovePlaces";
import styles from "./MoveToDialog.module.css";
import { useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { confirmAction } from "./ConfirmActionDialog";
import { confirmMovePrivate } from "./movePrivateConfirm";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { pageHref } from "@/lib/navigation";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { MoveToPhoneSheet } from "./MoveToPhoneSheet";
import { plainTitleOf } from "@/lib/closedMentions";
import { ownPresses } from "@/lib/ownPresses";
import { focusedOpener } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";

/** A database's page the popover moves, by its row's own preview and move. */
export type MoveToDialogRow = {
  page: Page;
  preview: (target: DatabaseRowMoveDestination) => Promise<{ propertyId: string; name: string }[]>;
  move: (target: DatabaseRowMoveDestination, acceptDroppedPropertyIds: string[]) => Promise<unknown>;
  /** Puts the row back where it was from where it went, for the toast's "실행 취소". */
  undo?: (target: DatabaseRowMoveDestination) => Promise<unknown>;
  /** Shows the row gone from where it is drawn, from the press until the returned release; released as refused, it is back. */
  leave?: (target: DatabaseRowMoveDestination) => (refused?: boolean) => void;
};

/**
 * "옮기기" for a page, as Notion's (seen 2026-09-30): a popover under what
 * opened it, with no title, a search for where the page goes, and the
 * places it can go under "추천", or under "페이지" once searched. Its foot
 * chooses the workspace; to another one the page moves, beyond Notion's
 * copy, under a note of what that cuts (useMoveToWorkspace).
 * A database's page moves through the same popover, as Notion's favorites
 * do (seen 2026-10-31), by its row's own move, which first says what values
 * the place can't keep; to another workspace it is copied as a page.
 */

const NO_PAGES: readonly string[] = [];


/** "〈이름〉의 변경된 위치는 …", or by count when the page has no name or more
    went with it, as Notion's ("페이지 1개의 변경된 위치는 …", seen 2026-11-07). */
// A refused move says why, as every screen says the server's reason
// (productErrorPresentation); a move that failed otherwise says it failed.
function moveFailure(error: unknown, failed: string): string {
  return productRefusalReason(error, "contentGraph.update") || failed;
}

/** Where a move went, as its notice names it: a page (its "이동" opens it) or a place's top, in its teamspace or none. */
type MovedPlace = { label: string; pageId: string; top?: boolean; teamspaceId?: string };

// The notice's "이동" opens the page moved into; moved to a place's top
// (private pages, a teamspace) it offers none and its "실행 취소" no arrow,
// as Notion's (seen 2026-11-13: a page and a row); a private page taken into a teamspace has no "실행 취소" (movedOutOfPrivate).
function goToAction(t: (key: string) => string, place: MovedPlace, moved: string) {
  return place.top ? [] : [{ label: t("moveToDialog:toast.go"), onClick: () => pushRoute(pageHref(place.pageId || moved)) }];
}

function movedNotice(t: (key: string, options?: Record<string, unknown>) => string, page: { title: string }, place: string, count: number) {
  return count > 1 || !page.title
    ? t("moveToDialog:toast.movedCountTo", { count, place })
    : t("moveToDialog:toast.movedTo", { title: page.title, place });
}

export function MoveToDialog({
  pageId,
  alsoPageIds = NO_PAGES,
  pages,
  session,
  anchor,
  anchorGap,
  onClose,
  onMoved,
  onPageChange,
  onStatus,
  row: givenRow,
  addTo = false,
}: {
  pageId: string;
  /** The other pages chosen with it, as the library's rows, which follow it there in order. */
  alsoPageIds?: readonly string[];
  pages: Page[];
  session: Session;
  /** What opened it, which it hangs under. */
  anchor?: MovePopoverAnchor | null;
  /** Its distance from what it opens at. */
  anchorGap?: number;
  onClose: () => void;
  onMoved?: () => void;
  onPageChange: (page: Page) => void;
  onStatus?: ProductStatusSink;
  /** A page just made, offered a place from its peek's "추가 대상": Notion's "추가할 페이지 검색" (seen 2026-11-01). */
  addTo?: boolean;
  /** A database's page to move instead of one of the sidebar's pages. */
  row?: MoveToDialogRow;
}) {
  const { t } = useTranslation(["moveToDialog", "rowMenu"]);
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(focusedOpener());
  const operationRef = useRef<AbortController | null>(null);
  const [query, setQuery] = useState("");
  // The suggestions opened to the pages under them, by their place in the tree.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const [targetWorkspaceId, setTargetWorkspaceId] = useState("");
  const productPages = useProductPages();
  // The open page, where pages moved into a database it draws show as its rows at the press.
  const store = useOpenPageGraphStore();
  const localPagesById = useMemo(() => pageMap(pages), [pages]);
  const row = useDatabasePageMove(session, localPagesById[pageId], givenRow, localPagesById);
  const movingPage = row?.page ?? localPagesById[pageId];
  const workspaceId = movingPage?.workspaceId?.trim() ?? "";
  // The list shows the workspace chosen at the foot; another one's pages
  // are the reader's, and its places their private pages and the teamspaces
  // they may add to there.
  const listWorkspaceId = targetWorkspaceId || workspaceId;
  const copying = listWorkspaceId !== workspaceId;
  const workspaces = productPages?.workspaces ?? [];
  const copyTo = useMoveToWorkspace(session, movingPage, workspaces.find((workspace) => workspace.id === listWorkspaceId), Boolean(row), () => close());
  const places = useMovePlaces(session, listWorkspaceId);
  // Before a search, the pages last moved into and visited there are suggested (moveDestinations).
  const { recentPageIds, movedToPageIds } = useMoveSuggestions(session, listWorkspaceId);
  const listPagesById = useMemo(
    () => (copying ? pageMap(productPages?.pages ?? []) : localPagesById),
    [copying, localPagesById, productPages?.pages],
  );
  // A page can also move into a database, as the database's page; a
  // database into none, which its search shows dimmed among the pages, as
  // Notion's (seen 2026-11-11; pageMoveDialogModel's databaseRefused).
  const { remotePagesById, titleSearchComplete, databases, shown, loadMore } = useMoveSearches({
    session, query, enabled: Boolean(movingPage), workspaceId: listWorkspaceId,
    databaseWorkspaceId: copying || movingPage?.kind === "database" ? "" : workspaceId,
  });
  const searchablePagesById = useMemo(
    () => ({ ...listPagesById, ...remotePagesById }),
    [listPagesById, remotePagesById],
  );
  const sourceParentLocked = Boolean(
    movingPage?.parentId && searchablePagesById[movingPage.parentId]?.isLocked,
  );
  // A page under a database's page is placed by the path the server names
  // (pagePathPieces.ts), as in search; the list asks for its pages' below.
  const pathsRead = usePagePathsAbove([]);

  // A database the search found as a database is offered once, as Notion's
  // list does: not again as the page the title search names it by (2026-11-10:
  // "order DB" stood twice, as a page and as a database).
  const databasesFound = useMemo(() => new Set(databases.flatMap(({ containerId, pageId }) => [containerId, pageId])), [databases]);
  const sharedPageIds = productPages?.sharedPageIds;
  const destinations = useMemo<MoveDestination[]>(() => movingPage
    ? moveDestinations({
        lockedParentLabel: t("moveToDialog:locked.parent"),
        movingPage,
        alsoMovingIds: alsoPageIds,
        pagesById: searchablePagesById,
        places,
        query,
        recentPageIds, movedToPageIds,
        workspaceId: listWorkspaceId,
        sharedPageIds: new Set(sharedPageIds ?? []),
      }).filter((destination) => !(destination.type === "page" && destination.page.kind === "database" && databasesFound.has(destination.page.id)))
    // The paths read so far place them again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    : [], [alsoPageIds, databasesFound, listWorkspaceId, movedToPageIds, movingPage, places, query, recentPageIds, searchablePagesById, sharedPageIds, t, pathsRead]);
  // A search shows its answers twenty at a time (moveSearchWindow.tsx).
  const searched = useMemo(() => moveSearchShown(destinations, Boolean(query.trim()), shown), [destinations, query, shown]);
  // Without a search, the suggestions open into the pages under them, as
  // Notion's (seen 2026-10-01); the private pages as the sidebar's private section lists them.
  const rows = useMemo<MoveListRow[]>(() => {
    if (!movingPage) return [];
    const treeInput = {
      lockedParentLabel: t("moveToDialog:locked.parent"),
      movingPage,
      alsoMovingIds: alsoPageIds,
      pagesById: searchablePagesById,
      workspaceId: listWorkspaceId,
      sharedPageIds: new Set(sharedPageIds ?? []),
    };
    return moveListRows({
      destinations: searched.found, searching: Boolean(query.trim()), expanded,
      childrenOf: (destination) => moveTreeChildren(destination, treeInput),
    });
    // The paths read so far place them again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [alsoPageIds, searched, expanded, listWorkspaceId, movingPage, query, searchablePagesById, sharedPageIds, t, pathsRead]);
  const options = useMemo(
    () => rows.flatMap((row) => (row.kind === "option" ? [row.destination] : [])),
    [rows],
  );
  // Those the search found beyond the reader's own pages, a database's pages among them, too.
  usePagePathsAbove(useMemo(() => options.flatMap((option) => (
    option.type === "page" && (!listPagesById[option.page.id] || pathLeavesOwnPages(option.page, (id) => searchablePagesById[id]))
      ? [option.page] : []
  )), [listPagesById, options, searchablePagesById]));
  const databaseLocation = (database: Parameters<typeof moveDatabaseLocation>[0]) => moveDatabaseLocation(database, searchablePagesById);
  const itemCount = options.length + databases.length;
  // A just-made page's "추가 대상" list hangs right under its button, as Notion's (seen 2026-11-13: at its foot, 108).
  const popoverStyle = useMovePopoverStyle(anchor, dialogRef, `${itemCount}:${query}`, anchorGap ?? (addTo ? 0 : undefined));
  const listEdges = useMoveListEdges(resultsRef, `${itemCount}:${rows.length}`);

  const close = useCallback((restoreFocus = true) => {
    operationRef.current?.abort();
    operationRef.current = null;
    onClose();
    // Back where the focus was, or — gone with the menu it was chosen from — to
    // the button that menu hung from, as Notion's (judged by the screen and a
    // letter typed after Escape, no script in the page before it, 2026-11-15
    // in Chrome: the top bar's "…" › 옮기기, Escape — the "…" ringed, its tip
    // shown, the letter landing nowhere).
    if (restoreFocus) giveFocusBack({ to: () => {
      const was = restoreFocusRef.current, back = was?.isConnected && was !== document.body ? was : anchor?.contextElement;
      restoreFocusRef.current = null;
      return back instanceof HTMLElement ? back : null;
    }, unlessOpened: true });
  }, [anchor, onClose]);

  const toast = useOptionalProductToast();
  // A phone's "옮기기" is a whole sheet (MoveToPhoneSheet).
  const { phone } = useSidebarLayout(true);
  // As Notion's (seen 2026-10-31): "〈page〉의 변경된 위치는 〈place〉입니다." with
  // "이동" to the place and "실행 취소", which puts one page back. Put last in
  // the place it was already in, it says nothing (seen 2026-11-10).
  const finishMove = useCallback((operation: AbortController, movedPage: Page, place: MovedPlace,
    answered: Promise<Page> = Promise.resolve(movedPage)) => {
    if (operationRef.current !== operation || operation.signal.aborted) return;
    onPageChange(movedPage);
    const before = movingPage;
    // The page's ⌘Z and the notice's "실행 취소" put it back the same way,
    // on the page as the server answered the move (shown before it did).
    let latest = answered;
    const moveTo = async (spot: Pick<Page, "parentId" | "teamspaceId" | "position">) => {
      const base = await latest;
      const next = keepAndSendPageMetadata(session, productPages?.mutationOutbox, base, {
        parentId: spot.parentId, teamspaceId: spot.teamspaceId ?? "", position: spot.position,
      });
      latest = next.catch(() => base);
      onPageChange(await next);
    };
    const placeChanged = !before || alsoPageIds.length > 0 || before.parentId !== movedPage.parentId
      || (before.teamspaceId ?? "") !== (movedPage.teamspaceId ?? "");
    if (before && alsoPageIds.length === 0 && placeChanged) {
      const after = { parentId: movedPage.parentId, teamspaceId: movedPage.teamspaceId, position: movedPage.position };
      notePageChangeStep(movedPage.id, before, after, moveTo);
    }
    if (placeChanged && toast && before) {
      toast.notify(movedNotice(t, movedPage, place.label, 1 + alsoPageIds.length),
        "default", [
          ...goToAction(t, place, movedPage.id),
          ...(alsoPageIds.length === 0 && !movedOutOfPrivate(before, place) ? [{ label: t("moveToDialog:toast.undo"), icon: "none" as const, onClick: () => moveTo(before) }] : []),
        ]);
    } else if (placeChanged) onStatus?.(t("moveToDialog:toast.moved"), "default");
    operationRef.current = null;
    if (onMoved) {
      close(false);
      onMoved();
    } else {
      close();
    }
  }, [alsoPageIds.length, close, movingPage, onMoved, onPageChange, onStatus, productPages, session, t, toast]);

  // The other pages chosen go to the same place one after another, past the
  // first, shown there with it at the press (user decision 2026-11-12: a
  // press answers on the next frame — each showed once the server had moved
  // the one before it); one the place is inside stays where it is. Each goes
  // to the server in turn (`send`); one refused goes back with those after
  // it, and `back` puts them all back.
  const showRest = useCallback((parentId: string, place: { teamspaceId?: string }, first: Pick<Page, "position">) => {
    let previous = first.position;
    const rest: { page: Page; change: NativePageMetadataChange }[] = [];
    for (const id of alsoPageIds) {
      const other = localPagesById[id];
      let inside = false;
      for (let at = searchablePagesById[parentId]; at && !inside; at = searchablePagesById[at.parentId]) inside = at.id === id;
      if (!other || inside || id === pageId) continue;
      const change = { parentId, ...place, position: positionBetween(previous, undefined) };
      previous = change.position;
      rest.push({ page: other, change });
      onPageChange(optimisticPageMetadata(other, change, session.user.id));
    }
    return {
      back: () => { for (const { page } of rest) onPageChange(page); },
      send: async (sent: (page: Page, change: NativePageMetadataChange) => Promise<Page>) => {
        for (const [at, { page, change }] of rest.entries()) {
          try {
            onPageChange(await sent(page, change));
          } catch (error) {
            for (const { page: back } of rest.slice(at)) onPageChange(back);
            throw error;
          }
        }
      },
    };
  }, [alsoPageIds, localPagesById, onPageChange, pageId, searchablePagesById, session]);

  // A database's page leaves where it is drawn at the press (`row.leave`)
  // and goes behind it (user decision 2026-11-12: a press answers on the
  // next frame — the popover stayed and the row in its database while the
  // server previewed and made the move, 0.85–1.2 s on a NAS). A page keeps
  // every value it had (the server holds them for it), so nothing is asked
  // and the move is said at once, with "실행 취소" when the row knows its way
  // back, as a page's move says (finishMove); another database may not keep
  // some, which only the server can say (its preview), asked about before
  // the move and said moved once agreed. `shown` is where it goes as known
  // at the press, `going` the place it is sent to once known (a database
  // page's source, read; a new page, once the server made it), null when
  // it can't go. Not agreed to or refused, the row is back where it was and
  // the notice goes; false.
  const rowMoved = useCallback(async (shown: DatabaseRowMoveDestination, place: MovedPlace,
    going: Promise<DatabaseRowMoveDestination | null> = Promise.resolve(shown)) => {
    if (!row || !movingPage) return false;
    const left = row.leave?.(shown) ?? (() => undefined);
    // Out as a page it is in the sidebar at once too, where the server puts it.
    const listed = shown.kind === "page"
      ? showRowAsPage({ page: movingPage, parentId: shown.id, teamspaceId: shown.teamspaceId, pagesById: searchablePagesById,
        setData: productPages?.setData }) : () => undefined;
    const release = (refused?: boolean) => { listed(refused); left(refused); };
    const notice = movedNotice(t, movingPage, place.label, 1);
    let moving: Promise<unknown> = Promise.resolve(), target = shown;
    const undo = row.undo;
    const say = () => {
      if (!toast) return onStatus?.(t("moveToDialog:toast.moved"), "default");
      toast.notify(notice, "default", [
        ...goToAction(t, place, movingPage.id),
        // Pressed before the server moved it, it goes back once it has; refused, it says so.
        ...(undo ? [{ label: t("moveToDialog:toast.undo"), icon: "none" as const, onClick: async () => {
          await moving;
          await undo(target).catch((error: unknown) => onStatus?.(moveFailure(error, t("moveToDialog:toast.moveFailed")), "error"));
        } }] : []),
      ]);
    };
    const keepsAll = shown.kind === "page";
    if (keepsAll) say();
    try {
      const sent = await going;
      if (sent) target = sent;
      const dropped = !sent || keepsAll ? [] : await row.preview(sent);
      if (!sent || (dropped.length > 0 && !await confirmAction({
        message: `${t("rowMenu:moveConfirm.body", { title: plainTitleOf(movingPage) || untitledPageDisplayTitle(movingPage) })} ${dropped.map(({ name }) => name).join(", ")}`,
        confirmLabel: t("rowMenu:moveConfirm.confirm"),
      }))) {
        release(true);
        toast?.dismissMessage(notice);
        return false;
      }
      if (!keepsAll) say();
      moving = row.move(sent, dropped.map(({ propertyId }) => propertyId));
      await moving;
    } catch (error) {
      release(true);
      toast?.dismissMessage(notice);
      throw error;
    }
    release();
    return true;
  }, [movingPage, onStatus, productPages?.setData, row, searchablePagesById, t, toast]);

  // The popover closes at the press, the row's move going on behind it (rowMoved).
  const moveRow = useCallback((shown: DatabaseRowMoveDestination, place: MovedPlace,
    going?: Promise<DatabaseRowMoveDestination | null>) => {
    if (!row || operationRef.current) return;
    void rowMoved(shown, place, going).catch((error: unknown) => onStatus?.(moveFailure(error, t("moveToDialog:toast.moveFailed")), "error"));
    close(!onMoved);
    onMoved?.();
  }, [close, onMoved, onStatus, row, rowMoved, t]);

  // Pages moved into a database leave the sidebar at the press and stand as
  // its rows where the open page draws it, the popover closed and the move
  // said, as a page's move says (finishMove): one page also with "실행 취소",
  // once the server moved it (Notion's, seen 2026-11-11). The server moves
  // them behind the press (movePagesIntoDatabaseShown); `source` is the data
  // source, read behind it for a full-page database's page. Refused, the
  // notice goes and the reason is said.
  const chooseDatabase = useCallback((database: { id: string; title: string; pageId?: string },
    source: string | Promise<string | undefined> = database.id) => {
    if (!movingPage || operationRef.current) return;
    if (row) {
      moveRow({ kind: "database", id: database.id }, { label: database.title, pageId: database.pageId ?? "" });
      return;
    }
    if (sourceParentLocked) {
      onStatus?.(t("moveToDialog:toast.lockedParent"), "error");
      return;
    }
    const ids = [pageId, ...alsoPageIds.filter((other) => other !== pageId && localPagesById[other])];
    const before = movingPage, notice = movedNotice(t, before, database.title, ids.length);
    const failed = (error: unknown) => onStatus?.(moveFailure(error, t("moveToDialog:toast.moveFailed")), "error");
    const moving = movePagesIntoDatabaseShown({ session, pageIds: ids, pages, source, store, setData: productPages?.setData });
    if (toast) {
      toast.notify(notice, "default", [
        { label: t("moveToDialog:toast.go"), onClick: () => pushRoute(pageHref(database.pageId || before.id)) },
        // Back in the sidebar with the pages under it and out of the database
        // at the press, once the move is done (putPageBackShown).
        ...(ids.length === 1 ? [{ label: t("moveToDialog:toast.undo"), onClick: () => {
          const under = withPagesUnder(pages, [before.id]), listed = pages.filter(({ id }) => under.has(id));
          void moving.then(() => putPageBackShown({ session, before, listed, store, setData: productPages?.setData })
            .then(onPageChange, failed), () => undefined);
        } }] : []),
      ]);
    } else onStatus?.(t("moveToDialog:toast.moved"), "default");
    void moving.catch((error: unknown) => {
      toast?.dismissMessage(notice);
      failed(error);
    });
    close(!onMoved);
    onMoved?.();
  }, [alsoPageIds, close, localPagesById, moveRow, movingPage, onMoved, onPageChange, onStatus, pageId, pages, productPages, row, session, sourceParentLocked, store, t, toast]);

  // A database's data source, found by its page or, for one a page's body holds (no page of the
  // reader's), its database's ID under that page, which a row or a page moved onto it goes into.
  const databasePageSource = (destination: Extract<MoveDestination, { type: "page" }>) =>
    fullPageDatabaseSource(session, destination.page, undefined, !localPagesById[destination.id]);

  const choose = useCallback(async (destination: MoveDestination) => {
    if (!movingPage || operationRef.current) return;
    // Another workspace takes the page, a database's page a copy (useMoveToWorkspace).
    if (copying) {
      void copyTo(destination);
      return;
    }
    if (destination.type === "page" && !destination.inside) rememberMoveTarget(session, workspaceId, destination.id);
    if (row) {
      if (destination.type === "page" && destination.page.isLocked) {
        onStatus?.(t("moveToDialog:toast.locked"), "error");
      } else if (destination.type === "page" && destination.page.kind === "database") {
        // A full-page database takes the row into its data source, read behind the press.
        moveRow({ kind: "database", id: "" }, { label: destination.label, pageId: destination.id },
          databasePageSource(destination).then((source) => source ? { kind: "database" as const, id: source.id } : Promise.reject(new Error("The database's source was not found."))));
      } else if (destination.type === "place") {
        // At a place's top it goes to the place chosen: a teamspace, or the
        // private pages, asked first from a teamspace as a page is (Notion's, seen 2026-11-13).
        if (!destination.place.teamspaceId && movingPage.teamspaceId
          && !await confirmMovePrivate(plainTitleOf(movingPage) || untitledPageDisplayTitle(movingPage))) return;
        moveRow({ kind: "page", id: "", teamspaceId: destination.place.teamspaceId }, { label: destination.label, pageId: "", top: true });
      } else {
        moveRow({ kind: "page", id: destination.id }, { label: destination.label, pageId: destination.id });
      }
      return;
    }
    if (destination.type === "page" && destination.inside) return;
    // A full-page database takes a page into its data source too, as Notion's (seen 2026-11-06: a row moved
    // out and back came back with its values); a page can't sit under it as a page. Its source is the open
    // page's, or read behind the press.
    if (destination.type === "page" && destination.page.kind === "database") {
      chooseDatabase({ id: "", title: destination.label, pageId: destination.id },
        databasePageSourceShown(store, destination.id) || databasePageSource(destination).then((source) => source?.id));
      return;
    }
    if (sourceParentLocked) {
      onStatus?.(t("moveToDialog:toast.lockedParent"), "error");
      return;
    }
    if (destination.type === "page" && destination.page.isLocked) {
      onStatus?.(t("moveToDialog:toast.locked"), "error");
      return;
    }
    // A page moved to the top of private pages is shared with no one.
    if (destination.type === "place" && !destination.place.teamspaceId && movingPage.teamspaceId
      && !await confirmMovePrivate(plainTitleOf(movingPage) || untitledPageDisplayTitle(movingPage))) {
      return;
    }
    const parentId = destination.type === "place" ? "" : destination.id;
    if (destination.type === "page" && !localPagesById[destination.id]) {
      onPageChange(destination.page);
    }
    const operation = new AbortController();
    operationRef.current = operation;
    const place = destination.type === "place" ? { teamspaceId: destination.place.teamspaceId } : {};
    const change = { parentId, ...place, position: appendMovePosition(searchablePagesById, parentId, pageId) };
    // The page shows in its new place and the dialog closes at once (user
    // decision 2026-11-12: a press answers on the next frame); the server's
    // answer follows, past the dialog, and a refusal puts the page back.
    const shown = optimisticPageMetadata(movingPage, change, session.user.id);
    const sending = keepAndSendPageMetadata(session, productPages?.mutationOutbox, movingPage, change);
    const rest = showRest(parentId, place, shown);
    finishMove(operation, shown, destination.type === "page" ? { label: destination.label, pageId: destination.id, teamspaceId: destination.page.teamspaceId } : { label: destination.label, pageId: "", top: true, teamspaceId: destination.place.teamspaceId },
      sending.catch(() => movingPage));
    let result: Awaited<typeof sending>;
    try {
      result = await sending;
    } catch (error) {
      onPageChange(movingPage);
      rest.back();
      toast?.dismissMessage(movedNotice(t, shown, destination.label, 1 + alsoPageIds.length));
      onStatus?.(moveFailure(error, t("moveToDialog:toast.moveFailed")), "error");
      return;
    }
    onPageChange(result);
    try {
      await rest.send((page, next) => keepAndSendPageMetadata(session, productPages?.mutationOutbox, page, next));
      // The pages under a page moved to another place went with it.
      if ((result.teamspaceId ?? "") !== (movingPage.teamspaceId ?? "") || alsoPageIds.length > 0) productPages?.refreshPages?.();
    } catch (error) {
      onStatus?.(moveFailure(error, t("moveToDialog:toast.moveFailed")), "error");
    }
  }, [alsoPageIds, chooseDatabase, close, copyTo, copying, finishMove, localPagesById, moveRow, movingPage, onPageChange, onStatus, pageId, productPages, row, searchablePagesById, session, showRest, sourceParentLocked, store, t, toast, workspaceId]);

  // A modal layer: Escape closes it once nothing opened over it (the
  // workspace menu) takes the key.
  const closeRef = useRef<() => void>(() => undefined);
  closeRef.current = () => close();
  const { zIndex } = useOverlayLayer(Boolean(movingPage), {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], restoreFocus: false, onEscape: () => closeRef.current(),
  });

  // The search takes the keys as soon as the dialog shows, so none typed
  // right after choosing "Move to" are lost.
  useLayoutEffect(() => {
    inputRef.current?.focus();
    return () => {
      operationRef.current?.abort();
      operationRef.current = null;
    };
  }, []);

  // The databases found stand among the pages by how well their names answer (rankedAmongPages).
  const listed = useMemo(() => rankedAmongPages(rows, (row) => row.kind === "option" && row.group === "pages"
    ? { score: row.destination.score, label: row.destination.label } : null, databases), [databases, rows]);
  type Choice = { key: string; label: string; destination?: MoveDestination; database?: { id: string; title: string }; disabled?: boolean };
  const choices: Choice[] = [...listed.flatMap((row): Choice[] => row.kind === "option" ? [{ key: row.key, label: row.destination.label,
    destination: row.destination, disabled: row.destination.disabled }] : row.kind === "database" ? [{ key: row.key, label: row.database.title, database: row.database }] : []),
    ...(searched.more ? [{ key: MOVE_LOAD_MORE_KEY, label: t("moveToDialog:results.loadMore") }] : [])];
  const picker = useNativeAutocomplete({ items: choices, query, onQuery: setQuery, inputRef, listRef: resultsRef,
    popoverRef: dialogRef, label: t("moveToDialog:search.label"), open: Boolean(movingPage),
    disabledKeys: choices.filter(choice => choice.disabled).map(choice => choice.key), commitTab: false, ...MOVE_LIST_KEYS,
    // No suggestion is chosen until an arrow key comes to one, the pointer
    // grounding the one under it apart; a search's first result is, dimmed
    // or not (MOVE_LIST_KEYS), as Notion's (seen 2026-11-05, 2026-11-14).
    highlightFirst: query.trim().length > 0,
    keyOf: choice => choice.key, textOf: choice => choice.label, onPick: choice => {
      if (choice.destination) void choose(choice.destination);
      else if (choice.database) void chooseDatabase(choice.database);
      else if (choice.key === MOVE_LOAD_MORE_KEY) loadMore();
    } });

  if (!movingPage) return null;
  // A just-made page's "추가 대상" has no other workspace to go to at its foot, as Notion's (seen 2026-11-13).
  const workspacePicker = workspaces.length > 1 && alsoPageIds.length === 0 && !addTo ? (
    <MoveWorkspacePicker
      workspaces={workspaces}
      workspaceId={listWorkspaceId}
      onChange={(id) => {
        setTargetWorkspaceId(id);
        setQuery("");
        setExpanded(new Set());
      }}
    />
  ) : null;
  const copyNotice = copying ? <p className={styles.copyNotice}><WarningFillIcon size="small" aria-hidden="true" />{t(row ? "moveToDialog:copy.notice" : "moveToDialog:move.notice")}</p> : null;
  // A search that finds nothing says so and offers nothing else, once it has answered (Notion's, seen 2026-11-13).
  const nothingFound = options.length === 0 && databases.length === 0 && (!query.trim() || titleSearchComplete);
  if (phone) {
    return <MoveToPhoneSheet query={query} onQuery={setQuery} notice={copyNotice} foot={workspacePicker} onClose={() => close()} nothingFound={nothingFound}
      owner={{ id: session.user.id, name: session.user.name.trim() || session.user.email }}
      placeholder={t(copying && row ? "moveToDialog:copy.searchPlaceholder" : addTo ? "moveToDialog:search.addToPlaceholder" : "moveToDialog:search.placeholder")}
      destinations={options} databases={databases.map((database) => ({ ...database, location: databaseLocation(database) }))}
      onChoose={(destination) => void choose(destination)} onChooseDatabase={(database) => void chooseDatabase(database)} onMore={searched.more ? loadMore : undefined} />;
  }
  // At the document's root, as the bar's other popovers: the bar (and a full
  // row peek's top) fades while someone writes, and ⌘⇧P pressed in the text
  // opens this over it — Notion's stays in view while its bar is away (seen 2026-11-07).
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }} {...ownPresses}>
      <button type="button" className={styles.backdrop} onClick={() => close()} tabIndex={-1} aria-label={t("moveToDialog:dialog.closeLabel")} />
      <div ref={dialogRef} className={styles.dialog} style={popoverStyle} role="dialog" aria-modal="true"
        aria-label={t("moveToDialog:title")} data-move-popover={anchor ? "anchored" : undefined} data-searching={query.trim() ? "true" : undefined} data-copying={copying ? "true" : undefined}
        data-workspace-foot={addTo ? "none" : undefined}>
        <div className={styles.searchRow}>
          <Search size="small" aria-hidden="true" />
          <input {...picker.inputProps} ref={inputRef}
            placeholder={t(copying && row ? "moveToDialog:copy.searchPlaceholder" : addTo ? "moveToDialog:search.addToPlaceholder" : "moveToDialog:search.placeholder")} />
        </div>
        {copyNotice}
        <div {...picker.listBoxProps} {...listEdges} className={styles.results} ref={resultsRef}
          onPointerLeave={() => { if (!query.trim()) picker.state.selectionManager.setFocusedKey(null); }}>
          {/* The databases found stand under "페이지" among the pages, each saying where it is as a page does. */}
          <MoveListRows rows={listed} state={picker.state} owner={{ id: session.user.id, name: session.user.name.trim() || session.user.email }}
            heading={(group) => t(`moveToDialog:results.${group}`)}
            onToggle={query.trim() ? undefined : (key) => setExpanded((current) => toggledMoveRow(current, key))}
            database={(row) => <MoveDatabaseOption key={row.key} itemKey={row.key} state={picker.state} database={row.database}
              location={query.trim() ? databaseLocation(row.database) : []} />} />
          {searched.more && <MoveLoadMoreRow state={picker.state} />}
          {nothingFound && <div className={styles.empty} role="status">{t("moveToDialog:results.empty")}</div>}
        </div>
        {workspacePicker}
      </div>
    </div>,
    document.body,
  );
}
